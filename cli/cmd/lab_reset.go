package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	daws "github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/dreadnode/dreadgoad/internal/config"
	inv "github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/labconfig"
	"github.com/spf13/cobra"
)

// adStatePlaybooks restore AD state without re-running infra/build playbooks.
// Used as the default `lab reset` playbook list — these are idempotent and
// undo the AD mutations (ACLs, group membership, password resets, ESC vulns,
// trusts) that attack runs leave behind.
var adStatePlaybooks = []string{
	"load_network_mappings.yml",
	"ad-data.yml",
	"ad-acl.yml",
	"ad-relations.yml",
	"ad-trusts.yml",
	"vulnerabilities.yml",
}

// purgeArgs is the JSON payload sent to the per-DC PowerShell. It carries
// the allowlist plus the run-mode flags; PowerShell deletes anything in AD
// that doesn't appear here and isn't otherwise exempt (built-ins, DCs,
// trust accounts, admin-created objects).
type purgeArgs struct {
	Apply            bool                `json:"Apply"`
	SkipCreatorCheck bool                `json:"SkipCreatorCheck"`
	Classes          []string            `json:"Classes"`
	Allowlist        labconfig.Allowlist `json:"Allowlist"`
}

// purgeResult is the JSON returned by the per-DC PowerShell after a marker
// line. We parse the slice after the marker so transcript noise above it
// doesn't break unmarshalling.
type purgeResult struct {
	DC               string         `json:"DC"`
	Users            []string       `json:"Users"`
	Computers        []string       `json:"Computers"`
	Groups           []string       `json:"Groups"`
	Skipped          []skippedEntry `json:"Skipped"`
	Errors           []string       `json:"Errors"`
	RemovedUsers     int            `json:"RemovedUsers"`
	RemovedComputers int            `json:"RemovedComputers"`
	RemovedGroups    int            `json:"RemovedGroups"`
}

type skippedEntry struct {
	Class  string `json:"Class"`
	Name   string `json:"Name"`
	Reason string `json:"Reason"`
}

const purgeResultMarker = "---DREADGOAD-PURGE-RESULT---"

