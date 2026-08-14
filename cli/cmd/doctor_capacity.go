package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/azure"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/doctor"
	"github.com/dreadnode/dreadgoad/internal/terragrunt"
)

// capacityCheckTimeout bounds the two ARM reads. A pre-flight check that hangs
// is worse than one that is skipped: `up` cannot start until it returns.
const capacityCheckTimeout = 30 * time.Second

// azureCapacityChecks reports whether the region can actually supply the VM
// sizes this environment asks for.
//
// This exists because SkuNotAvailable only otherwise surfaces minutes into
// `tofu apply`, after the network and bastion are already built — Azure
// publishes the same restriction on the Resource SKUs API before anything is
// created. Two reads, both free.
//
// Every result is pass or warn, never fail. `doctor.PrintResults` turns a fail
// into an aborted `up`, and neither signal is certain enough for that: capacity
// is real-time, so a restriction can clear between this check and the apply,
// and a quota reading can be shadowed by limits this does not model.
func azureCapacityChecks(cfg *config.Config) []doctor.CheckResult {
	if cfg.ResolvedProvider() != "azure" {
		return nil
	}
	region := cfg.Region
	if region == "" {
		return []doctor.CheckResult{{
			Name:    "Azure capacity",
			Status:  "warn",
			Message: "no region configured, so capacity could not be checked",
		}}
	}

	envDir := filepath.Join(cfg.ProjectRoot, "infra", "azure", cfg.Infra.Deployment, cfg.Env)
	if _, err := os.Stat(envDir); err != nil {
		return []doctor.CheckResult{{
			Name:   "Azure capacity",
			Status: "warn",
			Message: fmt.Sprintf(
				"no scaffolding at %s, so the requested VM sizes are unknown", envDir),
		}}
	}
	req, err := terragrunt.RequestedSizes(envDir)
	if err != nil || len(req.Sizes) == 0 {
		return []doctor.CheckResult{{
			Name:   "Azure capacity",
			Status: "warn",
			Message: fmt.Sprintf(
				"could not read the VM sizes from %s, so capacity was not checked", envDir),
		}}
	}

	ctx, cancel := context.WithTimeout(context.Background(), capacityCheckTimeout)
	defer cancel()

	client, err := azureClientForCapacity(ctx, cfg)
	if err != nil {
		return []doctor.CheckResult{{
			Name:    "Azure capacity",
			Status:  "warn",
			Message: fmt.Sprintf("could not reach Azure to check capacity: %v", err),
		}}
	}

	// One SKU read feeds both checks: the availability verdict and the vCPU
	// count the quota comparison needs.
	statuses, skuErr := client.SKUAvailability(ctx, region, req.Sizes)
	results := []doctor.CheckResult{skuCheck(statuses, skuErr, region, req, cfg.Infra.Deployment, cfg.Env)}
	if q := quotaCheck(ctx, client, region, req, statuses); q != nil {
		results = append(results, *q)
	}
	return results
}

func azureClientForCapacity(ctx context.Context, cfg *config.Config) (*azure.Client, error) {
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return nil, err
	}
	return azureClientFromProvider(prov)
}

func skuCheck(
	statuses []azure.SKUStatus, err error, region string,
	req terragrunt.Requested, deployment, env string,
) doctor.CheckResult {
	if err != nil {
		return doctor.CheckResult{
			Name:    "Azure VM size availability",
			Status:  "warn",
			Message: fmt.Sprintf("could not read SKU availability in %s: %v", region, err),
		}
	}

	var blocked, zonal []string
	for _, s := range statuses {
		switch {
		case !s.Offered:
			blocked = append(blocked, fmt.Sprintf("%s (not offered in %s)", s.Name, region))
		case len(s.Restrictions) > 0:
			blocked = append(blocked, fmt.Sprintf("%s (%s)", s.Name, strings.Join(s.Restrictions, ", ")))
		case len(s.RestrictedZones) > 0:
			zonal = append(zonal, fmt.Sprintf("%s (zones %s)", s.Name, strings.Join(s.RestrictedZones, ",")))
		}
	}

	if len(blocked) > 0 {
		return doctor.CheckResult{
			Name:   "Azure VM size availability",
			Status: "warn",
			Message: fmt.Sprintf(
				"%s unavailable in %s — `up` will likely fail with SkuNotAvailable. "+
					"Change the size in infra/azure/%s/%s/env.hcl and the unit terragrunt.hcl "+
					"files, or deploy to another region.",
				strings.Join(blocked, "; "), region, deployment, env),
		}
	}
	msg := fmt.Sprintf("%s available in %s", strings.Join(req.Sizes, ", "), region)
	if len(zonal) > 0 {
		// Not a blocker: the lab's units do not pin a zone, so Azure places the
		// VM in one that is not restricted.
		msg += fmt.Sprintf(" (zone-restricted: %s)", strings.Join(zonal, "; "))
	}
	return doctor.CheckResult{Name: "Azure VM size availability", Status: "pass", Message: msg}
}

// quotaCheck compares the range's core count against the region's vCPU quota.
// Returns nil when Azure does not report a total-cores counter, rather than
// inventing a pass for something it could not measure.
func quotaCheck(
	ctx context.Context, client *azure.Client, region string,
	req terragrunt.Requested, statuses []azure.SKUStatus,
) *doctor.CheckResult {
	items, err := client.RegionQuota(ctx, region)
	if err != nil {
		return &doctor.CheckResult{
			Name:    "Azure vCPU quota",
			Status:  "warn",
			Message: fmt.Sprintf("could not read quota in %s: %v", region, err),
		}
	}
	cores, ok := azure.FindQuota(items, "cores")
	if !ok {
		return nil
	}

	// Sizes are usually uniform across the lab; when they are not, the largest
	// is used for every VM so the estimate errs toward warning.
	perVM := largestVCPU(statuses)
	if perVM == 0 {
		return nil
	}
	needed := int64(perVM) * int64(req.Units)
	if needed > cores.Headroom() {
		return &doctor.CheckResult{
			Name:   "Azure vCPU quota",
			Status: "warn",
			Message: fmt.Sprintf(
				"range needs ~%d vCPUs (%d VMs x %d) but %s has %d of %d left — request a quota increase or use a smaller size",
				needed, req.Units, perVM, region, cores.Headroom(), cores.Limit),
		}
	}
	return &doctor.CheckResult{
		Name:   "Azure vCPU quota",
		Status: "pass",
		Message: fmt.Sprintf("~%d vCPUs needed, %d of %d free in %s",
			needed, cores.Headroom(), cores.Limit, region),
	}
}

func largestVCPU(statuses []azure.SKUStatus) int32 {
	var max int32
	for _, s := range statuses {
		if s.VCPUs > max {
			max = s.VCPUs
		}
	}
	return max
}
