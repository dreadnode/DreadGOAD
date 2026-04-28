// Package validate provides vulnerability validation checks for GOAD lab
// instances. It runs PowerShell commands against Windows hosts via AWS SSM
// and records pass/fail/warn results in a structured [Report].
package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dreadnode/dreadgoad/internal/labmap"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/fatih/color"
)

// Result represents a single check result.
type Result struct {
	Status   string `json:"status"` // PASS, FAIL, WARN, SKIP, INFO
	Category string `json:"category"`
	Name     string `json:"name"`
	Detail   string `json:"detail,omitempty"`
}

// Report holds all validation results.
type Report struct {
	Date     string   `json:"validation_date"`
	Env      string   `json:"environment"`
	Total    int      `json:"total_checks"`
	Passed   int      `json:"passed"`
	Failed   int      `json:"failed"`
	Warnings int      `json:"warnings"`
	Results  []Result `json:"checks"`
}

// Validator runs vulnerability checks against GOAD instances.
type Validator struct {
	mu       sync.Mutex
	provider provider.Provider
	log      *slog.Logger
	env      string
	verbose  bool
	report   Report
	hosts    map[string]string // hostname -> instance ID
	lab      *labmap.LabMap

	// failures counts consecutive runPS failures per host. A single transient
	// SSM/WinRM hiccup must not poison the rest of the run, so we only mark a
	// host dead after deadThreshold sustained failures. Successful calls
	// reset the counter.
	failures sync.Map // hostname -> *atomic.Int64

	// dead tracks hosts that have crossed the failure threshold; entries are
	// added exactly once via sync.Map.LoadOrStore so the "marking host dead"
	// warning fires once per host even under heavy concurrent fan-out.
	dead sync.Map // hostname -> struct{}
}

// NewValidator creates a new Validator.
func NewValidator(prov provider.Provider, env string, verbose bool, log *slog.Logger, lab *labmap.LabMap) *Validator {
	if log == nil {
		log = slog.Default()
	}
	return &Validator{
		provider: prov,
		log:      log,
		env:      env,
		verbose:  verbose,
		hosts:    make(map[string]string),
		lab:      lab,
		report: Report{
			Date: time.Now().UTC().Format(time.RFC3339),
			Env:  env,
		},
	}
}

// DiscoverHosts finds GOAD instances and maps hostnames to instance IDs.
// Host roles are derived from the lab config, not hardcoded.
func (v *Validator) DiscoverHosts(ctx context.Context) error {
	instances, err := v.provider.DiscoverInstances(ctx, v.env)
	if err != nil {
		return fmt.Errorf("discover instances: %w", err)
	}

	for _, inst := range instances {
		name := strings.ToUpper(inst.Name)
		for _, role := range v.lab.HostRoles() {
			host := strings.ToUpper(role)
			if strings.Contains(name, host) {
				v.hosts[host] = inst.ID
				v.addResult(os.Stdout, "PASS", "Discovery", fmt.Sprintf("Found %s", host), inst.ID)
			}
		}
	}

	for _, role := range v.lab.DCs() {
		host := strings.ToUpper(role)
		if _, ok := v.hosts[host]; !ok {
			v.addResult(os.Stdout, "FAIL", "Discovery", fmt.Sprintf("Missing %s", host), "not found")
			return fmt.Errorf("required host %s not found", host)
		}
	}
	return nil
}

// maxConcurrentChecks limits how many check categories run in parallel.
// This bounds concurrent calls to the underlying provider (AWS SSM, Ludus
// SSH+ansible, etc.). Tuned to keep all 28 default checks issuing work
// simultaneously while staying under typical provider throttle limits.
const maxConcurrentChecks = 16

// checkFunc is the signature for all check functions.
type checkFunc func(context.Context, io.Writer)

// runChecks executes check functions concurrently, printing each check's
// output in submission order as it completes (per-check buffered channels).
func (v *Validator) runChecks(ctx context.Context, checks []checkFunc) {
	chs := make([]chan []byte, len(checks))
	sem := make(chan struct{}, maxConcurrentChecks)

	for i, fn := range checks {
		chs[i] = make(chan []byte, 1)
		go func(ch chan<- []byte, f checkFunc) {
			sem <- struct{}{}
			defer func() { <-sem }()
			var buf bytes.Buffer
			f(ctx, &buf)
			ch <- buf.Bytes()
		}(chs[i], fn)
	}

	for _, ch := range chs {
		if _, err := os.Stdout.Write(<-ch); err != nil {
			if errors.Is(err, syscall.EPIPE) {
				return
			}
			fmt.Fprintf(os.Stderr, "validate: stdout write failed: %v\n", err)
			return
		}
	}
}

// RunQuickChecks runs a subset of critical checks.
func (v *Validator) RunQuickChecks(ctx context.Context) {
	v.runChecks(ctx, []checkFunc{
		v.checkCredentialDiscovery,
		v.checkNetworkMisconfigs,
		v.checkMSSQL,
		v.checkADCS,
		v.checkADCSESC7,
		v.checkADCSESC6,
		v.checkDomainTrusts,
		v.checkServices,
		v.checkScheduledTasks,
	})
}

