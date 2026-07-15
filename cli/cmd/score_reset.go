package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/labmap"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var scoreResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Clean file artifacts from the attack box and Windows hosts between agent runs",
	Long: `Removes agent-created files from the Kali attack box (nxc databases,
Kerberos tickets, NTDS dumps, Responder logs, Dreadnode session data)
and Windows hosts (webshells, share drops, temp scripts, registry dumps).

Optionally purges rogue AD computer accounts created during RBCD attacks.

Default mode is dry-run; pass --apply to actually delete.`,
	Example: `  dreadgoad score reset                    # dry-run: show what would be cleaned
  dreadgoad score reset --apply             # delete artifacts
  dreadgoad score reset --apply --purge-ad  # also remove rogue computer accounts
  dreadgoad score reset --skip-kali         # only clean Windows hosts
  dreadgoad score reset --skip-windows      # only clean the attack box`,
	RunE: runScoreReset,
}

func init() {
	scoreCmd.AddCommand(scoreResetCmd)

	scoreResetCmd.Flags().Bool("apply", false, "Actually delete artifacts (default: dry-run)")
	scoreResetCmd.Flags().String("attack-box", "", "Instance ID (AWS) or resource ID (Azure) of the Kali attack box")
	scoreResetCmd.Flags().String("ssh-key", "", "Path to SSH private key for the Kali VM (Azure)")
	scoreResetCmd.Flags().String("ssh-user", "kali", "SSH username for the Kali VM (Azure)")
	scoreResetCmd.Flags().Bool("skip-kali", false, "Skip Kali attack box cleanup")
	scoreResetCmd.Flags().Bool("skip-windows", false, "Skip Windows host cleanup")
	scoreResetCmd.Flags().Bool("purge-ad", false, "Also purge rogue AD computer accounts")
	scoreResetCmd.Flags().Bool("save-report", true, "Archive agent report before cleaning")
	scoreResetCmd.Flags().String("report-output", "", "Path to save archived agent report")
}

// resetResultMarker separates script noise from the structured JSON result.
const resetResultMarker = "---DREADGOAD-RESET-RESULT---"

// resetResult tracks cleanup outcomes for a single target.
// "Issues" covers files, rogue accounts, and rogue group memberships.
type resetResult struct {
	Host          string   `json:"host"`
	IssuesFound   int      `json:"issues_found"`
	IssuesRemoved int      `json:"issues_removed"`
	Errors        []string `json:"errors,omitempty"`
}

func runScoreReset(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	ctx := cmd.Context()

	apply, _ := cmd.Flags().GetBool("apply")
	skipKali, _ := cmd.Flags().GetBool("skip-kali")
	skipWindows, _ := cmd.Flags().GetBool("skip-windows")
	purgeAD, _ := cmd.Flags().GetBool("purge-ad")
	saveReport, _ := cmd.Flags().GetBool("save-report")
	reportOutput, _ := cmd.Flags().GetString("report-output")

	mode := "dry-run"
	if apply {
		mode = "APPLY"
	}

	fmt.Printf("=== DreadGOAD score reset (env=%s) ===\n", cfg.Env)
	fmt.Printf("    mode=%s  skip_kali=%v  skip_windows=%v  purge_ad=%v\n\n", mode, skipKali, skipWindows, purgeAD)

	var errs []string

	// Phase 1: Kali attack box.
	if !skipKali {
		if err := resetKali(ctx, cmd, cfg, apply, saveReport, reportOutput); err != nil {
			color.Red("  Kali cleanup failed: %v", err)
			errs = append(errs, fmt.Sprintf("kali: %v", err))
		}
		fmt.Println()
	}

	// Phase 2: Windows hosts.
	if !skipWindows {
		if hostErrs := resetWindows(ctx, cfg, apply); len(hostErrs) > 0 {
			errs = append(errs, hostErrs...)
		}
		fmt.Println()
	}

	// Phase 3: AD purge (optional).
	if purgeAD {
		fmt.Println("--- Phase 3: AD computer account cleanup ---")
		opts := purgeOptions{apply: apply, classes: []string{"computer"}}
		if err := purgeUnmanaged(ctx, cfg, opts); err != nil {
			color.Red("  AD purge failed: %v", err)
			errs = append(errs, fmt.Sprintf("ad: %v", err))
		}
		fmt.Println()
	}

	fmt.Println("=== score reset complete ===")
	if len(errs) > 0 {
		return fmt.Errorf("%d phase(s) had errors", len(errs))
	}
	return nil
}

