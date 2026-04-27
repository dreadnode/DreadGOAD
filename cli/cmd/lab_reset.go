package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	daws "github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/dreadnode/dreadgoad/internal/config"
	inv "github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/spf13/cobra"
)

// adStatePlaybooks restore AD state without re-running infra/build playbooks.
// Used as the default `lab reset` playbook list — these are idempotent and
// undo the AD mutations (ACLs, group membership, password resets, ESC vulns,
// trusts) that attack runs leave behind.
var adStatePlaybooks = []string{
	"ad-data.yml",
	"ad-acl.yml",
	"ad-relations.yml",
	"ad-trusts.yml",
	"vulnerabilities.yml",
}

// purgeGhostsPS removes auto-generated WIN-[A-Z0-9]{11}$ machine accounts
// created by NoPAC / MachineAccountQuota attempts. Runs on each DC under
// the SSM agent's identity (LocalSystem), so no credentials are needed.
const purgeGhostsPS = `$ErrorActionPreference = "Stop"
$pattern = '^WIN-[A-Z0-9]{11}$'
try {
    $ghosts = Get-ADComputer -Filter 'Name -like "WIN-*"' | Where-Object { $_.Name -match $pattern }
} catch {
    Write-Output "ERROR: Get-ADComputer failed: $_"
    exit 1
}
if (-not $ghosts) {
    Write-Output "removed=0"
    exit 0
}
$count = 0
foreach ($g in $ghosts) {
    try {
        Remove-ADComputer -Identity $g -Confirm:$false -ErrorAction Stop
        $count++
    } catch {
        Write-Output "WARN: failed to remove $($g.Name): $_"
    }
}
Write-Output "removed=$count"
`

var labPurgeGhostsCmd = &cobra.Command{
	Use:   "purge-ghosts",
	Short: "Delete WIN-* ghost machine accounts from each DC",
	Long: `Removes auto-generated WIN-[A-Z0-9]{11}$ computer accounts that prior
NoPAC / MachineAccountQuota attempts leave in AD. Runs Remove-ADComputer
on each domain controller via SSM. Idempotent.`,
	RunE: runLabPurgeGhosts,
}

var labResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset the lab to a known-clean AD baseline",
	Long: `Two-stage reset:
  1. Purge WIN-* ghost machine accounts from each DC.
  2. Re-run AD-state playbooks to restore users, ACLs, group membership,
     trusts, and vulnerability seeding.

Idempotent: safe to re-run.`,
	Example: `  dreadgoad lab reset
  dreadgoad lab reset --skip-purge
  dreadgoad lab reset --plays ad-data.yml,ad-acl.yml`,
	RunE: runLabReset,
}

func init() {
	labCmd.AddCommand(labPurgeGhostsCmd)
	labCmd.AddCommand(labResetCmd)

	labResetCmd.Flags().Bool("skip-purge", false, "Skip the ghost-account purge stage")
	labResetCmd.Flags().Bool("skip-provision", false, "Skip the AD-state playbook stage")
	labResetCmd.Flags().String("plays", "", "Comma-separated playbooks (default: AD-state set)")
	labResetCmd.Flags().String("limit", "", "Limit playbook execution to specific hosts")
	labResetCmd.Flags().Int("max-retries", 0, "Max retry attempts (default: from config)")
	labResetCmd.Flags().Int("retry-delay", 0, "Delay between retries in seconds (default: from config)")
}

func runLabPurgeGhosts(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	ctx := context.Background()
	return purgeGhostAccounts(ctx, cfg)
}

type dcTarget struct {
	hostname   string
	instanceID string
}

// collectDCTargets returns the inventory's DC hosts that have usable instance IDs.
// Hosts without IDs are reported as warnings rather than failing the run.
func collectDCTargets(parsed *inv.Inventory) []dcTarget {
	var targets []dcTarget
	for _, name := range parsed.Groups["dc"] {
		h := parsed.HostByName(name)
		if h == nil || h.InstanceID == "" || h.InstanceID == "PENDING" {
			fmt.Printf("WARN: skipping %s — no instance ID in inventory (run `dreadgoad inventory sync`)\n", name)
			continue
		}
		targets = append(targets, dcTarget{hostname: name, instanceID: h.InstanceID})
	}
	return targets
}

