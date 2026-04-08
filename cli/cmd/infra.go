package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

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

	// Load lab config first so we know which hosts to look for.
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

	// Derive expected hosts from the lab config instead of hardcoding.
	expectedHosts := lab.HostRoles()
	hostMap, err := discoverHostMap(ctx, client, cfg.Env, expectedHosts)
	if err != nil {
		return nil, err
	}

	if err := checkSSMOnline(ctx, client, hostMap); err != nil {
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

// checkSSMOnline verifies that SSM agents are online for all discovered instances.
func checkSSMOnline(ctx context.Context, client *daws.Client, hostMap map[string]string) error {
	var instanceIDs []string
	for _, id := range hostMap {
		instanceIDs = append(instanceIDs, id)
	}

	statuses, err := client.CheckSSMStatus(ctx, instanceIDs)
	if err != nil {
		return fmt.Errorf("check SSM status: %w", err)
	}

	idToHost := make(map[string]string, len(hostMap))
	for h, id := range hostMap {
		idToHost[id] = h
	}

	var offline []string
	for _, s := range statuses {
		if s.PingStatus != "Online" {
			offline = append(offline, fmt.Sprintf("%s (%s)", idToHost[s.InstanceID], s.PingStatus))
		}
	}

	if len(offline) > 0 {
		return fmt.Errorf("SSM agent not online: %s", strings.Join(offline, ", "))
	}
	color.Green("  SSM agents online: %d/%d instances", len(statuses), len(statuses))
	return nil
}