// resetKali cleans the Kali attack box via the ShellRunner (SSM or Bastion).
func resetKali(ctx context.Context, cmd *cobra.Command, cfg *config.Config, apply, saveReport bool, reportOutput string) error {
	fmt.Println("--- Phase 1: Kali attack box ---")

	runner, err := buildShellRunner(ctx, cmd, cfg)
	if err != nil {
		return fmt.Errorf("build shell runner: %w", err)
	}

	// Save the agent report before cleaning.
	if saveReport {
		reportContent, err := runner.RunShell(ctx, "cat $HOME/mkultra/agent_run/report.jsonl 2>/dev/null || true", 30*time.Second)
		if err != nil {
			color.Yellow("  WARN: could not fetch report: %v", err)
		} else if strings.TrimSpace(reportContent) != "" {
			if reportOutput == "" {
				reportOutput = fmt.Sprintf("report-%s.jsonl", time.Now().Format("20060102-150405"))
			}
			if err := os.WriteFile(reportOutput, []byte(reportContent), 0o644); err != nil {
				color.Yellow("  WARN: could not save report: %v", err)
			} else {
				lines := strings.Count(strings.TrimSpace(reportContent), "\n") + 1
				color.Green("  Saved agent report (%d lines) -> %s", lines, reportOutput)
			}
		} else {
			fmt.Println("  No agent report found (already cleaned or not yet generated)")
		}
	}

	script := buildKaliCleanupScript(apply)
	out, err := runner.RunShell(ctx, script, 3*time.Minute)
	if err != nil {
		return fmt.Errorf("run cleanup script: %w", err)
	}

	result, parseErr := parseResetResult(out)
	if parseErr != nil {
		fmt.Println(out)
		return fmt.Errorf("parse result: %w", parseErr)
	}

	// Print per-section detail lines (everything before the marker).
	if idx := strings.Index(out, resetResultMarker); idx > 0 {
		detail := strings.TrimSpace(out[:idx])
		if detail != "" {
			fmt.Println(detail)
		}
	}

	printResetSummary("Kali", result, apply)
	return nil
}

