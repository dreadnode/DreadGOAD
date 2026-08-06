package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/spf13/cobra"
)

// execCmd runs a script on range hosts through the provider's control plane
// (Azure Run Command / AWS SSM), NOT over WinRM.
//
// This is the provider-agnostic sibling of `ssm run` (AWS-only) and `runcmd run`
// (Azure-only), which exist for interactive human use and are left untouched.
// The distinction that matters: the control plane keeps working when a host's
// WinRM listener is down, which is exactly when an operator needs to get in. A
// WinRM/psrp-based path (ansible, `provision`, `health-check`) cannot reach a
// host whose 5985 is refusing — the reason this verb exists, and why it
// replaced the old `diagnose` verb rather than sitting alongside it.
var execCmd = &cobra.Command{
	Use:   "exec",
	Short: "Run a script on range hosts via the cloud control plane",
	Long: `Executes a script on one or more range hosts using the provider's
control-plane channel (Azure Run Command or AWS SSM) rather than WinRM. This
reaches hosts whose WinRM listener is down, so it works when 'provision' and
'health-check' cannot.

Scripts run with administrative privileges. There is no dry run: whatever is
passed to --cmd executes as written.

Provider notes:
  - AWS uses AWS-RunPowerShellScript, so targets must be Windows.
  - Azure infers the interpreter from the VM's OS, so Linux hosts work too.
  - Azure caps output at 4096 bytes per stream and takes ~5-15s per invocation;
    scope queries narrowly rather than dumping large output.`,
	Example: `  dreadgoad exec --hosts dc02 --cmd 'Get-Service WinRM'
  dreadgoad exec --hosts dc01,dc03 --cmd 'w32tm /query /status' --json
  dreadgoad exec --hosts dc02 --cmd 'Start-Service WinRM' --timeout 2m`,
	RunE: runExec,
}

func init() {
	rootCmd.AddCommand(execCmd)

	// No "all" default, unlike `ssm run`/`runcmd run`. This verb is driven by
	// the console agent as well as by hand, and a defaulted fan-out to every
	// host in the range is the wrong failure mode for a command that mutates.
	execCmd.Flags().String("hosts", "", "Comma-separated host names (required)")
	execCmd.Flags().StringP("cmd", "c", "", "Script to execute")
	execCmd.Flags().Bool("json", false, "Emit results as JSON")
	execCmd.Flags().Duration("timeout", 5*time.Minute, "Per-invocation timeout")
	_ = execCmd.MarkFlagRequired("hosts")
	_ = execCmd.MarkFlagRequired("cmd")
}

// execResult is one host's outcome, and the JSON contract consumed by the
// console (see console/backend/summary.py).
type execResult struct {
	Host       string `json:"host"`
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
}

