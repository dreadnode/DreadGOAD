package azure

import (
	"context"
	"fmt"
	"time"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

func init() {
	provider.Register(provider.NameAzure, func(ctx context.Context, opts provider.ConstructorOpts) (provider.Provider, error) {
		if opts.Region == "" {
			return nil, fmt.Errorf("azure region is required")
		}
		client := NewClient(opts.Region)
		return &AzureProvider{client: client}, nil
	})
}

// AzureProvider adapts the az-CLI–backed Client to the provider.Provider
// interface. It does not implement the optional SSM-specific recovery
// interfaces, since Azure Run Command operates over the control plane and
// does not depend on an in-VM agent that needs babysitting.
type AzureProvider struct {
	client *Client
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
	return fmt.Sprintf("Azure subscription %s (%s)", acct.Name, acct.ID), nil
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

func (p *AzureProvider) RunCommand(ctx context.Context, instanceID, command string, timeout time.Duration) (*provider.CommandResult, error) {
	res, err := p.client.RunPowerShellCommand(ctx, instanceID, command, timeout)
	if err != nil {
		return nil, err
	}
	return &provider.CommandResult{Status: res.Status, Stdout: res.Stdout, Stderr: res.Stderr}, nil
}

func (p *AzureProvider) RunCommandOnMultiple(ctx context.Context, instanceIDs []string, command string, timeout time.Duration) (map[string]*provider.CommandResult, error) {
	res, err := p.client.RunPowerShellOnMultiple(ctx, instanceIDs, command, timeout)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*provider.CommandResult, len(res))
	for id, r := range res {
		out[id] = &provider.CommandResult{Status: r.Status, Stdout: r.Stdout, Stderr: r.Stderr}
	}
	return out, nil
}

var _ provider.Provider = (*AzureProvider)(nil)

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