// buildKaliCleanupScript generates the shell script for Kali artifact cleanup.
// Uses $HOME so it works for both ssm-user (AWS) and kali (Azure).
func buildKaliCleanupScript(apply bool) string {
	type cleanTarget struct {
		label string
		find  string
		clean string
	}

	targets := []cleanTarget{
		{
			label: "nxc tmp",
			find:  `find $HOME/.nxc/tmp -type f 2>/dev/null | wc -l`,
			clean: `rm -rf $HOME/.nxc/tmp/* 2>/dev/null`,
		},
		{
			label: "nxc logs (LSA/SAM/NTDS/DPAPI/bloodhound)",
			find:  `find $HOME/.nxc/logs -type f 2>/dev/null | wc -l`,
			clean: `rm -rf $HOME/.nxc/logs/* 2>/dev/null`,
		},
		{
			label: "nxc lsassy tickets",
			find:  `find $HOME/.nxc/modules/lsassy -type f 2>/dev/null | wc -l`,
			clean: `rm -rf $HOME/.nxc/modules/lsassy/* 2>/dev/null`,
		},
		{
			label: "nxc spider_plus",
			find:  `find $HOME/.nxc/modules/nxc_spider_plus -type f 2>/dev/null | wc -l`,
			clean: `rm -rf $HOME/.nxc/modules/nxc_spider_plus/* 2>/dev/null`,
		},
		{
			label: "nxc pre2k",
			find:  `find $HOME/.nxc/modules/pre2k -type f 2>/dev/null | wc -l`,
			clean: `rm -rf $HOME/.nxc/modules/pre2k/* 2>/dev/null`,
		},
		{
			label: "nxc workspace databases",
			find:  `find $HOME/.nxc/workspaces -name "*.db" -type f 2>/dev/null | wc -l`,
			clean: `rm -f $HOME/.nxc/workspaces/default/*.db 2>/dev/null`,
		},
		{
			label: "cme data",
			find:  `find $HOME/.cme -type f 2>/dev/null | wc -l`,
			clean: `rm -rf $HOME/.cme/ 2>/dev/null`,
		},
		{
			label: "Responder logs",
			find:  `find $HOME/Responder/logs -type f 2>/dev/null | wc -l`,
			clean: `rm -rf $HOME/Responder/logs/* 2>/dev/null`,
		},
		{
			label: "dreadnode sessions",
			find:  `find $HOME/.dreadnode/sessions -type f 2>/dev/null | wc -l`,
			clean: `rm -rf $HOME/.dreadnode/sessions/* 2>/dev/null; rm -f $HOME/.dreadnode/prompt-history.jsonl 2>/dev/null`,
		},
		{
			label: "agent report",
			find:  `test -f $HOME/mkultra/agent_run/report.jsonl && echo 1 || echo 0`,
			clean: `rm -f $HOME/mkultra/agent_run/report.jsonl 2>/dev/null`,
		},
		{
			label: "/tmp artifacts",
			find:  `find /tmp -maxdepth 1 -type f \( -name "*.txt" -o -name "*.ps1" -o -name "*.bat" -o -name "*.pfx" -o -name "*.pem" -o -name "*.exe" -o -name "*.hive" -o -name "*.ccache" -o -name "*.kirbi" -o -name "*.keytab" -o -name "*.zip" -o -name "*.ntds" -o -name "*.sam" -o -name "*.b64" \) 2>/dev/null | wc -l`,
			clean: `find /tmp -maxdepth 1 -type f \( -name "*.txt" -o -name "*.ps1" -o -name "*.bat" -o -name "*.pfx" -o -name "*.pem" -o -name "*.exe" -o -name "*.hive" -o -name "*.ccache" -o -name "*.kirbi" -o -name "*.keytab" -o -name "*.zip" -o -name "*.ntds" -o -name "*.sam" -o -name "*.b64" \) -delete 2>/dev/null`,
		},
		{
			label: "stray certs/tickets in home",
			find:  `find $HOME -maxdepth 3 \( -name "*.ccache" -o -name "*.kirbi" -o -name "*.keytab" -o -name "*.pfx" \) ! -path "*/mkultra/*" ! -path "*/.local/*" 2>/dev/null | wc -l`,
			clean: `find $HOME -maxdepth 3 \( -name "*.ccache" -o -name "*.kirbi" -o -name "*.keytab" -o -name "*.pfx" \) ! -path "*/mkultra/*" ! -path "*/.local/*" -delete 2>/dev/null`,
		},
	}

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\ntotal_found=0\ntotal_removed=0\n")

	for i, t := range targets {
		sb.WriteString(fmt.Sprintf("\n# %s\ncount_%d=$(%s)\ntotal_found=$((total_found + count_%d))\n", t.label, i, t.find, i))
		if apply {
			// Count before, delete, count after — removed = before - after.
			sb.WriteString(fmt.Sprintf("if [ \"$count_%d\" -gt 0 ] 2>/dev/null; then\n", i))
			sb.WriteString(fmt.Sprintf("  %s\n", t.clean))
			sb.WriteString(fmt.Sprintf("  after_%d=$(%s)\n", i, t.find))
			sb.WriteString(fmt.Sprintf("  removed_%d=$((count_%d - after_%d))\n", i, i, i))
			sb.WriteString(fmt.Sprintf("  total_removed=$((total_removed + removed_%d))\n", i))
			sb.WriteString("fi\n")
		}
		sb.WriteString(fmt.Sprintf("echo \"  %s: $count_%d files\"\n", t.label, i))
	}

	sb.WriteString(fmt.Sprintf("\necho '%s'\n", resetResultMarker))
	sb.WriteString(`printf '{"host":"kali","issues_found":%d,"issues_removed":%d}\n' "$total_found" "$total_removed"`)
	sb.WriteString("\n")

	return sb.String()
}

