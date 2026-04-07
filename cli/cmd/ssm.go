package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	daws "github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/spf13/cobra"
)

var ssmCmd = &cobra.Command{
	Use:   "ssm",
	Short: "Manage AWS SSM sessions",
}

var ssmStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active SSM sessions for environment instances",
	RunE:  runSSMStatus,
}

var ssmCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Terminate stale SSM sessions",
	RunE:  runSSMCleanup,
}

var ssmConnectCmd = &cobra.Command{
	Use:   "connect <host>",
	Short: "Start interactive SSM session to a host",
	Long:  `Opens an interactive SSM session. Type "exit" to disconnect. Ctrl+C interrupts the running remote command without closing the session.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runSSMConnect,
}

var ssmRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run PowerShell commands across GOAD instances via SSM",
	RunE:  runSSMRun,
}

func init() {
	rootCmd.AddCommand(ssmCmd)
	ssmCmd.AddCommand(ssmStatusCmd)
	ssmCmd.AddCommand(ssmCleanupCmd)
	ssmCmd.AddCommand(ssmConnectCmd)
	ssmCmd.AddCommand(ssmRunCmd)

	ssmCleanupCmd.Flags().Int("max-age", 30, "Sessions older than this (minutes) are stale")
	ssmCleanupCmd.Flags().Bool("dry-run", false, "Show what would be terminated")

	ssmRunCmd.Flags().String("hosts", "all", "Comma-separated host names or 'all'")
	ssmRunCmd.Flags().StringP("cmd", "c", "", "PowerShell command to execute")
	_ = ssmRunCmd.MarkFlagRequired("cmd")
}

func runSSMStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	ctx := context.Background()

	inv, err := inventory.Parse(cfg.InventoryPath())
	if err != nil {
		return fmt.Errorf("parse inventory: %w", err)
	}

	client, err := daws.NewClient(ctx, inv.Region())
	if err != nil {
		return err
	}

	fmt.Printf("Active SSM sessions for %s environment:\n\n", cfg.Env)

	for _, host := range inv.Hosts {
		if host.InstanceID == "" {
			continue
		}

		sessions, err := client.DescribeActiveSessions(ctx, host.InstanceID)
		if err != nil {
			fmt.Printf("[%s] %s: error: %v\n", host.Name, host.InstanceID, err)
			continue
		}

		if len(sessions) == 0 {
			fmt.Printf("[%s] %s: No active sessions\n", host.Name, host.InstanceID)
		} else {
			fmt.Printf("[%s] %s: %d active session(s)\n", host.Name, host.InstanceID, len(sessions))
			for _, s := range sessions {
				fmt.Printf("  - %s (%s, started: %s)\n", s.SessionID, s.Status, s.StartDate.Format(time.RFC3339))
			}
		}
	}
	return nil
}

func runSSMCleanup(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	ctx := context.Background()

	maxAge, _ := cmd.Flags().GetInt("max-age")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	inv, err := inventory.Parse(cfg.InventoryPath())
	if err != nil {
		return fmt.Errorf("parse inventory: %w", err)
	}

	client, err := daws.NewClient(ctx, inv.Region())
	if err != nil {
		return err
	}

	fmt.Printf("Checking for stale SSM sessions (older than %d minutes)...\n", maxAge)

	terminated, err := client.CleanupStaleSessions(ctx, inv.InstanceIDs(),
		time.Duration(maxAge)*time.Minute, dryRun, slog.Default())
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Println("\nDry run complete. Use --dry-run=false to actually terminate.")
	} else {
		fmt.Printf("\nCleanup complete. Terminated %d stale session(s).\n", terminated)
	}
	return nil
}

func runSSMConnect(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	inv, err := inventory.Parse(cfg.InventoryPath())
	if err != nil {
		return fmt.Errorf("parse inventory: %w", err)
	}

	host := inv.HostByName(args[0])
	if host == nil || host.InstanceID == "" {
		return fmt.Errorf("host %q not found in inventory", args[0])
	}

	region := inv.Region()
	fmt.Printf("Starting SSM session to %s (%s) in %s...\n", host.Name, host.InstanceID, region)

	// Ignore SIGINT in the parent so Ctrl+C is forwarded to the SSM
	// session process (which handles it as a remote command interrupt)
	// rather than killing dreadgoad and tearing down the session.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	defer signal.Stop(sigCh)

	ssmCmd := exec.Command("aws", "ssm", "start-session",
		"--target", host.InstanceID,
		"--region", region)
	ssmCmd.Stdin = os.Stdin
	ssmCmd.Stdout = os.Stdout
	ssmCmd.Stderr = os.Stderr
	return ssmCmd.Run()
}

func runSSMRun(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	ctx := context.Background()

	hostsFlag, _ := cmd.Flags().GetString("hosts")
	psCmd, _ := cmd.Flags().GetString("cmd")

	// Determine region - prefer flag, then inventory
	region := cfg.Region
	if region == "" {
		region = "us-west-1"
	}

	client, err := daws.NewClient(ctx, region)
	if err != nil {
		return err
	}

	// Discover instances
	instances, err := client.DiscoverInstances(ctx, cfg.Env)
	if err != nil {
		return fmt.Errorf("discover instances: %w", err)
	}

	if len(instances) == 0 {
		return fmt.Errorf("no running GOAD instances found for env=%s", cfg.Env)
	}

	targetIDs, targetNames := filterInstances(instances, hostsFlag)
	if len(targetIDs) == 0 {
		return fmt.Errorf("no matching instances found")
	}

	fmt.Printf("Running command on: %s\n", strings.Join(targetNames, ", "))
	fmt.Printf("Command: %s\n\n", psCmd)

	results, err := client.RunPowerShellOnMultiple(ctx, targetIDs, psCmd, 60*time.Second)
	if err != nil {
		return err
	}

	for i, id := range targetIDs {
		name := targetNames[i]
		result := results[id]
		fmt.Printf("=== %s (%s) ===\n", name, id)
		if result != nil {
			fmt.Printf("Status: %s\n", result.Status)
			if result.Stdout != "" {
				fmt.Println(result.Stdout)
			}
			if result.Stderr != "" {
				fmt.Printf("STDERR: %s\n", result.Stderr)
			}
		}
		fmt.Println()
	}
	return nil
}

func filterInstances(instances []daws.Instance, hostsFlag string) ([]string, []string) {
	var ids, names []string
	if hostsFlag == "all" {
		for _, inst := range instances {
			ids = append(ids, inst.InstanceID)
			names = append(names, inst.Name)
		}
		return ids, names
	}
	for _, hostName := range strings.Split(hostsFlag, ",") {
		hostName = strings.TrimSpace(hostName)
		found := false
		for _, inst := range instances {
			if strings.Contains(strings.ToUpper(inst.Name), strings.ToUpper(hostName)) {
				ids = append(ids, inst.InstanceID)
				names = append(names, inst.Name)
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("WARNING: Host %q not found\n", hostName)
		}
	}
	return ids, names
}
