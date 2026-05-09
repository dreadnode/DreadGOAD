package aws

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

func init() {
	provider.Register(provider.NameAWS, func(ctx context.Context, opts provider.ConstructorOpts) (provider.Provider, error) {
		if opts.Region == "" {
			return nil, fmt.Errorf("AWS region is required")
		}
		client, err := NewClient(ctx, opts.Region)
		if err != nil {
			return nil, err
		}
		return &AWSProvider{client: client}, nil
	})
}

// AWSProvider adapts the existing AWS Client to the Provider interface.
type AWSProvider struct {
	client *Client
}

// Client returns the underlying AWS client for SSM-specific operations
// that are not part of the generic Provider interface.
func (p *AWSProvider) Client() *Client {
	return p.client
}

func (p *AWSProvider) Name() string { return provider.NameAWS }

func (p *AWSProvider) VerifyCredentials(ctx context.Context) (string, error) {
	identity, err := p.client.VerifyCredentials(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("AWS account %s", identity.Account), nil
}

func (p *AWSProvider) DiscoverInstances(ctx context.Context, env string) ([]provider.Instance, error) {
	instances, err := p.client.DiscoverInstances(ctx, env)
	if err != nil {
		return nil, err
	}
	return toProviderInstances(instances), nil
}

func (p *AWSProvider) DiscoverAllInstances(ctx context.Context, env string) ([]provider.Instance, error) {
	instances, err := p.client.DiscoverAllInstances(ctx, env)
	if err != nil {
		return nil, err
	}
	return toProviderInstances(instances), nil
}

func (p *AWSProvider) FindInstanceByHostname(ctx context.Context, env, hostname string) (*provider.Instance, error) {
	inst, err := p.client.FindInstanceByHostnameAll(ctx, env, hostname)
	if err != nil {
		return nil, err
	}
	pi := toProviderInstance(*inst)
	return &pi, nil
}

func (p *AWSProvider) StartInstances(ctx context.Context, ids []string) error {
	return p.client.StartInstances(ctx, ids)
}

func (p *AWSProvider) StopInstances(ctx context.Context, ids []string) error {
	return p.client.StopInstances(ctx, ids)
}

func (p *AWSProvider) WaitForInstanceStopped(ctx context.Context, id string) error {
	return p.client.WaitForInstanceStopped(ctx, id)
}

func (p *AWSProvider) DestroyInstances(ctx context.Context, ids []string) error {
	return p.client.TerminateInstances(ctx, ids)
}

func (p *AWSProvider) RunCommand(ctx context.Context, instanceID, command string, timeout time.Duration) (*provider.CommandResult, error) {
	result, err := p.client.RunPowerShellCommand(ctx, instanceID, command, timeout)
	if err != nil {
		return nil, err
	}
	return &provider.CommandResult{
		Status: result.Status,
		Stdout: result.Stdout,
		Stderr: result.Stderr,
	}, nil
}

func (p *AWSProvider) RunCommandOnMultiple(ctx context.Context, instanceIDs []string, command string, timeout time.Duration) (map[string]*provider.CommandResult, error) {
	results, err := p.client.RunPowerShellOnMultiple(ctx, instanceIDs, command, timeout)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*provider.CommandResult, len(results))
	for id, r := range results {
		out[id] = &provider.CommandResult{
			Status: r.Status,
			Stdout: r.Stdout,
			Stderr: r.Stderr,
		}
	}
	return out, nil
}

// SessionManager implementation (SSM sessions).

func (p *AWSProvider) CleanupStaleSessions(ctx context.Context, instanceIDs []string, maxAge time.Duration, dryRun bool) (int, error) {
	return p.client.CleanupStaleSessions(ctx, instanceIDs, maxAge, dryRun, slog.Default())
}

func (p *AWSProvider) DescribeActiveSessions(ctx context.Context, instanceID string) ([]provider.Session, error) {
	sessions, err := p.client.DescribeActiveSessions(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	var out []provider.Session
	for _, s := range sessions {
		out = append(out, provider.Session{
			SessionID:  s.SessionID,
			InstanceID: s.InstanceID,
			StartDate:  s.StartDate,
			Status:     s.Status,
		})
	}
	return out, nil
}

func (p *AWSProvider) StartInteractiveShell(ctx context.Context, instanceID, region string) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	defer signal.Stop(sigCh)

	ssmCmd := exec.Command("aws", "ssm", "start-session",
		"--target", instanceID,
		"--region", region)
	ssmCmd.Stdin = os.Stdin
	ssmCmd.Stdout = os.Stdout
	ssmCmd.Stderr = os.Stderr
	return ssmCmd.Run()
}

// SSMRecovery implementation.

func (p *AWSProvider) EnableSSMUserLocal(ctx context.Context, instanceID string) error {
	return p.client.EnableSSMUserLocal(ctx, instanceID)
}

func (p *AWSProvider) FixSSMUserViaDomainAccount(ctx context.Context, instanceID string) error {
	return p.client.FixSSMUserViaDomainAccount(ctx, instanceID)
}

func (p *AWSProvider) RestartSSMAgent(ctx context.Context, instanceID string) error {
	return p.client.RestartSSMAgent(ctx, instanceID)
}

func (p *AWSProvider) RemoteRestartSSMAgent(ctx context.Context, helperInstanceID, targetFQDN, domain, password string) error {
	return p.client.RemoteRestartSSMAgent(ctx, helperInstanceID, targetFQDN, domain, password)
}

func (p *AWSProvider) CheckSSMStatus(ctx context.Context, instanceIDs []string) ([]provider.SSMStatus, error) {
	statuses, err := p.client.CheckSSMStatus(ctx, instanceIDs)
	if err != nil {
		return nil, err
	}
	var out []provider.SSMStatus
	for _, s := range statuses {
		out = append(out, provider.SSMStatus{
			InstanceID: s.InstanceID,
			PingStatus: s.PingStatus,
		})
	}
	return out, nil
}

// Compile-time interface checks.
var (
	_ provider.Provider         = (*AWSProvider)(nil)
	_ provider.SessionManager   = (*AWSProvider)(nil)
	_ provider.InteractiveShell = (*AWSProvider)(nil)
	_ provider.SSMRecovery      = (*AWSProvider)(nil)
)

func toProviderInstance(i Instance) provider.Instance {
	return provider.Instance{
		ID:        i.InstanceID,
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