// resetWindows cleans file artifacts from all Windows hosts in parallel.
func resetWindows(ctx context.Context, cfg *config.Config, apply bool) []string {
	fmt.Println("--- Phase 2: Windows hosts ---")

	infra, err := requireInfra(ctx)
	if err != nil {
		msg := fmt.Sprintf("windows: infrastructure setup: %v", err)
		color.Red("  %s", msg)
		return []string{msg}
	}

	type hostJob struct {
		role       string
		instanceID string
		hc         labmap.HostConfig
	}
	var jobs []hostJob
	for _, role := range infra.Lab.WindowsHosts() {
		upper := strings.ToUpper(role)
		instanceID, ok := infra.HostMap[upper]
		if !ok {
			color.Yellow("  %s: no instance ID (skipping)", upper)
			continue
		}
		hc, ok := hostConfigByRole(infra.Lab, role)
		if !ok {
			color.Yellow("  %s: no host config (skipping)", upper)
			continue
		}
		jobs = append(jobs, hostJob{role: role, instanceID: instanceID, hc: hc})
	}

	type hostResult struct {
		hostname string
		output   string
		err      error
	}

	results := make([]hostResult, len(jobs))
	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Add(1)
		go func(idx int, j hostJob) {
			defer wg.Done()
			out, err := runWindowsCleanup(ctx, infra.Provider, j.instanceID, j.hc, infra.Lab, apply)
			results[idx] = hostResult{
				hostname: strings.ToUpper(j.hc.Hostname),
				output:   out,
				err:      err,
			}
		}(i, job)
	}
	wg.Wait()

	// Print results in order.
	var errs []string
	for _, r := range results {
		fmt.Printf("=== %s ===\n", r.hostname)
		if r.err != nil {
			color.Red("  %v", r.err)
			errs = append(errs, fmt.Sprintf("%s: %v", r.hostname, r.err))
			continue
		}
		parsed, parseErr := parseResetResult(r.output)
		if parseErr != nil {
			if r.output != "" {
				fmt.Println(r.output)
			}
			color.Red("  parse error: %v", parseErr)
			errs = append(errs, fmt.Sprintf("%s: parse: %v", r.hostname, parseErr))
			continue
		}
		if idx := strings.Index(r.output, resetResultMarker); idx > 0 {
			detail := strings.TrimSpace(r.output[:idx])
			if detail != "" {
				fmt.Println(detail)
			}
		}
		printResetSummary(r.hostname, parsed, apply)
	}
	return errs
}

// windowsResetArgs is the JSON payload sent to each Windows host's PowerShell.
type windowsResetArgs struct {
	Apply            bool                `json:"Apply"`
	AllowedFiles     []string            `json:"AllowedFiles"`
	CleanIIS         bool                `json:"CleanIIS"`
	CleanShares      bool                `json:"CleanShares"`
	CheckLocalUsers  bool                `json:"CheckLocalUsers"`  // false on DCs (use --purge-ad instead)
	AllowedUsers     []string            `json:"AllowedUsers"`     // expected local accounts
	ExpectedGroups   map[string][]string `json:"ExpectedGroups"`   // group -> expected members (for membership diff)
	BlacklistedExes  []string            `json:"BlacklistedExes"`  // attack tool executables to remove from Windows\Temp
}

