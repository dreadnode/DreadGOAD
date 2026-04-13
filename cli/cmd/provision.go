package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"slices"

	"github.com/dreadnode/dreadgoad/internal/ansible"
	daws "github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/doctor"
	inv "github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/lab"
	"github.com/dreadnode/dreadgoad/internal/variant"
	"github.com/spf13/cobra"
)

var provisionCmd = &cobra.Command{
	Use:   "provision",
	Short: "Run GOAD provisioning playbooks with retry logic",
	Long: `Runs Ansible playbooks to provision Active Directory infrastructure.

Executes the full playbook sequence (or a subset) with error-specific
retry strategies, SSM session management, and idle timeout monitoring.`,
	Example: `  dreadgoad provision
  dreadgoad provision --plays build.yml,ad-servers.yml
  dreadgoad provision --from ad-data.yml
  dreadgoad provision --env staging --debug
  dreadgoad provision --plays ad-data.yml --limit dc01
  dreadgoad provision --max-retries 5 --retry-delay 60`,
	RunE: runProvision,
}

var adUsersCmd = &cobra.Command{
	Use:   "ad-users",
	Short: "Ensure AD users exist (runs ad-data.yml)",
	RunE: func(cmd *cobra.Command, args []string) error {
		plays, _ := cmd.Flags().GetString("plays")
		if plays == "" {
			_ = cmd.Flags().Set("plays", "ad-data.yml")
		}
		return runProvision(cmd, args)
	},
}

func init() {
	rootCmd.AddCommand(provisionCmd)
	rootCmd.AddCommand(adUsersCmd)

	provisionCmd.Flags().String("plays", "", "Comma-separated playbooks to run (default: all)")
	provisionCmd.Flags().String("from", "", "Resume provisioning from this playbook onward")
	provisionCmd.Flags().String("limit", "", "Limit execution to specific hosts")
	provisionCmd.Flags().Int("max-retries", 0, "Max retry attempts (default: from config)")
	provisionCmd.Flags().Int("retry-delay", 0, "Delay between retries in seconds (default: from config)")

	adUsersCmd.Flags().String("plays", "ad-data.yml", "Playbooks to run")
	adUsersCmd.Flags().String("limit", "", "Limit execution to specific hosts")
	adUsersCmd.Flags().Int("max-retries", 0, "Max retry attempts")
	adUsersCmd.Flags().Int("retry-delay", 0, "Delay between retries in seconds")
}

func resolvePlaybooks(cfg *config.Config, playsFlag, fromFlag string) ([]string, error) {
	if playsFlag != "" && fromFlag != "" {
		return nil, fmt.Errorf("--plays and --from are mutually exclusive")
	}

	var playbooks []string
	if playsFlag != "" {
		playbooks = strings.Split(playsFlag, ",")
	} else {
		playbooks = lab.PlaybooksForLab(cfg.ProjectRoot, "", cfg.Playbooks)
	}

	if fromFlag == "" {
		return playbooks, nil
	}

	for i, p := range playbooks {
		if p == fromFlag {
			return playbooks[i:], nil
		}
	}
	return nil, fmt.Errorf("playbook %q not found in playbook list: %v", fromFlag, playbooks)
}

func ensureVariant(cfg *config.Config) error {
	envCfg := cfg.ActiveEnvironment()
	if !envCfg.Variant {
		return nil
	}
	source, target := cfg.ResolvedVariantPaths()
	variantName := envCfg.VariantName
	if variantName == "" {
		variantName = "variant-1"
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		slog.Info("Variant directory already exists, skipping generation", "target", target)
		return nil
	}
	fmt.Printf("Environment %q has variant=true, generating variant...\n", cfg.Env)
	gen := variant.NewGenerator(source, target, variantName)
	if err := gen.Run(); err != nil {
		return fmt.Errorf("auto variant generation failed: %w", err)
	}
	fmt.Printf("Variant generated: %s\n", target)
	return nil
}

// preflightChecks validates tooling, builds the Ansible collection, and
// prepares artifacts needed before provisioning playbooks run.
func preflightChecks(ctx context.Context, cfg *config.Config) error {
	if err := doctor.CheckAnsibleCoreVersion(); err != nil {
		return fmt.Errorf("ansible-core version check failed: %w", err)
	}
	if err := ansible.BuildCollection(cfg.ProjectRoot); err != nil {
		return fmt.Errorf("collection build failed: %w", err)
	}
	if err := ansible.PrepareADCSZips(cfg.ProjectRoot); err != nil {
		slog.Warn("ADCS zip preparation failed", "error", err)
	}
	if err := ensureVariant(cfg); err != nil {
		return err
	}
	// Ensure inventory instance IDs match live AWS state. After infra
	// apply the IDs change, and provisioning against stale IDs fails
	// with confusing SSM connection errors.
	if err := ensureInventorySynced(ctx, cfg); err != nil {
		slog.Warn("inventory sync check failed", "error", err)
	}
	// Generate instance-to-IP mapping so Ansible can resolve host IPs
	// without slow runtime network detection over SSM.
	if err := generateInstanceMapping(ctx, ""); err != nil {
		slog.Warn("instance mapping generation failed, playbooks will use runtime detection", "error", err)
	}
	return nil
}

