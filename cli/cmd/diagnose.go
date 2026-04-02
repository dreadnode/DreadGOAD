package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dreadnode/dreadgoad/internal/ansible"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/spf13/cobra"
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Run diagnostic checks against domain controllers",
	Long: `Runs the diagnose-dc01 playbook from an independent host to verify
network connectivity, LDAP, WinRM, and DNS for the primary domain controller.

Diagnostics run from dc03/srv03 (vortexindustries domain) to test dc01
(deltasystems domain) connectivity with detailed troubleshooting output.`,
	Example: `  dreadgoad diagnose
  dreadgoad diagnose --dc01-ip 10.0.1.10
  dreadgoad diagnose --env staging --debug`,
	RunE: runDiagnose,
}

func init() {
	rootCmd.AddCommand(diagnoseCmd)

	diagnoseCmd.Flags().String("dc01-ip", "", "Override dc01 IP address (skips AWS lookup)")
}

func runDiagnose(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	ctx := context.Background()

	dc01IP, _ := cmd.Flags().GetString("dc01-ip")

	// Ensure log directory
	_ = os.MkdirAll(cfg.LogDir, 0o755)
	logFile := filepath.Join(cfg.LogDir, fmt.Sprintf("%s-diagnose-%s.log",
		cfg.Env, time.Now().Format("20060102_150405")))

	fmt.Println("===============================================")
	fmt.Printf("DreadGOAD DC01 Diagnostics - %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("Environment: %s\n", cfg.Env)
	fmt.Printf("Log file: %s\n", logFile)
	fmt.Println("===============================================")

	opts := ansible.RunOptions{
		Playbook: "diagnose-dc01.yml",
		Env:      cfg.Env,
		Debug:    cfg.Debug,
		LogFile:  logFile,
	}

	if dc01IP != "" {
		opts.ExtraVars = map[string]string{
			"dc01_ip_override": dc01IP,
		}
		fmt.Printf("Using dc01 IP override: %s\n", dc01IP)
	}

	fmt.Println("Running diagnostics...")
	fmt.Println("-----------------------------------------------")

	result := ansible.RunPlaybook(ctx, opts)

	fmt.Println("===============================================")
	if result.Success {
		fmt.Println("Diagnostics completed successfully.")
	} else {
		fmt.Println("Diagnostics detected issues. Review output above for details.")
		if result.TimedOut {
			fmt.Println("WARNING: Diagnostic playbook timed out.")
		}
	}
	fmt.Printf("Full log: %s\n", logFile)
	fmt.Println("===============================================")

	if !result.Success {
		return fmt.Errorf("diagnostics failed (exit code %d)", result.ExitCode)
	}
	return nil
}