// knownAttackToolExes is a blacklist of executables commonly dropped by agents.
// Only these specific filenames are removed from Windows\Temp — other .exe files
// (provisioning tools, installers) are left alone.
var knownAttackToolExes = []string{
	"godpotato.exe",
	"godpotato-net4.exe",
	"godpotato-net2.exe",
	"godpotato-net35.exe",
	"printspoofer.exe",
	"printspoofer64.exe",
	"printspoofer32.exe",
	"juicypotato.exe",
	"roguepotato.exe",
	"sweetpotato.exe",
	"efspotato.exe",
	"rubeus.exe",
	"mimikatz.exe",
	"sharphound.exe",
	"certify.exe",
	"certipy.exe",
	"seatbelt.exe",
	"sharpview.exe",
	"winpeas.exe",
	"winpeasx64.exe",
	"winpeasx86.exe",
	"chisel.exe",
	"ligolo-ng.exe",
	"nc.exe",
	"nc64.exe",
	"ncat.exe",
	"plink.exe",
	"procdump.exe",
	"procdump64.exe",
	"psexec.exe",
	"psexec64.exe",
	"lazagne.exe",
	"sharpkatz.exe",
	"nanodump.exe",
	"runascs.exe",
	"sharpsccm.exe",
	"snaffler.exe",
	"kerbrute.exe",
	"bloodhound.exe",
	"adpeas.exe",
	"whisker.exe",
	"coercer.exe",
	"petitpotam.exe",
	"spoolsample.exe",
	"sharpmad.exe",
	"powermad.exe",
	"standandalone.exe",
	"invoke-mimikatz.exe",
}

// defaultLocalUsers are Windows built-in and provisioning accounts that should
// never be flagged as rogue.
var defaultLocalUsers = []string{
	"administrator",
	"guest",
	"defaultaccount",
	"wdagutilityaccount",
	"ssm-user",
	"ansible",
	"goadmin",
}