// purgeUnmanagedScriptTpl runs on each DC. The %s gets replaced with a
// base64-encoded purgeArgs JSON blob to avoid quoting hell across SSM.
const purgeUnmanagedScriptTpl = `$ErrorActionPreference = "Stop"
$argsJson = [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
$cfg = $argsJson | ConvertFrom-Json

$apply = [bool]$cfg.Apply
$skipCreatorCheck = [bool]$cfg.SkipCreatorCheck
$classSet = @{}
foreach ($c in $cfg.Classes) { $classSet[$c] = $true }

$allowedUsers = @{}
foreach ($v in $cfg.Allowlist.Users) { $allowedUsers[$v.ToLower()] = $true }
$allowedComputers = @{}
foreach ($v in $cfg.Allowlist.Computers) { $allowedComputers[$v.ToUpper()] = $true }
$allowedGroups = @{}
foreach ($v in $cfg.Allowlist.Groups) { $allowedGroups[$v.ToLower()] = $true }
$allowedTrusts = @{}
foreach ($v in $cfg.Allowlist.Trusts) { $allowedTrusts[$v.ToUpper()] = $true }

$exemptUsers = @{}
foreach ($v in @('administrator','krbtgt','guest','defaultaccount','ssm-user','ansible')) {
    $exemptUsers[$v] = $true
}

$UAC_INTERDOMAIN_TRUST = 0x800
$UAC_SERVER_TRUST      = 0x2000

$domain = Get-ADDomain
$adminSids = @{}
$adminSids["$($domain.DomainSID.Value)-512"] = $true   # Domain Admins
$adminSids["$($domain.DomainSID.Value)-519"] = $true   # Enterprise Admins
$adminSids["S-1-5-32-544"]                   = $true   # BUILTIN\Administrators

function Test-CreatorIsAdmin($sid) {
    if (-not $sid) { return $false }
    try {
        $obj = Get-ADObject -Filter "objectSID -eq '$sid'" -Properties memberOf
    } catch { return $false }
    if (-not $obj) { return $false }
    foreach ($g in $obj.memberOf) {
        try {
            $gobj = Get-ADGroup -Identity $g
            if ($adminSids.ContainsKey($gobj.SID.Value)) { return $true }
        } catch { }
    }
    return $false
}

$result = [ordered]@{
    DC               = $env:COMPUTERNAME
    Users            = @()
    Computers        = @()
    Groups           = @()
    Skipped          = @()
    Errors           = @()
    RemovedUsers     = 0
    RemovedComputers = 0
    RemovedGroups    = 0
}

if ($classSet.user) {
    try { $users = Get-ADUser -Filter * -Properties userAccountControl,mS-DS-CreatorSID }
    catch { $result.Errors += "Get-ADUser failed: $_"; $users = @() }
    foreach ($u in $users) {
        $name = $u.SamAccountName.ToLower()
        if ($exemptUsers.ContainsKey($name))    { continue }
        if ($allowedUsers.ContainsKey($name))   { continue }
        $uac = [int]$u.userAccountControl
        if (($uac -band $UAC_INTERDOMAIN_TRUST) -ne 0) { continue }
        if (($uac -band $UAC_SERVER_TRUST)      -ne 0) { continue }
        if ($allowedTrusts.ContainsKey($u.SamAccountName.ToUpper())) { continue }
        if (-not $skipCreatorCheck -and (Test-CreatorIsAdmin $u.'mS-DS-CreatorSID')) {
            $result.Skipped += [ordered]@{ Class='user'; Name=$u.SamAccountName; Reason='admin-creator' }
            continue
        }
        $result.Users += $u.SamAccountName
        if ($apply) {
            try { Remove-ADUser -Identity $u -Confirm:$false -ErrorAction Stop; $result.RemovedUsers++ }
            catch { $result.Errors += "remove user $($u.SamAccountName): $_" }
        }
    }
}

if ($classSet.computer) {
    $dcNames = @{}
    try { foreach ($d in (Get-ADDomainController -Filter *)) { $dcNames[$d.Name.ToUpper()] = $true } }
    catch { $result.Errors += "Get-ADDomainController failed: $_" }
    try { $computers = Get-ADComputer -Filter * -Properties userAccountControl,mS-DS-CreatorSID }
    catch { $result.Errors += "Get-ADComputer failed: $_"; $computers = @() }
    foreach ($c in $computers) {
        $upName = $c.Name.ToUpper()
        if ($dcNames.ContainsKey($upName))         { continue }
        if ($allowedComputers.ContainsKey($upName)) { continue }
        if (-not $skipCreatorCheck -and (Test-CreatorIsAdmin $c.'mS-DS-CreatorSID')) {
            $result.Skipped += [ordered]@{ Class='computer'; Name=$c.Name; Reason='admin-creator' }
            continue
        }
        $result.Computers += $c.Name
        if ($apply) {
            try { Remove-ADComputer -Identity $c -Confirm:$false -ErrorAction Stop; $result.RemovedComputers++ }
            catch { $result.Errors += "remove computer $($c.Name): $_" }
        }
    }
}

if ($classSet.group) {
    try { $groups = Get-ADGroup -Filter * -Properties SID }
    catch { $result.Errors += "Get-ADGroup failed: $_"; $groups = @() }
    foreach ($g in $groups) {
        $sid = $g.SID.Value
        if ($sid -like 'S-1-5-32-*') { continue }
        $parts = $sid -split '-'
        $rid = [int]$parts[$parts.Length - 1]
        if ($rid -lt 1000) { continue }
        $name = $g.Name.ToLower()
        if ($allowedGroups.ContainsKey($name)) { continue }
        $result.Groups += $g.Name
        if ($apply) {
            try { Remove-ADGroup -Identity $g -Confirm:$false -ErrorAction Stop; $result.RemovedGroups++ }
            catch { $result.Errors += "remove group $($g.Name): $_" }
        }
    }
}

Write-Output '` + purgeResultMarker + `'
$result | ConvertTo-Json -Depth 4 -Compress
`

