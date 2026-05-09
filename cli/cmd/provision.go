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
	"github.com/dreadnode/dreadgoad/internal/azure"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/doctor"
	inv "github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/lab"
	"github.com/dreadnode/dreadgoad/internal/ludus"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/dreadnode/dreadgoad/internal/variant"
	"github.com/spf13/cobra"
)

// closableTunnel is the union return shape from maybeStartSOCKSTunnel. The
// Ludus and Azure paths wrap different transports but both expose a Close().
type closableTunnel interface{ Close() }

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
	provisionCmd.MarkFlagsMutuallyExclusive("plays", "from")

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

// isSSMInventory checks whether the current inventory uses AWS SSM connections.
// Returns false (non-SSM) if the inventory does not exist or cannot be parsed,
// so that non-AWS providers are never blocked by AWS-specific operations.
func isSSMInventory(cfg *config.Config) bool {
	parsed, err := inv.Parse(cfg.InventoryPath())
	if err != nil {
		return false
	}
	return parsed.IsSSM()
}

// preflightChecks validates tooling, builds the Ansible collection, and
// prepares artifacts needed before provisioning playbooks run.
func preflightChecks(ctx context.Context, cfg *config.Config) error {
	if err := doctor.CheckAnsibleCoreVersion(); err != nil {
		return fmt.Errorf("ansible-core version check failed: %w", err)
	}
	if err := ansible.InstallRequirements(cfg.ProjectRoot); err != nil {
		return fmt.Errorf("ansible dependency install failed: %w", err)
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

	// Bootstrap the inventory file for all providers. For non-AWS providers
	// (Ludus, Proxmox) this renders the provider-specific template. For AWS
	// it copies from the .example template. This must happen before the
	// SSM-specific checks below, which depend on the inventory existing.
	if err := bootstrapInventory(cfg.InventoryPath()); err != nil {
		return fmt.Errorf("inventory bootstrap failed: %w", err)
	}

	// AWS-specific preflight: sync inventory instance IDs and generate
	// IP mappings. Skipped for non-SSM providers (Ludus, Proxmox, etc.)
	// where the inventory is managed manually.
	if isSSMInventory(cfg) {
		if err := ensureInventorySynced(ctx, cfg); err != nil {
			slog.Warn("inventory sync check failed", "error", err)
		}
		if err := generateInstanceMapping(ctx, ""); err != nil {
			slog.Warn("instance mapping generation failed, playbooks will use runtime detection", "error", err)
		}
	}
	return nil
}

// bootstrapInventory creates the inventory file if it does not exist.
// For AWS, it copies from the .example template.
// For Proxmox and other providers, it renders the provider-specific
// inventory template from ad/LAB/providers/PROVIDER/inventory.
func bootstrapInventory(invPath string) error {
	if _, err := os.Stat(invPath); err == nil {
		return nil
	}

	cfg, cfgErr := config.Get()
	if cfgErr == nil && !cfg.IsAWS() {
		if err := bootstrapFromProviderTemplate(invPath, cfg); err == nil {
			return nil
		}
	}

	return bootstrapFromExample(invPath)
}

func bootstrapFromProviderTemplate(invPath string, cfg *config.Config) error {
	labName := "GOAD"
	if cfg.ResolvedProvider() == "proxmox" {
		labName = cfg.ProxmoxLab()
	}
	providerName := cfg.ResolvedProvider()
	templatePath := filepath.Join(cfg.ProjectRoot, "ad", labName, "providers", providerName, "inventory")

	data, err := os.ReadFile(templatePath)
	if err != nil {
		return err
	}

	ipRange, err := resolveIPRange(cfg, providerName)
	if err != nil {
		return err
	}

	rendered := strings.ReplaceAll(string(data), "{{ip_range}}", ipRange)
	if err := os.WriteFile(invPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write inventory: %w", err)
	}
	slog.Info("bootstrapped inventory from provider template", "path", invPath, "provider", providerName)
	return nil
}

func resolveIPRange(cfg *config.Config, providerName string) (string, error) {
	if providerName == "ludus" {
		ctx := context.Background()
		if prov, err := cfg.NewProvider(ctx); err == nil {
			type ipRanger interface {
				IPRange(ctx context.Context) (string, error)
			}
			if lr, ok := prov.(ipRanger); ok {
				if r, err := lr.IPRange(ctx); err == nil {
					return r, nil
				}
			}
		}
		return "", fmt.Errorf("ludus range not deployed yet; run 'dreadgoad infra apply' first to get IP range")
	}

	ipRange := cfg.Proxmox.IPRange
	if ipRange == "" {
		ipRange = "192.168.10"
	}
	return ipRange, nil
}

func bootstrapFromExample(invPath string) error {
	examplePath := invPath + ".example"
	if _, err := os.Stat(examplePath); err != nil {
		return fmt.Errorf("inventory file not found: %s (no .example template either)", invPath)
	}
	data, err := os.ReadFile(examplePath)
	if err != nil {
		return fmt.Errorf("read example inventory: %w", err)
	}
	if err := os.WriteFile(invPath, data, 0o644); err != nil {
		return fmt.Errorf("write inventory from example: %w", err)
	}
	slog.Info("bootstrapped inventory from example template", "path", invPath)
	return nil
}

// ensureInventorySynced compares inventory instance IDs against live EC2
// state and auto-syncs if they diverge. This prevents provisioning against
// stale instance IDs after an infra destroy/apply cycle.
// This is a no-op for non-SSM inventories (e.g. Ludus, Proxmox).
func ensureInventorySynced(ctx context.Context, cfg *config.Config) error {
	invPath := cfg.InventoryPath()
	if err := bootstrapInventory(invPath); err != nil {
		return err
	}
	parsed, err := inv.Parse(invPath)
	if err != nil {
		return fmt.Errorf("parse inventory: %w", err)
	}

	if !parsed.IsSSM() {
		return nil
	}

	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	liveInstances, err := prov.DiscoverInstances(ctx, cfg.Env)
	if err != nil {
		return fmt.Errorf("discover instances: %w", err)
	}
	if len(liveInstances) == 0 {
		return fmt.Errorf("no running instances found for env=%s", cfg.Env)
	}

	liveIDs := make(map[string]struct{}, len(liveInstances))
	for _, inst := range liveInstances {
		liveIDs[inst.ID] = struct{}{}
	}

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

	slog.Info("inventory has stale instance IDs, auto-syncing from provider")
	var instances []instanceInfo
	for _, i := range liveInstances {
		instances = append(instances, instanceInfo{InstanceID: i.ID, Name: i.Name})
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

	return provisionPlaybooks(ctx, cfg, playbooks, limit, maxRetries, retryDelay)
}

// provisionPlaybooks runs preflight checks then executes the given playbooks
// with retry logic. Shared between `provision` and `lab reset`.
func provisionPlaybooks(ctx context.Context, cfg *config.Config, playbooks []string, limit string, maxRetries, retryDelay int) error {
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

	// When running against a remote Ludus server or Azure (where private
	// VMs aren't reachable from the laptop), open a SOCKS5 proxy through
	// SSH and override Ansible to route WinRM via the psrp connection
	// plugin. For Azure, the proxy chain is: laptop → Bastion port-tunnel
	// → controller SSH → SOCKS5 → in-VNet GOAD VM:5985.
	var socksTunnel closableTunnel
	var socksVars map[string]string
	if tunnel, vars, err := maybeStartSOCKSTunnel(ctx, cfg); err != nil {
		return fmt.Errorf("SOCKS tunnel setup failed: %w", err)
	} else if tunnel != nil {
		socksTunnel = tunnel
		socksVars = vars
		defer socksTunnel.Close()
	}

	log := slog.Default()
	useSSM := isSSMInventory(cfg)

	// Clean up stale SSM sessions before starting provisioning to prevent
	// connection saturation from orphaned sessions of previous runs.
	if useSSM {
		log.Info("cleaning up stale SSM sessions before provisioning")
		ansible.CleanupSSMSessions(ctx, cfg.Env, log)
	}

	for i, playbook := range playbooks {
		opts := ansible.RetryOptions{
			Playbook:  playbook,
			Env:       cfg.Env,
			Limit:     limit,
			Debug:     cfg.Debug,
			LogFile:   logFile,
			ExtraVars: socksVars,
		}
		if maxRetries > 0 {
			opts.MaxRetries = maxRetries
		}
		if retryDelay > 0 {
			opts.RetryDelay = time.Duration(retryDelay) * time.Second
		}

		if err := ansible.RunPlaybookWithRetry(ctx, opts); err != nil {
			return fmt.Errorf("provisioning failed at %s: %w\n  see full log: %s", playbook, err, logFile)
		}

		// Between playbooks: clean up accumulated SSM sessions and wait
		// after reboot-inducing playbooks for SSM agents to reconnect.
		if i < len(playbooks)-1 {
			if useSSM {
				ansible.CleanupSSMSessions(ctx, cfg.Env, log)
			}
			if useSSM && slices.Contains(config.RebootPlaybooks, playbook) {
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

// maybeStartSOCKSTunnel selects a provider-appropriate SOCKS5 tunnel for
// reaching private Windows hosts. Returns (nil, nil, nil) when the active
// provider doesn't need one (AWS SSM dial-in, Ludus in local mode, etc.).
//
// Each branch returns the same shape: a closable handle + the Ansible
// extra-vars that route the psrp connection plugin through the proxy.
func maybeStartSOCKSTunnel(ctx context.Context, cfg *config.Config) (closableTunnel, map[string]string, error) {
	switch cfg.ResolvedProvider() {
	case provider.NameAzure:
		return startAzureSOCKSTunnel(ctx, cfg)
	case "ludus":
		return startLudusSOCKSTunnel(cfg)
	default:
		return nil, nil, nil
	}
}

// startAzureSOCKSTunnel chains an Azure Bastion port-forward + an SSH SOCKS5
// proxy through the in-VNet Ansible controller, then returns the psrp vars
// Ansible needs to dial GOAD VM:5985 through that chain.
func startAzureSOCKSTunnel(ctx context.Context, cfg *config.Config) (closableTunnel, map[string]string, error) {
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("create azure provider: %w", err)
	}
	azProv, ok := prov.(*azure.AzureProvider)
	if !ok {
		return nil, nil, fmt.Errorf("provider is not azure (got %T)", prov)
	}

	fmt.Println("Opening Azure Bastion → controller → SOCKS5 chain for WinRM access...")
	tunnel, err := azure.StartProvisionTunnel(ctx, azProv.Client(), cfg.Env)
	if err != nil {
		return nil, nil, err
	}
	fmt.Printf("  SOCKS5 proxy: %s\n", tunnel.ProxyURL())

	vars := map[string]string{
		"ansible_connection":           "psrp",
		"ansible_psrp_proxy":           tunnel.ProxyURL(),
		"ansible_psrp_auth":            "ntlm",
		"ansible_psrp_cert_validation": "ignore",
		"ansible_psrp_protocol":        "http",
		"ansible_port":                 "5985",
	}
	return tunnel, vars, nil
}

// startLudusSOCKSTunnel preserves the original Ludus-in-SSH-mode behavior:
// open SSH to the Ludus host, layer SOCKS5, route Ansible psrp through it.
func startLudusSOCKSTunnel(cfg *config.Config) (closableTunnel, map[string]string, error) {
	target := cfg.Ludus.SSHTarget()
	if target == "" {
		return nil, nil, nil
	}

	sshCfg := ludus.SSHConfig{
		Host:     target,
		User:     cfg.Ludus.SSHUser,
		KeyPath:  cfg.Ludus.SSHKeyPath,
		Password: cfg.Ludus.SSHPassword,
		Port:     cfg.Ludus.SSHPort,
	}

	fmt.Println("Starting SOCKS5 tunnel to Ludus host for WinRM access...")
	tunnel, err := ludus.StartSOCKSTunnel(sshCfg)
	if err != nil {
		return nil, nil, err
	}
	fmt.Printf("  SOCKS5 proxy listening on localhost:%d\n", tunnel.Port)

	// Override Ansible connection vars to route WinRM through the tunnel
	// using the psrp connection plugin (which supports SOCKS proxies).
	vars := map[string]string{
		"ansible_connection":           "psrp",
		"ansible_psrp_proxy":           tunnel.ProxyURL(),
		"ansible_psrp_auth":            "ntlm",
		"ansible_psrp_cert_validation": "ignore",
		"ansible_psrp_protocol":        "http",
		"ansible_port":                 "5985",
	}

	return tunnel, vars, nil
}