// runWindowsCleanup executes the cleanup script on a single Windows host
// and returns the raw stdout. Parsing is done by the caller so results
// can be printed in deterministic order after parallel execution.
func runWindowsCleanup(ctx context.Context, prov provider.Provider, instanceID string, hc labmap.HostConfig, lab *labmap.LabMap, apply bool) (string, error) {
	args := windowsResetArgs{
		Apply:           apply,
		AllowedFiles:    parseFileAllowlist(hc),
		CleanIIS:        hasIISContent(hc),
		CleanShares:     hasShareContent(hc),
		CheckLocalUsers: hc.Type != "dc",
		AllowedUsers:    buildLocalUserAllowlist(hc, lab),
		ExpectedGroups:  buildExpectedGroups(hc),
		BlacklistedExes: knownAttackToolExes,
	}

	raw, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("marshal args: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	script := fmt.Sprintf(windowsCleanupScriptTpl, encoded)

	result, err := prov.RunCommand(ctx, instanceID, script, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("run command: %w", err)
	}
	return result.Stdout, nil
}

// windowsCleanupScriptTpl is the PowerShell template for Windows host cleanup.
// The %s placeholder receives base64-encoded windowsResetArgs JSON.
const windowsCleanupScriptTpl = `$ErrorActionPreference = "Continue"
$argsJson = [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + `%s` + `'))
$cfg = $argsJson | ConvertFrom-Json
$apply = [bool]$cfg.Apply

$allowedFiles = @{}
foreach ($f in $cfg.AllowedFiles) { $allowedFiles[$f.ToLower()] = $true }
foreach ($f in @('desktop.ini', '.gitkeep')) { $allowedFiles[$f.ToLower()] = $true }

$totalFound = 0
$totalRemoved = 0
$errors = @()

if ([bool]$cfg.CleanIIS -and (Test-Path 'C:\inetpub\wwwroot\upload')) {
    $files = Get-ChildItem 'C:\inetpub\wwwroot\upload' -File -ErrorAction SilentlyContinue |
        Where-Object { -not $allowedFiles.ContainsKey($_.Name.ToLower()) }
    $count = ($files | Measure-Object).Count
    $totalFound += $count
    if ($apply -and $count -gt 0) {
        foreach ($f in $files) {
            try { Remove-Item $f.FullName -Force -ErrorAction Stop; $totalRemoved++ }
            catch { $errors += "iis: $($f.Name): $_" }
        }
    }
    Write-Output "  IIS upload: $count files"
}

if ([bool]$cfg.CleanShares) {
    foreach ($shareDir in @('C:\shares\all', 'C:\shares\public')) {
        if (-not (Test-Path $shareDir)) { continue }
        $files = Get-ChildItem $shareDir -File -ErrorAction SilentlyContinue |
            Where-Object { -not $allowedFiles.ContainsKey($_.Name.ToLower()) }
        $count = ($files | Measure-Object).Count
        $totalFound += $count
        $dirName = Split-Path $shareDir -Leaf
        if ($apply -and $count -gt 0) {
            foreach ($f in $files) {
                try { Remove-Item $f.FullName -Force -ErrorAction Stop; $totalRemoved++ }
                catch { $errors += "share ${dirName}: $($f.Name): $_" }
            }
        }
        Write-Output "  shares\${dirName}: $count files"

        $subDirs = Get-ChildItem $shareDir -Directory -ErrorAction SilentlyContinue |
            Where-Object { -not $allowedFiles.ContainsKey($_.Name.ToLower()) }
        if ($apply -and $subDirs) {
            foreach ($d in $subDirs) {
                try { Remove-Item $d.FullName -Recurse -Force -ErrorAction Stop }
                catch { $errors += "share ${dirName}: dir $($d.Name): $_" }
            }
        }
    }
}

$suspiciousExts = @('.ps1','.bat','.cmd','.vbs','.js','.dll','.kirbi','.ccache','.pfx','.hive','.aspx','.asp','.zip','.b64','.com','.scr','.msi')
$blacklistedExes = @{}
foreach ($e in $cfg.BlacklistedExes) { $blacklistedExes[$e.ToLower()] = $true }
$tempFiles = Get-ChildItem 'C:\Windows\Temp' -File -ErrorAction SilentlyContinue |
    Where-Object { ($suspiciousExts -contains $_.Extension) -or ($blacklistedExes.ContainsKey($_.Name.ToLower())) }
$count = ($tempFiles | Measure-Object).Count
$totalFound += $count
if ($apply -and $count -gt 0) {
    foreach ($f in $tempFiles) {
        try { Remove-Item $f.FullName -Force -ErrorAction Stop; $totalRemoved++ }
        catch { $errors += "temp: $($f.Name): $_" }
    }
}
Write-Output "  Windows\Temp: $count files"

$pubFiles = Get-ChildItem 'C:\Users\Public' -File -Recurse -ErrorAction SilentlyContinue |
    Where-Object { $_.Name -ne 'desktop.ini' }
$count = ($pubFiles | Measure-Object).Count
$totalFound += $count
if ($apply -and $count -gt 0) {
    foreach ($f in $pubFiles) {
        try { Remove-Item $f.FullName -Force -ErrorAction Stop; $totalRemoved++ }
        catch { $errors += "public: $($f.Name): $_" }
    }
}
Write-Output "  Users\Public: $count files"

if ([bool]$cfg.CheckLocalUsers) {
    $allowedUsers = @{}
    foreach ($u in $cfg.AllowedUsers) { $allowedUsers[$u.ToLower()] = $true }
    $rogueUsers = @()
    try {
        $localUsers = Get-LocalUser -ErrorAction Stop
        foreach ($u in $localUsers) {
            if (-not $allowedUsers.ContainsKey($u.Name.ToLower())) {
                $rogueUsers += $u.Name
                $totalFound++
                if ($apply) {
                    try { Remove-LocalUser -Name $u.Name -ErrorAction Stop; $totalRemoved++ }
                    catch { $errors += "local-user: $($u.Name): $_" }
                }
            }
        }
    } catch {
        $errors += "Get-LocalUser: $_"
    }
    if ($rogueUsers.Count -gt 0) {
        Write-Output "  Rogue local accounts: $($rogueUsers -join ', ')"
    } else {
        Write-Output "  Local accounts: clean"
    }
}

if ($cfg.ExpectedGroups -and [bool]$cfg.CheckLocalUsers) {
    $groupIssues = @()
    foreach ($prop in $cfg.ExpectedGroups.PSObject.Properties) {
        $groupName = $prop.Name
        # Build a set of expected usernames (strip domain prefix for comparison).
        $expectedUsers = @{}
        foreach ($m in $prop.Value) {
            $u = $m.ToLower()
            if ($u -match '\\(.+)$') { $u = $Matches[1] }
            $expectedUsers[$u] = $true
        }
        try {
            $actual = Get-LocalGroupMember -Group $groupName -ErrorAction Stop
        } catch {
            $errors += "Get-LocalGroupMember ${groupName}: $_"
            continue
        }
        foreach ($member in $actual) {
            # Extract just the username part (strip DOMAIN\ or COMPUTERNAME\ prefix).
            $memberFull = $member.Name.ToLower()
            $memberShort = if ($memberFull -match '\\(.+)$') { $Matches[1] } else { $memberFull }
            if ($expectedUsers.ContainsKey($memberShort)) { continue }
            # Skip well-known built-in principals.
            $builtIn = @('administrator','domain admins','enterprise admins')
            if ($builtIn -contains $memberShort) { continue }
            $groupIssues += "${groupName}: $($member.Name)"
            $totalFound++
            if ($apply) {
                try {
                    Remove-LocalGroupMember -Group $groupName -Member $member.Name -ErrorAction Stop
                    $totalRemoved++
                } catch { $errors += "remove-member ${groupName}\$($member.Name): $_" }
            }
        }
    }
    if ($groupIssues.Count -gt 0) {
        Write-Output "  Rogue group members: $($groupIssues -join '; ')"
    } else {
        Write-Output "  Group membership: clean"
    }
}

Write-Output '` + resetResultMarker + `'
$errJson = '[]'
if ($errors.Count -gt 0) {
    $escaped = @()
    foreach ($e in $errors) {
        $escaped += ('"' + ($e -replace '[\\"]', '\$0') + '"')
    }
    $errJson = '[' + ($escaped -join ',') + ']'
}
$hostName = $env:COMPUTERNAME -replace '[\\"]', ''
Write-Output ('{"host":"' + $hostName + '","issues_found":' + $totalFound + ',"issues_removed":' + $totalRemoved + ',"errors":' + $errJson + '}')
`

// parseFileAllowlist extracts destination file basenames from VulnsVars["files"].
func parseFileAllowlist(hc labmap.HostConfig) []string {
	// Always include infrastructure files from vulnerability provisioning.
	allowed := []string{"Documents.searchConnector-ms", "test.scf"}

	raw, ok := hc.VulnsVars["files"]
	if !ok {
		return allowed
	}

	var entries map[string]struct {
		Dest string `json:"dest"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return allowed
	}
	for _, e := range entries {
		base := filepath.Base(e.Dest)
		// Skip directory-only destinations like "C:\inetpub\" where Base returns the dir name.
		if base == "" || base == "." || !strings.Contains(base, ".") {
			continue
		}
		allowed = append(allowed, base)
	}
	return allowed
}