var labPurgeUnmanagedCmd = &cobra.Command{
	Use:   "purge-unmanaged",
	Short: "Delete AD users/computers/groups that aren't in the lab config",
	Long: `Diffs each DC's AD against the lab-config allowlist (lab.domains[*].users,
lab.domains[*].groups, lab.hosts[*].hostname) and deletes anything that
isn't on it. Built-in groups, domain controllers, trust accounts, and
objects created by Domain/Enterprise Admins are always exempted.

Default mode is dry-run; pass --apply to actually delete.

Supersedes the older WIN-* regex-based purge: any computer account whose
name doesn't match a host in the lab config is now caught regardless of
naming pattern (DESKTOP-*, custom names like ARES01$, etc.).`,
	Example: `  dreadgoad lab purge-unmanaged
  dreadgoad lab purge-unmanaged --apply
  dreadgoad lab purge-unmanaged --classes computer --apply
  dreadgoad lab purge-unmanaged --hosts dc01 --skip-creator-check`,
	RunE: runLabPurgeUnmanaged,
}

var labResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset the lab to a known-clean AD baseline",
	Long: `Two-stage reset:
  1. Delete unmanaged AD objects (users, computers, groups not in the lab config).
  2. Re-run AD-state playbooks to restore users, ACLs, group membership,
     trusts, and vulnerability seeding.

Idempotent: safe to re-run.`,
	Example: `  dreadgoad lab reset
  dreadgoad lab reset --skip-purge
  dreadgoad lab reset --plays ad-data.yml,ad-acl.yml`,
	RunE: runLabReset,
}

func init() {
	labCmd.AddCommand(labPurgeUnmanagedCmd)
	labCmd.AddCommand(labResetCmd)

	labPurgeUnmanagedCmd.Flags().Bool("apply", false, "Delete the flagged objects (default: dry-run)")
	labPurgeUnmanagedCmd.Flags().Bool("skip-creator-check", false, "Skip the Domain/Enterprise Admin creator-SID safety belt")
	labPurgeUnmanagedCmd.Flags().String("classes", "user,computer,group", "Object classes to consider (csv)")
	labPurgeUnmanagedCmd.Flags().String("hosts", "", "Limit to specific DC hostnames (csv; default: all in [dc])")

	labResetCmd.Flags().Bool("skip-purge", false, "Skip the unmanaged-object purge stage")
	labResetCmd.Flags().Bool("skip-provision", false, "Skip the AD-state playbook stage")
	labResetCmd.Flags().String("plays", "", "Comma-separated playbooks (default: AD-state set)")
	labResetCmd.Flags().String("limit", "", "Limit playbook execution to specific hosts")
	labResetCmd.Flags().Int("max-retries", 0, "Max retry attempts (default: from config)")
	labResetCmd.Flags().Int("retry-delay", 0, "Delay between retries in seconds (default: from config)")
	labResetCmd.Flags().Bool("skip-creator-check", false, "Skip the admin creator-SID safety belt during purge")
}

type purgeOptions struct {
	apply            bool
	skipCreatorCheck bool
	classes          []string
	hostFilter       []string
}

func runLabPurgeUnmanaged(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	apply, _ := cmd.Flags().GetBool("apply")
	skipCreator, _ := cmd.Flags().GetBool("skip-creator-check")
	classesCSV, _ := cmd.Flags().GetString("classes")
	hostsCSV, _ := cmd.Flags().GetString("hosts")

	opts := purgeOptions{
		apply:            apply,
		skipCreatorCheck: skipCreator,
		classes:          splitCSV(classesCSV),
		hostFilter:       splitCSV(hostsCSV),
	}
	return purgeUnmanaged(context.Background(), cfg, opts)
}