func runExec(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	prov, cfg, err := getProvider(ctx)
	if err != nil {
		return err
	}

	hostsFlag, _ := cmd.Flags().GetString("hosts")
	script, _ := cmd.Flags().GetString("cmd")
	asJSON, _ := cmd.Flags().GetBool("json")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	if strings.TrimSpace(script) == "" {
		return fmt.Errorf("--cmd is empty; nothing to run")
	}

	instances, err := prov.DiscoverInstances(ctx, cfg.Env)
	if err != nil {
		return fmt.Errorf("discover instances: %w", err)
	}
	if len(instances) == 0 {
		return fmt.Errorf("no running instances found for env=%s", cfg.Env)
	}

	targets, err := resolveExecTargets(instances, hostsFlag)
	if err != nil {
		return err
	}

	ids := make([]string, 0, len(targets))
	for _, t := range targets {
		ids = append(ids, t.ID)
	}

	if !asJSON {
		names := make([]string, 0, len(targets))
		for _, t := range targets {
			names = append(names, t.Name)
		}
		fmt.Printf("Running on: %s\n", strings.Join(names, ", "))
		fmt.Printf("Command: %s\n\n", script)
	}

	// Azure schedules run-command deletes in background goroutines; draining
	// keeps them from being orphaned when the process exits. Registered BEFORE
	// the call, not after: a partial failure is exactly when invocations have
	// been issued and the error path would otherwise skip the drain, leaving
	// run-command subresources accumulating on the VM.
	defer func() {
		if d, ok := prov.(provider.Drainer); ok {
			d.Drain()
		}
	}()

	results, err := prov.RunCommandOnMultiple(ctx, ids, script, timeout)
	if err != nil {
		return err
	}

	out := make([]execResult, 0, len(targets))
	failed := 0
	for _, t := range targets {
		r := execResult{Host: t.Name, InstanceID: t.ID, Status: "no result"}
		if res := results[t.ID]; res != nil {
			r.Status, r.Stdout, r.Stderr = res.Status, res.Stdout, res.Stderr
		}
		if !strings.EqualFold(r.Status, "Succeeded") {
			failed++
		}
		out = append(out, r)
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
	} else {
		for _, r := range out {
			fmt.Printf("=== %s ===\n", r.Host)
			fmt.Printf("Status: %s\n", r.Status)
			if r.Stdout != "" {
				fmt.Println(r.Stdout)
			}
			if r.Stderr != "" {
				fmt.Printf("STDERR: %s\n", r.Stderr)
			}
			fmt.Println()
		}
	}

	// A non-zero exit lets the console report the run as failed rather than
	// leaving the agent to infer it from prose in the output.
	if failed > 0 {
		return fmt.Errorf("%d of %d host(s) did not succeed", failed, len(out))
	}
	return nil
}

// resolveExecTargets maps a comma-separated host list onto instances.
//
// Deliberately stricter than filterProviderInstances (used by ssm/runcmd):
// that one substring-matches, so "dc0" silently selects dc01, dc02 AND dc03.
// Here a token must match a host exactly or as a dash-delimited segment of the
// provider's VM name (e.g. "dc02" matches "dreadindex-dreadgoad-DC02-vm"), and
// an unmatched or ambiguous token is an error rather than a warning — for a
// command that mutates, silently hitting the wrong host is the worst outcome.
func resolveExecTargets(instances []provider.Instance, hostsFlag string) ([]provider.Instance, error) {
	if strings.TrimSpace(hostsFlag) == "" {
		return nil, fmt.Errorf("--hosts is required (name the hosts explicitly)")
	}

	var targets []provider.Instance
	seen := map[string]bool{}
	for _, raw := range strings.Split(hostsFlag, ",") {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		matches := matchInstances(instances, token)
		if len(matches) == 0 {
			return nil, fmt.Errorf("host %q not found in env (known: %s)",
				token, strings.Join(instanceNames(instances), ", "))
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("host %q is ambiguous, matches: %s",
				token, strings.Join(instanceNames(matches), ", "))
		}
		if m := matches[0]; !seen[m.ID] {
			seen[m.ID] = true
			targets = append(targets, m)
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("--hosts matched no instances")
	}
	return targets, nil
}

// matchInstances finds instances a token names: an exact name match wins
// outright, otherwise the token must equal one dash-delimited segment of the
// VM name. Segment matching is what lets an operator say "dc02" for
// "dreadindex-dreadgoad-DC02-vm" without "dc0" also matching it.
func matchInstances(instances []provider.Instance, token string) []provider.Instance {
	var segment []provider.Instance
	for _, inst := range instances {
		if strings.EqualFold(inst.Name, token) {
			return []provider.Instance{inst}
		}
		for _, part := range strings.Split(inst.Name, "-") {
			if strings.EqualFold(part, token) {
				segment = append(segment, inst)
				break
			}
		}
	}
	return segment
}

func instanceNames(instances []provider.Instance) []string {
	names := make([]string, 0, len(instances))
	for _, inst := range instances {
		names = append(names, inst.Name)
	}
	sort.Strings(names)
	return names
}