// buildLocalUserAllowlist constructs the list of expected local accounts for a host.
// Includes Windows built-ins, provisioning accounts, and any accounts defined in the
// lab config's local_groups (users granted local admin, RDP, etc.).
func buildLocalUserAllowlist(hc labmap.HostConfig, lab *labmap.LabMap) []string {
	seen := map[string]bool{}
	for _, u := range defaultLocalUsers {
		seen[strings.ToLower(u)] = true
	}
	// Add the lab admin user (e.g. "goadmin" or "administrator").
	if lab != nil && lab.AdminUser != "" {
		seen[strings.ToLower(lab.AdminUser)] = true
	}
	// Add users referenced in this host's local_groups config.
	for _, members := range hc.LocalGroups {
		for _, m := range members {
			// Members can be "domain\user" — extract the user part.
			if idx := strings.LastIndex(m, "\\"); idx >= 0 {
				m = m[idx+1:]
			}
			seen[strings.ToLower(m)] = true
		}
	}
	allowed := make([]string, 0, len(seen))
	for u := range seen {
		allowed = append(allowed, u)
	}
	return allowed
}

// buildExpectedGroups returns the expected local group memberships from the lab config.
// Only includes groups explicitly configured in local_groups. Provisioning accounts
// (ansible, ssm-user, goadmin, Administrator) are always added to Administrators.
func buildExpectedGroups(hc labmap.HostConfig) map[string][]string {
	if len(hc.LocalGroups) == 0 {
		return nil
	}
	groups := make(map[string][]string, len(hc.LocalGroups))
	for group, members := range hc.LocalGroups {
		normalized := make([]string, 0, len(members)+4)
		for _, m := range members {
			normalized = append(normalized, strings.ToLower(m))
		}
		// Provisioning accounts always have local admin.
		if strings.EqualFold(group, "Administrators") {
			for _, u := range []string{"administrator", "ansible", "ssm-user", "goadmin"} {
				normalized = append(normalized, u)
			}
		}
		groups[group] = normalized
	}
	return groups
}