func splitCSV(s string) []string {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

type dcTarget struct {
	hostname   string
	instanceID string
}

// collectDCTargets returns the inventory's DC hosts that have usable instance IDs.
// Hosts without IDs are reported as warnings rather than failing the run.
// If filter is non-empty, only hostnames in filter (case-insensitive) are returned.
func collectDCTargets(parsed *inv.Inventory, filter []string) []dcTarget {
	allowed := map[string]struct{}{}
	for _, f := range filter {
		allowed[strings.ToLower(f)] = struct{}{}
	}
	var targets []dcTarget
	for _, name := range parsed.Groups["dc"] {
		if len(allowed) > 0 {
			if _, ok := allowed[strings.ToLower(name)]; !ok {
				continue
			}
		}
		h := parsed.HostByName(name)
		if h == nil || h.InstanceID == "" || h.InstanceID == "PENDING" {
			fmt.Printf("WARN: skipping %s — no instance ID in inventory (run `dreadgoad inventory sync`)\n", name)
			continue
		}
		targets = append(targets, dcTarget{hostname: name, instanceID: h.InstanceID})
	}
	return targets
}

// buildPurgeScript marshals args, base64-encodes them, and substitutes into
// the PowerShell template.
func buildPurgeScript(args purgeArgs) (string, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("marshal purge args: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return fmt.Sprintf(purgeUnmanagedScriptTpl, encoded), nil
}

// runPurgeOnDC executes the purge script on a single DC, prints a summary,
// and returns the parsed result. Non-fatal errors are logged but do not
// abort the run.
func runPurgeOnDC(ctx context.Context, client *daws.Client, t dcTarget, script string, apply bool) *purgeResult {
	fmt.Printf("=== %s (%s) ===\n", t.hostname, t.instanceID)
	cmdResult, err := client.RunPowerShellCommand(ctx, t.instanceID, script, 5*time.Minute)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
		return nil
	}
	if cmdResult.Status != "Success" {
		fmt.Printf("  status=%s (continuing)\n", cmdResult.Status)
	}
	if cmdResult.Stderr != "" {
		fmt.Printf("  STDERR: %s\n", strings.TrimSpace(cmdResult.Stderr))
	}
	res, parseErr := parsePurgeResult(cmdResult.Stdout)
	if parseErr != nil {
		fmt.Printf("  parse error: %v\n", parseErr)
		if cmdResult.Stdout != "" {
			fmt.Println(cmdResult.Stdout)
		}
		return nil
	}
	printPurgeResult(res, apply)
	return res
}

// purgeUnmanaged is the entry point shared by `lab purge-unmanaged` and
// `lab reset`. It loads the allowlist, fans out the purge script across
// DCs, and prints a per-DC + total summary.
func purgeUnmanaged(ctx context.Context, cfg *config.Config, opts purgeOptions) error {
	if len(opts.classes) == 0 {
		opts.classes = []string{"user", "computer", "group"}
	}

	parsed, err := inv.Parse(cfg.InventoryPath())
	if err != nil {
		return fmt.Errorf("parse inventory: %w", err)
	}
	if !parsed.IsSSM() {
		return fmt.Errorf("purge-unmanaged currently requires an SSM-based inventory")
	}
	if len(parsed.Groups["dc"]) == 0 {
		return fmt.Errorf("no hosts in [dc] group of inventory %s", cfg.InventoryPath())
	}

	targets := collectDCTargets(parsed, opts.hostFilter)
	if len(targets) == 0 {
		return fmt.Errorf("no DC instance IDs available; sync the inventory first")
	}

	allow, err := labconfig.Load(cfg.LabConfigPath())
	if err != nil {
		return fmt.Errorf("load lab config allowlist: %w", err)
	}
	fmt.Printf("Allowlist: users=%d computers=%d groups=%d trusts=%d\n",
		len(allow.Users), len(allow.Computers), len(allow.Groups), len(allow.Trusts))

	script, err := buildPurgeScript(purgeArgs{
		Apply:            opts.apply,
		SkipCreatorCheck: opts.skipCreatorCheck,
		Classes:          opts.classes,
		Allowlist:        *allow,
	})
	if err != nil {
		return err
	}

	region, err := cfg.ResolveRegionWithInventory(parsed)
	if err != nil {
		return err
	}
	client, err := daws.NewClient(ctx, region)
	if err != nil {
		return err
	}

	mode := "dry-run"
	if opts.apply {
		mode = "APPLY"
	}
	fmt.Printf("Purging unmanaged AD objects on %d DC(s) in %s [%s, classes=%s]\n",
		len(targets), region, mode, strings.Join(opts.classes, ","))

	var totalFlagged, totalRemoved int
	for _, t := range targets {
		r := runPurgeOnDC(ctx, client, t, script, opts.apply)
		if r == nil {
			continue
		}
		totalFlagged += len(r.Users) + len(r.Computers) + len(r.Groups)
		totalRemoved += r.RemovedUsers + r.RemovedComputers + r.RemovedGroups
	}

	if opts.apply {
		fmt.Printf("\nTotal removed: %d (across %d DC(s))\n", totalRemoved, len(targets))
	} else {
		fmt.Printf("\nTotal flagged: %d (dry-run; rerun with --apply to delete)\n", totalFlagged)
	}
	return nil
}

// parsePurgeResult extracts the JSON payload that follows the marker line.
func parsePurgeResult(stdout string) (*purgeResult, error) {
	idx := strings.Index(stdout, purgeResultMarker)
	if idx < 0 {
		return nil, fmt.Errorf("result marker not found in DC output")
	}
	tail := stdout[idx+len(purgeResultMarker):]
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return nil, fmt.Errorf("empty result payload after marker")
	}
	var r purgeResult
	if err := json.Unmarshal([]byte(tail), &r); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}
	return &r, nil
}

