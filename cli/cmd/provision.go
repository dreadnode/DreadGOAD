package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/ansible"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/doctor"
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
			cmd.Flags().Set("plays", "ad-data.yml")
		}
		return runProvision(cmd, args)
	},
}

func init() {
	rootCmd.AddCommand(provisionCmd)
	rootCmd.AddCommand(adUsersCmd)

	provisionCmd.Flags().String("plays", "", "Comma-separated playbooks to run (default: all)")
	provisionCmd.Flags().String("limit", "", "Limit execution to specific hosts")
	provisionCmd.Flags().Int("max-retries", 0, "Max retry attempts (default: from config)")
	provisionCmd.Flags().Int("retry-delay", 0, "Delay between retries in seconds (default: from config)")

	// ad-users inherits provision flags
	adUsersCmd.Flags().String("plays", "ad-data.yml", "Playbooks to run")
	adUsersCmd.Flags().String("limit", "", "Limit execution to specific hosts")
	adUsersCmd.Flags().Int("max-retries", 0, "Max retry attempts")
	adUsersCmd.Flags().Int("retry-delay", 0, "Delay between retries in seconds")
}

func runProvision(cmd *cobra.Command, args []string) error {
	cfg := config.Get()
	ctx := context.Background()

	// Determine playbooks
	playsFlag, _ := cmd.Flags().GetString("plays")
	var playbooks []string
	if playsFlag != "" {
		playbooks = strings.Split(playsFlag, ",")
	} else {
		playbooks = cfg.Playbooks
	}

	limit, _ := cmd.Flags().GetString("limit")
	maxRetries, _ := cmd.Flags().GetInt("max-retries")
	retryDelay, _ := cmd.Flags().GetInt("retry-delay")

	// Ensure log directory
	os.MkdirAll(cfg.LogDir, 0o755)
	logFile := filepath.Join(cfg.LogDir, fmt.Sprintf("%s-dreadgoad-%s.log",
		cfg.Env, time.Now().Format("20060102_150405")))

	// Pre-flight: verify ansible-core version compatibility
	if err := doctor.CheckAnsibleCoreVersion(); err != nil {
		return fmt.Errorf("ansible-core version check failed: %w", err)
	}

	// Pre-flight: prepare ADCS zips
	if err := ansible.PrepareADCSZips(cfg.ProjectRoot); err != nil {
		slog.Warn("ADCS zip preparation failed", "error", err)
	}

	// Log header
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

	// Run each playbook
	for _, playbook := range playbooks {
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
	}

	fmt.Println("===============================================")
	fmt.Printf("All playbooks completed successfully at %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("Full log: %s\n", logFile)
	fmt.Println("===============================================")
	return nil
}
