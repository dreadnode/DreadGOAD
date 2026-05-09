package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/spf13/cobra"
)

var ssmCmd = &cobra.Command{
	Use:   "ssm",
	Short: "Manage AWS SSM sessions",
	Long: `SSM commands are AWS-specific. For Azure use 'dreadgoad runcmd'
(Azure Run Command). For other providers see their respective verbs.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Get()
		if err != nil {
			return err
		}
		if !cfg.IsAWS() {
			return fmt.Errorf("ssm commands are only available with the AWS provider (current: %s); use 'runcmd' for Azure", cfg.ResolvedProvider())
		}
		return nil
	},
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

func getSessionManager(ctx context.Context) (provider.SessionManager, *config.Config, error) {
	cfg, err := config.Get()
	if err != nil {
		return nil, nil, err
	}
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return nil, nil, err
	}
	sm, ok := prov.(provider.SessionManager)
	if !ok {
		return nil, nil, fmt.Errorf("provider %s does not support session management", prov.Name())
	}
	return sm, cfg, nil
}

func runSSMStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	sm, cfg, err := getSessionManager(ctx)
	if err != nil {
		return err
	}

	inv, err := inventory.Parse(cfg.InventoryPath())
	if err != nil {
		return fmt.Errorf("parse inventory: %w", err)
	}

	fmt.Printf("Active SSM sessions for %s environment:\n\n", cfg.Env)

	for _, host := range inv.Hosts {
		if host.InstanceID == "" {
			continue
		}

		sessions, err := sm.DescribeActiveSessions(ctx, host.InstanceID)
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
	ctx := context.Background()
	sm, cfg, err := getSessionManager(ctx)
	if err != nil {
		return err
	}

	maxAge, _ := cmd.Flags().GetInt("max-age")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	inv, err := inventory.Parse(cfg.InventoryPath())
	if err != nil {
		return fmt.Errorf("parse inventory: %w", err)
	}

	fmt.Printf("Checking for stale SSM sessions (older than %d minutes)...\n", maxAge)

	terminated, err := sm.CleanupStaleSessions(ctx, inv.InstanceIDs(),
		time.Duration(maxAge)*time.Minute, dryRun)
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
	ctx := context.Background()

	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return err
	}
	shell, ok := prov.(provider.InteractiveShell)
	if !ok {
		return fmt.Errorf("provider %s does not support interactive shells", prov.Name())
	}

	inv, err := inventory.Parse(cfg.InventoryPath())
	if err != nil {
		return fmt.Errorf("parse inventory: %w", err)
	}

	host := inv.HostByName(args[0])
	if host == nil || host.InstanceID == "" {
		return fmt.Errorf("host %q not found in inventory", args[0])
	}

	region, err := cfg.ResolveRegionWithInventory(inv)
	if err != nil {
		return err
	}
	fmt.Printf("Starting SSM session to %s (%s) in %s...\n", host.Name, host.InstanceID, region)

	return shell.StartInteractiveShell(ctx, host.InstanceID, region)
}

func runSSMRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	prov, cfg, err := getProvider(ctx)
	if err != nil {
		return err
	}

	hostsFlag, _ := cmd.Flags().GetString("hosts")
	psCmd, _ := cmd.Flags().GetString("cmd")

	instances, err := prov.DiscoverInstances(ctx, cfg.Env)
	if err != nil {
		return fmt.Errorf("discover instances: %w", err)
	}

	if len(instances) == 0 {
		return fmt.Errorf("no running GOAD instances found for env=%s", cfg.Env)
	}

	targetIDs, targetNames := filterProviderInstances(instances, hostsFlag)
	if len(targetIDs) == 0 {
		return fmt.Errorf("no matching instances found")
	}

	fmt.Printf("Running command on: %s\n", strings.Join(targetNames, ", "))
	fmt.Printf("Command: %s\n\n", psCmd)

	results, err := prov.RunCommandOnMultiple(ctx, targetIDs, psCmd, 60*time.Second)
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

func filterProviderInstances(instances []provider.Instance, hostsFlag string) ([]string, []string) {
	var ids, names []string
	if hostsFlag == "all" {
		for _, inst := range instances {
			ids = append(ids, inst.ID)
			names = append(names, inst.Name)
		}
		return ids, names
	}
	for _, hostName := range strings.Split(hostsFlag, ",") {
		hostName = strings.TrimSpace(hostName)
		found := false
		for _, inst := range instances {
			if strings.Contains(strings.ToUpper(inst.Name), strings.ToUpper(hostName)) {
				ids = append(ids, inst.ID)
				names = append(names, inst.Name)
				found = true
				break
			}
		}
		if !found {
			slog.Warn("host not found", "host", hostName)
		}
	}
	return ids, names
}