// hasIISContent checks if the host has files destined for the IIS upload directory.
func hasIISContent(hc labmap.HostConfig) bool {
	raw, ok := hc.VulnsVars["files"]
	if !ok {
		return false
	}
	var entries map[string]struct {
		Dest string `json:"dest"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return false
	}
	for _, e := range entries {
		lower := strings.ToLower(e.Dest)
		if strings.HasPrefix(lower, `c:\inetpub`) || strings.HasPrefix(lower, `c:\\inetpub`) {
			return true
		}
	}
	return false
}

// hasShareContent checks if the host has share-related vulnerabilities.
func hasShareContent(hc labmap.HostConfig) bool {
	for _, v := range hc.Vulns {
		switch v {
		case "shares", "directory", "openshares", "files":
			return true
		}
	}
	return false
}

// parseResetResult extracts the JSON payload after the marker line.
func parseResetResult(stdout string) (*resetResult, error) {
	idx := strings.Index(stdout, resetResultMarker)
	if idx < 0 {
		return nil, fmt.Errorf("result marker not found in output")
	}
	tail := strings.TrimSpace(stdout[idx+len(resetResultMarker):])
	if tail == "" {
		return nil, fmt.Errorf("empty result after marker")
	}
	// Take only the first line (avoid trailing noise).
	if nl := strings.Index(tail, "\n"); nl > 0 {
		tail = tail[:nl]
	}
	var r resetResult
	if err := json.Unmarshal([]byte(tail), &r); err != nil {
		return nil, fmt.Errorf("unmarshal: %w (raw: %s)", err, tail)
	}
	return &r, nil
}

// printResetSummary prints the cleanup summary for a single host.
func printResetSummary(host string, r *resetResult, apply bool) {
	verb := "would remove"
	if apply {
		verb = "removed"
	}
	if r.IssuesFound == 0 {
		color.Green("  clean (no artifacts)")
	} else {
		fmt.Printf("  Total: %d issues found, %d %s\n", r.IssuesFound, r.IssuesRemoved, verb)
	}
	for _, e := range r.Errors {
		color.Red("  ERROR: %s", e)
	}
}
