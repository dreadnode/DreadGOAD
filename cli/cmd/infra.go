package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	daws "github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/labmap"
	"github.com/fatih/color"
)

// infraContext holds the validated infrastructure state needed by commands.
type infraContext struct {
	Client  *daws.Client
	HostMap map[string]string // hostname -> instance ID
	Env     string
	Region  string
	Lab     *labmap.LabMap
}

// requireInfra validates that AWS credentials work, GOAD instances are discoverable,
// and SSM agents are online. Returns the ready-to-use infrastructure context.
func requireInfra(ctx context.Context) (*infraContext, error) {
	cfg, err := config.Get()
	if err != nil {
		return nil, err
	}

	region, err := cfg.ResolveRegion()
	if err != nil {
		return nil, err
	}

	client, err := daws.NewClient(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("create AWS client: %w", err)
	}

	identity, err := client.VerifyCredentials(ctx)
	if err != nil {
		return nil, err
	}
	color.Green("  AWS credentials OK (account %s)", identity.Account)

	var lab *labmap.LabMap
	ec := cfg.ActiveEnvironment()
	if ec.Variant {
		_, target := cfg.ResolvedVariantPaths()
		var loadErr error
		lab, loadErr = labmap.LoadFromVariant(target)
		if loadErr != nil {
			return nil, fmt.Errorf("load variant mapping: %w", loadErr)
		}
	} else {
		src := ec.VariantSource
		if src == "" {
			src = "ad/GOAD"
		}
		if !filepath.IsAbs(src) {
			src = filepath.Join(cfg.ProjectRoot, src)
		}
		var loadErr error
		lab, loadErr = labmap.LoadFromSource(src, cfg.Env)
		if loadErr != nil {
			return nil, fmt.Errorf("load lab config: %w", loadErr)
		}
	}

	expectedHosts := lab.HostRoles()
	hostMap, err := discoverHostMap(ctx, client, cfg.Env, expectedHosts)
	if err != nil {
		return nil, err
	}

	if err := checkSSMOnline(ctx, client, hostMap, lab); err != nil {
		return nil, err
	}
	fmt.Println()

	return &infraContext{
		Client:  client,
		HostMap: hostMap,
		Env:     cfg.Env,
		Region:  region,
		Lab:     lab,
	}, nil
}

// discoverHostMap finds running instances and maps host roles to instance IDs.
func discoverHostMap(ctx context.Context, client *daws.Client, env string, expectedHosts []string) (map[string]string, error) {
	instances, err := client.DiscoverInstances(ctx, env)
	if err != nil {
		return nil, fmt.Errorf("discover instances: %w", err)
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("no running instances found for env=%s", env)
	}

	hostMap := make(map[string]string)
	for _, inst := range instances {
		name := strings.ToUpper(inst.Name)
		for _, h := range expectedHosts {
			upper := strings.ToUpper(h)
			if strings.Contains(name, upper) {
				hostMap[upper] = inst.InstanceID
			}
		}
	}

	var found, missing []string
	for _, h := range expectedHosts {
		upper := strings.ToUpper(h)
		if _, ok := hostMap[upper]; ok {
			found = append(found, upper)
		} else {
			missing = append(missing, upper)
		}
	}
	color.Green("  Instances discovered: %s", strings.Join(found, ", "))
	if len(missing) > 0 {
		color.Yellow("  Instances not found: %s", strings.Join(missing, ", "))
	}

	return hostMap, nil
}

// ssmRetryAttempts is the number of times to re-check offline SSM agents before giving up.
const ssmRetryAttempts = 6

// ssmRetryInterval is the delay between SSM status re-checks.
const ssmRetryInterval = 15 * time.Second

// checkSSMOnline verifies that SSM agents are online for all discovered instances.
// Instances with a transient status (e.g. ConnectionLost) are retried with backoff.
// If retries are exhausted, it attempts to remotely restart the SSM agent on
// offline instances via a working instance before giving up.
func checkSSMOnline(ctx context.Context, client *daws.Client, hostMap map[string]string, lab *labmap.LabMap) error {
	idToHost := make(map[string]string, len(hostMap))
	for h, id := range hostMap {
		idToHost[id] = h
	}

	pending := make([]string, 0, len(hostMap))
	for _, id := range hostMap {
		pending = append(pending, id)
	}

	totalInstances := len(pending)

	for attempt := range ssmRetryAttempts {
		statuses, err := client.CheckSSMStatus(ctx, pending)
		if err != nil {
			return fmt.Errorf("check SSM status: %w", err)
		}

		var offline []string
		var nextPending []string
		for _, s := range statuses {
			if s.PingStatus != "Online" {
				offline = append(offline, fmt.Sprintf("%s (%s)", idToHost[s.InstanceID], s.PingStatus))
				nextPending = append(nextPending, s.InstanceID)
			}
		}

		if len(offline) == 0 {
			color.Green("  SSM agents online: %d/%d instances", totalInstances, totalInstances)
			return nil
		}

		if attempt == ssmRetryAttempts-1 {
			// Last retry exhausted — attempt remote SSM agent restart.
			if recovered := recoverSSMAgents(ctx, client, hostMap, idToHost, nextPending, lab); recovered {
				return nil
			}
			return fmt.Errorf("SSM agent not online: %s", strings.Join(offline, ", "))
		}

		color.Yellow("  SSM agent not ready: %s — retrying in %s (%d/%d)",
			strings.Join(offline, ", "), ssmRetryInterval, attempt+1, ssmRetryAttempts)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(ssmRetryInterval):
		}

		pending = nextPending
	}

	return nil // unreachable
}

