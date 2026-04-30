package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/spf13/cobra"
)

// runcmdCmd is the Azure analogue of the AWS `ssm` command tree. The verb is
// deliberately different so users don't conflate the two: AWS SSM has
// long-lived sessions and an in-VM agent; Azure Run Command is one-shot,
// control-plane, and stateless. Same operator goals (interactive shell,
// run-on-many) — different mechanisms.
var runcmdCmd = &cobra.Command{
	Use:   "runcmd",
	Short: "Run commands and open shells via Azure Run Command",
	Long: `Azure equivalent of 'ssm'. Uses Azure Run Command (control-plane,
no inbound ports). Caveats vs. AWS SSM: each invocation is one-shot
(~5-15s latency), output is capped at 4096 bytes per stream, and there
are no persistent sessions to list or clean up.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Get()
		if err != nil {
			return err
		}
		if cfg.ResolvedProvider() != provider.NameAzure {
			return fmt.Errorf("runcmd is only available with the Azure provider (current: %s); use 'ssm' for AWS", cfg.ResolvedProvider())
		}
		return nil
	},
}

var runcmdConnectCmd = &cobra.Command{
	Use:   "connect <host>",
	Short: "Open a Run Command-backed REPL to a host",
	Long: `Opens an interactive shell that loops Azure Run Command invocations.
Each line you type takes ~5-15s and is capped at 4096 bytes of output per stream.
$PWD is preserved across commands.

For real-time interactive shells, deploy Azure Bastion (dreadgoad infra apply
--with-bastion) and use 'dreadgoad bastion ssh|rdp|tunnel'.

Type "exit" or send EOF (Ctrl+D) to disconnect.`,
	Args: cobra.ExactArgs(1),
	RunE: runRuncmdConnect,
}

var runcmdRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a PowerShell command across GOAD instances",
	RunE:  runRuncmdRun,
}

func init() {
	rootCmd.AddCommand(runcmdCmd)
	runcmdCmd.AddCommand(runcmdConnectCmd)
	runcmdCmd.AddCommand(runcmdRunCmd)

	runcmdRunCmd.Flags().String("hosts", "all", "Comma-separated host names or 'all'")
	runcmdRunCmd.Flags().StringP("cmd", "c", "", "PowerShell command to execute")
	_ = runcmdRunCmd.MarkFlagRequired("cmd")
}

func runRuncmdConnect(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return err
	}
	shell, ok := prov.(provider.InteractiveShell)
	if !ok {
		return fmt.Errorf("azure provider does not implement InteractiveShell (build issue)")
	}

	instanceID, err := resolveAzureHost(ctx, prov, cfg, args[0])
	if err != nil {
		return err
	}

	fmt.Printf("Opening Azure Run Command shell to %s...\n", args[0])
	return shell.StartInteractiveShell(ctx, instanceID, "")
}

func runRuncmdRun(cmd *cobra.Command, args []string) error {
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

	results, err := prov.RunCommandOnMultiple(ctx, targetIDs, psCmd, 5*time.Minute)
	if err != nil {
		return err
	}

	for i, id := range targetIDs {
		name := targetNames[i]
		result := results[id]
		fmt.Printf("=== %s ===\n", name)
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

// resolveAzureHost looks up the target host's Azure resource ID. The Azure
// inventory may not carry resource IDs (they're long), so prefer live
// discovery via the provider and fall back to the inventory ansible_host.
func resolveAzureHost(ctx context.Context, prov provider.Provider, cfg *config.Config, hostName string) (string, error) {
	if inst, err := prov.FindInstanceByHostname(ctx, cfg.Env, hostName); err == nil && inst.ID != "" {
		return inst.ID, nil
	}
	if inv, err := inventory.Parse(cfg.InventoryPath()); err == nil {
		if h := inv.HostByName(hostName); h != nil && h.InstanceID != "" {
			return h.InstanceID, nil
		}
	}
	return "", fmt.Errorf("host %q not found via Azure discovery or inventory", hostName)
}
