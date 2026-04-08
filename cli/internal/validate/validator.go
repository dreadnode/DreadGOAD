package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	daws "github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/dreadnode/dreadgoad/internal/labmap"
	"github.com/fatih/color"
)

// Result represents a single check result.
type Result struct {
	Status   string `json:"status"` // PASS, FAIL, WARN, SKIP, INFO
	Category string `json:"category"`
	Name     string `json:"name"`
	Detail   string `json:"detail,omitzero"`
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
	mu      sync.Mutex
	client  *daws.Client
	log     *slog.Logger
	env     string
	verbose bool
	report  Report
	hosts   map[string]string // hostname -> instance ID
	lab     *labmap.LabMap
}

// NewValidator creates a new Validator.
func NewValidator(client *daws.Client, env string, verbose bool, log *slog.Logger, lab *labmap.LabMap) *Validator {
	if log == nil {
		log = slog.Default()
	}
	return &Validator{
		client:  client,
		log:     log,
		env:     env,
		verbose: verbose,
		hosts:   make(map[string]string),
		lab:     lab,
		report: Report{
			Date: time.Now().UTC().Format(time.RFC3339),
			Env:  env,
		},
	}
}

// DiscoverHosts finds GOAD instances and maps hostnames to instance IDs.
// Host roles are derived from the lab config, not hardcoded.
func (v *Validator) DiscoverHosts(ctx context.Context) error {
	instances, err := v.client.DiscoverInstances(ctx, v.env)
	if err != nil {
		return fmt.Errorf("discover instances: %w", err)
	}

	// Match instances to host roles from config
	for _, inst := range instances {
		name := strings.ToUpper(inst.Name)
		for _, role := range v.lab.HostRoles() {
			host := strings.ToUpper(role)
			if strings.Contains(name, host) {
				v.hosts[host] = inst.InstanceID
				v.addResult(os.Stdout, "PASS", "Discovery", fmt.Sprintf("Found %s", host), inst.InstanceID)
			}
		}
	}

	// Verify DCs are found (DCs are required, servers are optional)
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
// This bounds concurrent SSM calls to avoid throttling.
const maxConcurrentChecks = 5

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
		os.Stdout.Write(<-ch)
	}
}

// RunQuickChecks runs a subset of critical checks.
func (v *Validator) RunQuickChecks(ctx context.Context) {
	v.runChecks(ctx, []checkFunc{
		v.checkCredentialDiscovery,
		v.checkNetworkMisconfigs,
		v.checkMSSQL,
		v.checkADCS,
		v.checkDomainTrusts,
		v.checkServices,
		v.checkScheduledTasks,
	})
}

// RunAllChecks executes all vulnerability validation checks.
func (v *Validator) RunAllChecks(ctx context.Context) {
	v.runChecks(ctx, []checkFunc{
		v.checkCredentialDiscovery,
		v.checkKerberosAttacks,
		v.checkNetworkMisconfigs,
		v.checkAnonymousSMB,
		v.checkDelegation,
		v.checkMachineAccountQuota,
		v.checkMSSQL,
		v.checkADCS,
		v.checkACLPermissions,
		v.checkDomainTrusts,
		v.checkSIDFiltering,
		v.checkServices,
		v.checkScheduledTasks,
		v.checkLLMNR,
		v.checkGPOAbuse,
		v.checkGMSA,
		v.checkLAPS,
		v.checkSMBShares,
		v.checkFirewallDisabled,
		v.checkPasswordPolicy,
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

func (v *Validator) runPS(ctx context.Context, host, command string) string {
	instanceID, ok := v.hosts[host]
	if !ok {
		v.log.Warn("host not found", "host", host)
		return ""
	}
	if v.verbose {
		v.log.Debug("running PS command", "host", host, "command", command)
	}
	result, err := v.client.RunPowerShellCommand(ctx, instanceID, command, 60*time.Second)
	if err != nil {
		v.log.Warn("PS command failed", "host", host, "error", err)
		return ""
	}
	return result.Stdout
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
		fmt.Fprint(w, color.GreenString("  ✓ %s\n", name))
	case "FAIL":
		fmt.Fprint(w, color.RedString("  ✗ %s\n", name))
	case "WARN":
		fmt.Fprint(w, color.YellowString("  ⚠ %s\n", name))
	case "SKIP":
		fmt.Fprint(w, color.CyanString("  ⊘ %s\n", name))
	case "INFO":
		fmt.Fprintf(w, "  ℹ %s\n", name)
	}
}

func (v *Validator) hasHost(host string) bool {
	_, ok := v.hosts[host]
	return ok
}
