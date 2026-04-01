package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	daws "github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/fatih/color"
)

// Result represents a single check result.
type Result struct {
	Status   string `json:"status"` // PASS, FAIL, WARN
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
	client  *daws.Client
	log     *slog.Logger
	env     string
	verbose bool
	report  Report
	hosts   map[string]string // hostname -> instance ID
}

// NewValidator creates a new Validator.
func NewValidator(client *daws.Client, env string, verbose bool, log *slog.Logger) *Validator {
	if log == nil {
		log = slog.Default()
	}
	return &Validator{
		client:  client,
		log:     log,
		env:     env,
		verbose: verbose,
		hosts:   make(map[string]string),
		report: Report{
			Date: time.Now().UTC().Format(time.RFC3339),
			Env:  env,
		},
	}
}

// DiscoverHosts finds GOAD instances and maps hostnames to instance IDs.
func (v *Validator) DiscoverHosts(ctx context.Context) error {
	instances, err := v.client.DiscoverInstances(ctx, v.env)
	if err != nil {
		return fmt.Errorf("discover instances: %w", err)
	}

	for _, inst := range instances {
		name := strings.ToUpper(inst.Name)
		for _, host := range []string{"DC01", "DC02", "DC03", "SRV02", "SRV03"} {
			if strings.Contains(name, host) {
				v.hosts[host] = inst.InstanceID
				v.addResult("PASS", "Discovery", fmt.Sprintf("Found %s", host), inst.InstanceID)
			}
		}
	}

	// Verify required hosts
	for _, required := range []string{"DC01", "DC02", "DC03"} {
		if _, ok := v.hosts[required]; !ok {
			v.addResult("FAIL", "Discovery", fmt.Sprintf("Missing %s", required), "not found")
			return fmt.Errorf("required host %s not found", required)
		}
	}
	return nil
}

// RunQuickChecks runs a subset of critical checks: credentials, services, SMB signing, and trusts.
func (v *Validator) RunQuickChecks(ctx context.Context) {
	v.checkCredentialDiscovery(ctx)
	v.checkNetworkMisconfigs(ctx)
	v.checkMSSQL(ctx)
	v.checkADCS(ctx)
	v.checkDomainTrusts(ctx)
	v.checkServices(ctx)
}

// RunAllChecks executes all vulnerability validation checks.
func (v *Validator) RunAllChecks(ctx context.Context) {
	v.checkCredentialDiscovery(ctx)
	v.checkKerberosAttacks(ctx)
	v.checkNetworkMisconfigs(ctx)
	v.checkAnonymousSMB(ctx)
	v.checkDelegation(ctx)
	v.checkMachineAccountQuota(ctx)
	v.checkMSSQL(ctx)
	v.checkADCS(ctx)
	v.checkACLPermissions(ctx)
	v.checkDomainTrusts(ctx)
	v.checkServices(ctx)
}

// GetReport returns the current report.
func (v *Validator) GetReport() *Report {
	v.report.Total = len(v.report.Results)
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

func (v *Validator) addResult(status, category, name, detail string) {
	r := Result{Status: status, Category: category, Name: name, Detail: detail}
	v.report.Results = append(v.report.Results, r)

	switch status {
	case "PASS":
		v.report.Passed++
		color.Green("  ✓ %s", name)
	case "FAIL":
		v.report.Failed++
		color.Red("  ✗ %s", name)
	case "WARN":
		v.report.Warnings++
		color.Yellow("  ⚠ %s", name)
	}
}

func (v *Validator) hasHost(host string) bool {
	_, ok := v.hosts[host]
	return ok
}