// RunAllChecks executes all vulnerability validation checks.
func (v *Validator) RunAllChecks(ctx context.Context) {
	v.runChecks(ctx, []checkFunc{
		// Section 2 — Configured Users
		v.checkConfiguredUsers,
		// Section 3 — Configured Groups
		v.checkConfiguredGroups,
		// Section 5 — Credential Discovery
		v.checkCredentialDiscovery,
		v.checkUsernamePasswordEqual,
		v.checkAutologonRegistry,
		v.checkCmdkeyCredentials,
		v.checkSysvolPlaintext,
		v.checkShareFilePlaintext,
		v.checkSharePermissions,
		v.checkAdministratorFolder,
		// Section 6 — Network Poisoning / Hardening
		v.checkKerberosAttacks,
		v.checkNetworkMisconfigs,
		v.checkAnonymousSMB,
		v.checkSMBv1,
		v.checkCredSSP,
		v.checkWebDAVRedirector,
		v.checkDelegation,
		v.checkMachineAccountQuota,
		// Section 7 — MSSQL
		v.checkMSSQL,
		// Section 8 — ADCS
		v.checkADCS,
		v.checkADCSESC1,
		v.checkADCSESC2,
		v.checkADCSESC3,
		v.checkADCSESC4,
		v.checkADCSESC6,
		v.checkADCSESC7,
		v.checkADCSESC9,
		v.checkADCSESC10,
		v.checkADCSESC11,
		v.checkADCSESC13,
		v.checkADCSESC15,
		v.checkCertEnrollShare,
		// ACLs / trusts / services
		v.checkACLPermissions,
		v.checkDomainTrusts,
		v.checkSIDFiltering,
		v.checkSIDHistory,
		v.checkServices,
		v.checkScheduledTasks,
		v.checkLLMNR,
		v.checkGPOAbuse,
		v.checkGMSA,
		v.checkLAPS,
		v.checkSMBShares,
		v.checkFirewallDisabled,
		v.checkPasswordPolicy,
		v.checkLDAPSigning,
		v.checkRunAsPPL,
		// Section 10 — IIS
		v.checkIISUploadPermissions,
		// Section 11 — Local Admin Access Map
		v.checkLocalAdmins,
		// Section 13 — CVE Patch Status
		v.checkCVEPatches,
		// Section 14 — Admin Shares
		v.checkAdminShares,
		// Section 16 — DNS / Audit
		v.checkDNSConditionalForwarder,
		v.checkDCSACLAudit,
		v.checkLDAPDiagnosticLogging,
		v.checkASRRules,
	})
}

// GetReport returns the current report.
func (v *Validator) GetReport() *Report {
	v.report.Total = v.report.Passed + v.report.Failed + v.report.Warnings
	return &v.report
}

// SaveReport writes the report to a JSON file.
func (v *Validator) SaveReport(path string) error {
	data, err := json.MarshalIndent(v.GetReport(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// runPSTimeout is the per-attempt budget for a single SSM/WinRM PowerShell call.
// Concurrent fan-out across 16 checks frequently pushes individual SSM calls
// past 30s under load; 90s leaves headroom without letting truly hung hosts
// stall the run forever (those are caught by the dead-host threshold below).
const runPSTimeout = 90 * time.Second

// runPSAttempts is the per-call retry budget for transient errors (SSM API
// throttles, momentary WinRM hiccups). Total worst-case wall time per dead
// call is runPSTimeout * runPSAttempts.
const runPSAttempts = 3

// deadThreshold is the number of *fully retried* runPS calls that must fail
// before a host is declared dead and skipped for the rest of the run. One
// transient blip should not turn the next ~30 checks on a host into bogus
// failures.
const deadThreshold = 3

func (v *Validator) runPS(ctx context.Context, host, command string) string {
	instanceID, ok := v.hosts[host]
	if !ok {
		v.log.Warn("host not found", "host", host)
		return ""
	}
	if v.verbose {
		v.log.Debug("running PS command", "host", host, "command", command)
	}

	if _, dead := v.dead.Load(host); dead {
		return ""
	}

	var lastErr error
	for attempt := 1; attempt <= runPSAttempts; attempt++ {
		result, err := v.provider.RunCommand(ctx, instanceID, command, runPSTimeout)
		if err == nil {
			if c, loaded := v.failures.Load(host); loaded {
				c.(*atomic.Int64).Store(0)
			}
			return result.Stdout
		}
		lastErr = err
		if attempt < runPSAttempts {
			select {
			case <-ctx.Done():
				return ""
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
	}

	counterAny, _ := v.failures.LoadOrStore(host, &atomic.Int64{})
	n := counterAny.(*atomic.Int64).Add(1)
	if n >= deadThreshold {
		if _, alreadyDead := v.dead.LoadOrStore(host, struct{}{}); !alreadyDead {
			v.log.Warn("PS command failed repeatedly; marking host dead for remainder of run",
				"host", host, "failures", n, "error", lastErr)
		}
	} else {
		v.log.Warn("PS command failed", "host", host, "failures", n, "error", lastErr)
	}
	return ""
}

func (v *Validator) addResult(w io.Writer, status, category, name, detail string) {
	r := Result{Status: status, Category: category, Name: name, Detail: detail}

	v.mu.Lock()
	v.report.Results = append(v.report.Results, r)
	switch status {
	case "PASS":
		v.report.Passed++
	case "FAIL":
		v.report.Failed++
	case "WARN":
		v.report.Warnings++
	}
	v.mu.Unlock()

	switch status {
	case "PASS":
		_, _ = fmt.Fprint(w, color.GreenString("  ✓ %s\n", name))
	case "FAIL":
		_, _ = fmt.Fprint(w, color.RedString("  ✗ %s\n", name))
	case "WARN":
		_, _ = fmt.Fprint(w, color.YellowString("  ⚠ %s\n", name))
	case "SKIP":
		_, _ = fmt.Fprint(w, color.CyanString("  ⊘ %s\n", name))
	case "INFO":
		_, _ = fmt.Fprintf(w, "  ℹ %s\n", name)
	}
}

func (v *Validator) hasHost(host string) bool {
	_, ok := v.hosts[host]
	return ok
}