// runGhostPurgeOnDC executes the purge script on a single DC and returns the
// number of accounts removed (or 0 on any non-fatal failure).
func runGhostPurgeOnDC(ctx context.Context, client *daws.Client, t dcTarget) int {
	fmt.Printf("=== %s (%s) ===\n", t.hostname, t.instanceID)
	result, err := client.RunPowerShellCommand(ctx, t.instanceID, purgeGhostsPS, 5*time.Minute)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
		return 0
	}
	if out := result.Stdout; out != "" {
		fmt.Print(out)
		if !strings.HasSuffix(out, "\n") {
			fmt.Println()
		}
	}
	if result.Stderr != "" {
		fmt.Printf("  STDERR: %s\n", strings.TrimSpace(result.Stderr))
	}
	if result.Status != "Success" {
		fmt.Printf("  status=%s (continuing)\n", result.Status)
	}
	if n := parseRemovedCount(result.Stdout); n >= 0 {
		return n
	}
	return 0
}

// purgeGhostAccounts discovers DC instances from the inventory and runs the
// ghost-purge PowerShell on each via SSM. SSM-side failures are reported
// per-host but do not abort the run.
func purgeGhostAccounts(ctx context.Context, cfg *config.Config) error {
	parsed, err := inv.Parse(cfg.InventoryPath())
	if err != nil {
		return fmt.Errorf("parse inventory: %w", err)
	}
	if !parsed.IsSSM() {
		return fmt.Errorf("purge-ghosts currently requires an SSM-based inventory")
	}
	if len(parsed.Groups["dc"]) == 0 {
		return fmt.Errorf("no hosts in [dc] group of inventory %s", cfg.InventoryPath())
	}

	targets := collectDCTargets(parsed)
	if len(targets) == 0 {
		return fmt.Errorf("no DC instance IDs available; sync the inventory first")
	}

	region, err := cfg.ResolveRegionWithInventory(parsed)
	if err != nil {
		return err
	}
	client, err := daws.NewClient(ctx, region)
	if err != nil {
		return err
	}

	fmt.Printf("Purging WIN-* ghost accounts on %d DC(s) in %s...\n", len(targets), region)
	total := 0
	for _, t := range targets {
		total += runGhostPurgeOnDC(ctx, client, t)
	}
	fmt.Printf("\nTotal ghost accounts purged: %d\n", total)
	return nil
}

// parseRemovedCount extracts the integer N from "removed=N" lines emitted by
// the ghost-purge script. Returns -1 if not found.
func parseRemovedCount(out string) int {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "removed="); ok {
			var n int
			if _, err := fmt.Sscanf(rest, "%d", &n); err == nil {
				return n
			}
		}
	}
	return -1
}

func runLabReset(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	ctx := context.Background()

	skipPurge, _ := cmd.Flags().GetBool("skip-purge")
	skipProvision, _ := cmd.Flags().GetBool("skip-provision")
	playsFlag, _ := cmd.Flags().GetString("plays")
	limit, _ := cmd.Flags().GetString("limit")
	maxRetries, _ := cmd.Flags().GetInt("max-retries")
	retryDelay, _ := cmd.Flags().GetInt("retry-delay")

	playbooks := adStatePlaybooks
	if playsFlag != "" {
		playbooks = strings.Split(playsFlag, ",")
	}

	fmt.Printf("=== DreadGOAD lab-reset (env=%s) ===\n", cfg.Env)
	fmt.Printf("    skip_purge=%v skip_provision=%v\n", skipPurge, skipProvision)
	fmt.Printf("    plays=%s\n\n", strings.Join(playbooks, ","))

	if !skipPurge {
		fmt.Println("--- Stage 1: purge ghost machine accounts ---")
		if err := purgeGhostAccounts(ctx, cfg); err != nil {
			fmt.Printf("WARN: ghost purge failed: %v (continuing)\n", err)
		}
		fmt.Println()
	}

	if !skipProvision {
		fmt.Println("--- Stage 2: restore AD baseline state ---")
		if err := provisionPlaybooks(ctx, cfg, playbooks, limit, maxRetries, retryDelay); err != nil {
			return err
		}
	}

	fmt.Println("=== lab-reset complete ===")
	return nil
}