// ensureInventorySynced compares inventory instance IDs against live EC2
// state and auto-syncs if they diverge. This prevents provisioning against
// stale instance IDs after an infra destroy/apply cycle.
func ensureInventorySynced(ctx context.Context, cfg *config.Config) error {
	invPath := cfg.InventoryPath()
	parsed, err := inv.Parse(invPath)
	if err != nil {
		return fmt.Errorf("parse inventory: %w", err)
	}

	region, err := cfg.ResolveRegionWithInventory(parsed)
	if err != nil {
		return fmt.Errorf("resolve region: %w", err)
	}
	client, err := daws.NewClient(ctx, region)
	if err != nil {
		return fmt.Errorf("create AWS client: %w", err)
	}

	awsInstances, err := client.DiscoverInstances(ctx, cfg.Env)
	if err != nil {
		return fmt.Errorf("discover instances: %w", err)
	}
	if len(awsInstances) == 0 {
		return fmt.Errorf("no running instances found for env=%s", cfg.Env)
	}

	// Build a set of live instance IDs from AWS.
	liveIDs := make(map[string]struct{}, len(awsInstances))
	for _, inst := range awsInstances {
		liveIDs[inst.InstanceID] = struct{}{}
	}

	// Check if every inventory instance ID exists in the live set.
	stale := false
	for _, host := range parsed.Hosts {
		if host.InstanceID == "" {
			continue
		}
		if _, ok := liveIDs[host.InstanceID]; !ok {
			stale = true
			break
		}
	}

	if !stale {
		return nil
	}

	slog.Info("inventory has stale instance IDs, auto-syncing from AWS")
	var instances []instanceInfo
	for _, i := range awsInstances {
		instances = append(instances, instanceInfo{InstanceID: i.InstanceID, Name: i.Name})
	}
	return applyInstanceUpdates(invPath, instances)
}

func runProvision(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	ctx := context.Background()

	playsFlag, _ := cmd.Flags().GetString("plays")
	fromFlag, _ := cmd.Flags().GetString("from")
	playbooks, err := resolvePlaybooks(cfg, playsFlag, fromFlag)
	if err != nil {
		return err
	}

	limit, _ := cmd.Flags().GetString("limit")
	maxRetries, _ := cmd.Flags().GetInt("max-retries")
	retryDelay, _ := cmd.Flags().GetInt("retry-delay")

	_ = os.MkdirAll(cfg.LogDir, 0o755)
	logFile := filepath.Join(cfg.LogDir, fmt.Sprintf("%s-dreadgoad-%s.log",
		cfg.Env, time.Now().Format("20060102_150405")))

	if err := preflightChecks(ctx, cfg); err != nil {
		return err
	}

	fmt.Println("===============================================")
	fmt.Printf("DreadGOAD provisioning started at %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("Environment: %s\n", cfg.Env)
	fmt.Printf("Log file: %s\n", logFile)
	if limit != "" {
		fmt.Printf("Limited to hosts: %s\n", limit)
	}
	fmt.Println("===============================================")
	fmt.Println("\nPlaybooks to execute:")
	for _, p := range playbooks {
		fmt.Printf("  - ansible/playbooks/%s\n", p)
	}
	fmt.Println("-----------------------------------------------")

	// Clean up stale SSM sessions before starting provisioning to prevent
	// connection saturation from orphaned sessions of previous runs.
	log := slog.Default()
	log.Info("cleaning up stale SSM sessions before provisioning")
	ansible.CleanupSSMSessions(ctx, cfg.Env, log)

	for i, playbook := range playbooks {
		opts := ansible.RetryOptions{
			Playbook: playbook,
			Env:      cfg.Env,
			Limit:    limit,
			Debug:    cfg.Debug,
			LogFile:  logFile,
		}
		if maxRetries > 0 {
			opts.MaxRetries = maxRetries
		}
		if retryDelay > 0 {
			opts.RetryDelay = time.Duration(retryDelay) * time.Second
		}

		if err := ansible.RunPlaybookWithRetry(ctx, opts); err != nil {
			return fmt.Errorf("provisioning failed at %s: %w", playbook, err)
		}

		// Between playbooks: clean up accumulated SSM sessions and wait
		// after reboot-inducing playbooks for SSM agents to reconnect.
		if i < len(playbooks)-1 {
			ansible.CleanupSSMSessions(ctx, cfg.Env, log)

			if slices.Contains(config.RebootPlaybooks, playbook) {
				log.Info("playbook may have caused reboots, waiting for SSM reconnection",
					"playbook", playbook, "delay", "120s")
				time.Sleep(120 * time.Second)
			}
		}
	}

	fmt.Println("===============================================")
	fmt.Printf("All playbooks completed successfully at %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("Full log: %s\n", logFile)
	fmt.Println("===============================================")
	return nil
}