// recoverSSMAgents attempts to restart SSM agents on offline instances by
// running Invoke-Command from a working instance. This handles the case where
// the SSM agent has a stale DNS cache (e.g. resolving SSM endpoint to a public
// IP instead of the VPC endpoint after a DC promotion changed DNS settings).
// hostConfigByRole looks up a HostConfig by role name, trying the given case
// first then falling back to a case-insensitive scan.
func hostConfigByRole(lab *labmap.LabMap, role string) (labmap.HostConfig, bool) {
	if hc, ok := lab.HostConfigs[role]; ok {
		return hc, true
	}
	lower := strings.ToLower(role)
	for k, hc := range lab.HostConfigs {
		if strings.ToLower(k) == lower {
			return hc, true
		}
	}
	return labmap.HostConfig{}, false
}

// pickRecoveryHelper selects an online instance to use for remote recovery.
// Prefers DCs (more likely to have WinRM connectivity), then sorts alphabetically
// by role for deterministic selection.
func pickRecoveryHelper(hostMap, idToHost map[string]string, offlineIDs []string, lab *labmap.LabMap) string {
	offlineSet := make(map[string]bool, len(offlineIDs))
	for _, id := range offlineIDs {
		offlineSet[id] = true
	}

	type candidate struct {
		role string
		id   string
		isDC bool
	}
	var candidates []candidate
	for _, id := range hostMap {
		if offlineSet[id] {
			continue
		}
		role := strings.ToLower(idToHost[id])
		hc, ok := hostConfigByRole(lab, role)
		candidates = append(candidates, candidate{role: role, id: id, isDC: ok && hc.Type == "dc"})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].isDC != candidates[j].isDC {
			return candidates[i].isDC // DCs first
		}
		return candidates[i].role < candidates[j].role
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].id
}

// restartOfflineAgents attempts to restart SSM agents on each offline instance
// via PowerShell remoting from the helper. Returns the IDs that were successfully restarted.
func restartOfflineAgents(ctx context.Context, client *daws.Client, helperID string, idToHost map[string]string, offlineIDs []string, lab *labmap.LabMap) []string {
	var restartedIDs []string
	for _, offID := range offlineIDs {
		role := strings.ToLower(idToHost[offID])
		hc, ok := hostConfigByRole(lab, role)
		if !ok {
			continue
		}

		dc, ok := lab.DomainConfigs[hc.Domain]
		if !ok || dc.DomainPassword == "" {
			continue
		}

		fqdn := hc.Hostname + "." + hc.Domain
		color.Yellow("    Restarting SSM agent on %s (%s)...", strings.ToUpper(role), fqdn)

		if err := client.RemoteRestartSSMAgent(ctx, helperID, fqdn, dc.NetBIOSName, dc.DomainPassword); err != nil {
			color.Red("    Failed: %v", err)
			continue
		}
		color.Green("    SSM agent restarted on %s", strings.ToUpper(role))
		restartedIDs = append(restartedIDs, offID)
	}
	return restartedIDs
}

func recoverSSMAgents(ctx context.Context, client *daws.Client, hostMap, idToHost map[string]string, offlineIDs []string, lab *labmap.LabMap) bool {
	if lab == nil {
		return false
	}

	helperID := pickRecoveryHelper(hostMap, idToHost, offlineIDs, lab)
	if helperID == "" {
		return false
	}

	color.Yellow("  Attempting remote SSM agent restart via %s...", idToHost[helperID])

	restartedIDs := restartOfflineAgents(ctx, client, helperID, idToHost, offlineIDs, lab)
	if len(restartedIDs) == 0 {
		return false
	}

	color.Yellow("  Waiting %s for SSM agents to reconnect...", ssmRetryInterval)
	select {
	case <-ctx.Done():
		color.Red("  Context cancelled while waiting for SSM agents")
		return false
	case <-time.After(ssmRetryInterval):
	}

	statuses, err := client.CheckSSMStatus(ctx, restartedIDs)
	if err != nil {
		color.Red("  Failed to check SSM status after restart: %v", err)
		return false
	}
	for _, s := range statuses {
		if s.PingStatus != "Online" {
			return false
		}
	}

	color.Green("  SSM agents online: %d/%d instances (recovered via remote restart)",
		len(hostMap), len(hostMap))
	return true
}
