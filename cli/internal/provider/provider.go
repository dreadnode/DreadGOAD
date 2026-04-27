package provider

import (
	"context"
	"time"
)

// Instance represents a discovered VM/instance from any provider.
type Instance struct {
	ID        string // provider-specific identifier (EC2 instance ID, Proxmox VMID, etc.)
	Name      string
	PrivateIP string
	State     string // "running", "stopped", etc.
}

// CommandResult holds the output of a remote command execution.
type CommandResult struct {
	Status string
	Stdout string
	Stderr string
}

// Provider defines the interface that all infrastructure providers must implement.
// Commands use this interface instead of directly depending on AWS, Proxmox, etc.
type Provider interface {
	// Name returns the provider identifier (e.g. "aws", "proxmox").
	Name() string

	// VerifyCredentials checks that provider credentials are valid and returns
	// a human-readable identity string (e.g. AWS account ID, Proxmox username).
	VerifyCredentials(ctx context.Context) (string, error)

	// DiscoverInstances finds lab instances for the given environment.
	// Only running instances are returned by default.
	DiscoverInstances(ctx context.Context, env string) ([]Instance, error)

	// DiscoverAllInstances finds lab instances in any state (including stopped).
	DiscoverAllInstances(ctx context.Context, env string) ([]Instance, error)

	// FindInstanceByHostname finds an instance (any state) whose name contains the hostname.
	FindInstanceByHostname(ctx context.Context, env, hostname string) (*Instance, error)

	// StartInstances starts the given instances.
	StartInstances(ctx context.Context, ids []string) error

	// StopInstances stops the given instances.
	StopInstances(ctx context.Context, ids []string) error

	// WaitForInstanceStopped blocks until the instance reaches a stopped state.
	WaitForInstanceStopped(ctx context.Context, id string) error

	// DestroyInstances permanently terminates the given instances.
	DestroyInstances(ctx context.Context, ids []string) error

	// RunCommand executes a command on an instance and returns the result.
	// The command type (PowerShell, shell, etc.) is provider-specific.
	RunCommand(ctx context.Context, instanceID, command string, timeout time.Duration) (*CommandResult, error)

	// RunCommandOnMultiple executes a command on multiple instances.
	RunCommandOnMultiple(ctx context.Context, instanceIDs []string, command string, timeout time.Duration) (map[string]*CommandResult, error)
}

// SessionManager is an optional interface for providers that support
// remote session management (e.g. AWS SSM). Commands check for this
// capability before performing session-related operations.
type SessionManager interface {
	// CleanupStaleSessions terminates sessions older than maxAge.
	CleanupStaleSessions(ctx context.Context, instanceIDs []string, maxAge time.Duration, dryRun bool) (int, error)

	// DescribeActiveSessions returns active sessions for an instance.
	DescribeActiveSessions(ctx context.Context, instanceID string) ([]Session, error)

	// StartInteractiveSession starts an interactive terminal session to an instance.
	StartInteractiveSession(ctx context.Context, instanceID, region string) error
}

// Session represents an active remote session.
type Session struct {
	SessionID  string
	InstanceID string
	StartDate  time.Time
	Status     string
}

// SSMRecovery is an optional interface for providers that support
// SSM-specific recovery operations (agent restart, user account fixes).
type SSMRecovery interface {
	// EnableSSMUserLocal re-enables the local ssm-user account.
	EnableSSMUserLocal(ctx context.Context, instanceID string) error

	// FixSSMUserViaDomainAccount creates ssm-user as a domain account on DCs.
	FixSSMUserViaDomainAccount(ctx context.Context, instanceID string) error

	// RestartSSMAgent restarts the SSM agent on an instance.
	RestartSSMAgent(ctx context.Context, instanceID string) error

	// RemoteRestartSSMAgent restarts the SSM agent on a target via a helper instance.
	RemoteRestartSSMAgent(ctx context.Context, helperInstanceID, targetFQDN, domain, password string) error

	// CheckSSMStatus returns the SSM agent status for each instance.
	CheckSSMStatus(ctx context.Context, instanceIDs []string) ([]SSMStatus, error)
}

// SSMStatus holds the SSM agent ping status for an instance.
type SSMStatus struct {
	InstanceID string
	PingStatus string
}

