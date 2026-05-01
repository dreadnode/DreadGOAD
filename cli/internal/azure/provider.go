package azure

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

func init() {
	provider.Register(provider.NameAzure, func(ctx context.Context, opts provider.ConstructorOpts) (provider.Provider, error) {
		if opts.Region == "" {
			return nil, fmt.Errorf("azure region is required")
		}
		client, err := NewClient(opts.Region)
		if err != nil {
			return nil, err
		}
		return &AzureProvider{
			client:        client,
			env:           opts.Env,
			inventoryPath: opts.InventoryPath,
		}, nil
	})
}

// AzureProvider adapts the az-CLI–backed Client to the provider.Provider
// interface. RunCommand is served by an internal WinRM runner (see winrm.go)
// that tunnels through Bastion → controller → SOCKS5 — the AWS-shaped
// `provider.RunCommand(ctx, instanceID, ...)` seam is preserved, so the
// validator stays unaware of any of this.
//
// The legacy managed Run Command code in runcommand.go remains available
// for ad-hoc use (e.g. interactive shell) but is no longer the validate
// hot path; managed Run Commands took ~15–30s per call versus sub-second
// for WinRM through the existing tunnel.
type AzureProvider struct {
	client        *Client
	env           string
	inventoryPath string

	winrmOnce sync.Once
	winrm     *winrmRunner
}

// Client returns the underlying az-CLI client for Azure-specific operations
// (used by the provision command for Run Command access).
func (p *AzureProvider) Client() *Client { return p.client }

func (p *AzureProvider) Name() string { return provider.NameAzure }

func (p *AzureProvider) VerifyCredentials(ctx context.Context) (string, error) {
	acct, err := p.client.VerifyCredentials(ctx)
	if err != nil {
		return "", err
	}
	if acct.Name != "" {
		return fmt.Sprintf("Azure subscription %s (%s)", acct.Name, acct.ID), nil
	}
	return fmt.Sprintf("Azure subscription %s", acct.ID), nil
}

func (p *AzureProvider) DiscoverInstances(ctx context.Context, env string) ([]provider.Instance, error) {
	instances, err := p.client.DiscoverInstances(ctx, env, false)
	if err != nil {
		return nil, err
	}
	return toProviderInstances(instances), nil
}

func (p *AzureProvider) DiscoverAllInstances(ctx context.Context, env string) ([]provider.Instance, error) {
	instances, err := p.client.DiscoverInstances(ctx, env, true)
	if err != nil {
		return nil, err
	}
	return toProviderInstances(instances), nil
}

func (p *AzureProvider) FindInstanceByHostname(ctx context.Context, env, hostname string) (*provider.Instance, error) {
	inst, err := p.client.FindInstanceByHostname(ctx, env, hostname)
	if err != nil {
		return nil, err
	}
	pi := toProviderInstance(*inst)
	return &pi, nil
}

func (p *AzureProvider) StartInstances(ctx context.Context, ids []string) error {
	return p.client.StartInstances(ctx, ids)
}

func (p *AzureProvider) StopInstances(ctx context.Context, ids []string) error {
	return p.client.StopInstances(ctx, ids)
}

func (p *AzureProvider) WaitForInstanceStopped(ctx context.Context, id string) error {
	return p.client.WaitForInstanceStopped(ctx, id)
}

func (p *AzureProvider) DestroyInstances(ctx context.Context, ids []string) error {
	return p.client.DestroyInstances(ctx, ids)
}

// runner returns the lazily-initialized winrmRunner for this provider.
// Constructed once on first RunCommand; tunnel + maps come up during the
// runner's own lazy init on first runPS call.
func (p *AzureProvider) runner() *winrmRunner {
	p.winrmOnce.Do(func() {
		p.winrm = newWinRMRunner(p.client, p.env, p.inventoryPath)
	})
	return p.winrm
}

func (p *AzureProvider) RunCommand(ctx context.Context, instanceID, command string, timeout time.Duration) (*provider.CommandResult, error) {
	res, err := p.runner().runPS(ctx, instanceID, command, timeout)
	if err != nil {
		return nil, err
	}
	return &provider.CommandResult{Status: res.Status, Stdout: res.Stdout, Stderr: res.Stderr}, nil
}

func (p *AzureProvider) RunCommandOnMultiple(ctx context.Context, instanceIDs []string, command string, timeout time.Duration) (map[string]*provider.CommandResult, error) {
	type result struct {
		id  string
		res *provider.CommandResult
	}
	results := make(chan result, len(instanceIDs))
	var wg sync.WaitGroup
	for _, id := range instanceIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			res, err := p.RunCommand(ctx, id, command, timeout)
			if err != nil {
				results <- result{id: id, res: &provider.CommandResult{Status: "Error", Stderr: err.Error()}}
				return
			}
			results <- result{id: id, res: res}
		}(id)
	}
	wg.Wait()
	close(results)
	out := make(map[string]*provider.CommandResult, len(instanceIDs))
	for r := range results {
		out[r.id] = r.res
	}
	return out, nil
}

// StartInteractiveShell opens a Run Command-backed REPL on the target VM.
// region is unused (the resource ID already encodes location).
func (p *AzureProvider) StartInteractiveShell(ctx context.Context, instanceID, _ string) error {
	return p.client.StartInteractiveShell(ctx, instanceID)
}

// Drain tears down the WinRM runner's tunnel + cached clients and blocks
// until any leftover managed Run Command DELETE goroutines complete (still
// drained for safety, even though the validate hot path no longer uses
// managed Run Commands).
func (p *AzureProvider) Drain() {
	if p.winrm != nil {
		p.winrm.close()
	}
	p.client.Drain()
}

var (
	_ provider.Provider         = (*AzureProvider)(nil)
	_ provider.InteractiveShell = (*AzureProvider)(nil)
	_ provider.Drainer          = (*AzureProvider)(nil)
)

func toProviderInstance(i Instance) provider.Instance {
	return provider.Instance{
		ID:        i.ID,
		Name:      i.Name,
		PrivateIP: i.PrivateIP,
		State:     i.State,
	}
}

func toProviderInstances(instances []Instance) []provider.Instance {
	out := make([]provider.Instance, len(instances))
	for i, inst := range instances {
		out[i] = toProviderInstance(inst)
	}
	return out
}