func printPurgeResult(r *purgeResult, apply bool) {
	emit := func(label string, names []string) {
		if len(names) == 0 {
			return
		}
		verb := "would remove"
		if apply {
			verb = "removed"
		}
		fmt.Printf("  %s (%s): %s\n", label, verb, strings.Join(names, ", "))
	}
	emit("USERS", r.Users)
	emit("COMPUTERS", r.Computers)
	emit("GROUPS", r.Groups)
	for _, s := range r.Skipped {
		fmt.Printf("  SKIPPED %s %q (%s)\n", s.Class, s.Name, s.Reason)
	}
	for _, e := range r.Errors {
		fmt.Printf("  ERROR: %s\n", e)
	}
	if len(r.Users)+len(r.Computers)+len(r.Groups) == 0 && len(r.Skipped) == 0 && len(r.Errors) == 0 {
		fmt.Println("  clean (no unmanaged objects)")
	}
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
	skipCreator, _ := cmd.Flags().GetBool("skip-creator-check")

	playbooks := adStatePlaybooks
	if playsFlag != "" {
		playbooks = strings.Split(playsFlag, ",")
	}

	fmt.Printf("=== DreadGOAD lab-reset (env=%s) ===\n", cfg.Env)
	fmt.Printf("    skip_purge=%v skip_provision=%v\n", skipPurge, skipProvision)
	fmt.Printf("    plays=%s\n\n", strings.Join(playbooks, ","))

	if !skipPurge {
		fmt.Println("--- Stage 1: purge unmanaged AD objects ---")
		opts := purgeOptions{apply: true, skipCreatorCheck: skipCreator}
		if err := purgeUnmanaged(ctx, cfg, opts); err != nil {
			fmt.Printf("WARN: purge-unmanaged failed: %v (continuing)\n", err)
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
