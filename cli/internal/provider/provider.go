package provider

import (
	"context"
	"strings"
	"time"
)

// Instance represents a discovered VM/instance from any provider.
type Instance struct {
	ID        string // provider-specific identifier (EC2 instance ID, Proxmox VMID, etc.)
	Name      string
	PrivateIP string
	State     string // "running", "stopped", etc.
	// Account is the cloud account the instance belongs to: the AWS account ID
	// or the Azure subscription ID. Both come from data already fetched during
	// discovery, so populating it costs no extra API call. Empty when the
	// provider cannot determine it.
	Account string
	// Group is the provider's resource container: an Azure resource group.
	// Empty on providers that have no such concept — AWS has none, where a
	// range is identified by tag convention rather than by containment.
	Group string
	Tags  map[string]string
}

// FindInstanceByRole returns the first instance whose Role tag matches role.
// Providers that do not expose resource tags simply leave Instance.Tags nil.
func FindInstanceByRole(instances []Instance, role string) *Instance {
	for i := range instances {
		if strings.EqualFold(instances[i].Tags["Role"], role) {
			return &instances[i]
		}
	}
	return nil
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
// persistent remote sessions (e.g. AWS SSM). Commands check for this
// capability before performing session-related operations. Providers that
// model command execution as one-shot control-plane invocations (e.g. Azure
// Run Command) generally do NOT implement this interface, and callers should
// gracefully degrade rather than treating its absence as an error.
type SessionManager interface {
	// CleanupStaleSessions terminates sessions older than maxAge.
	CleanupStaleSessions(ctx context.Context, instanceIDs []string, maxAge time.Duration, dryRun bool) (int, error)

	// DescribeActiveSessions returns active sessions for an instance.
	DescribeActiveSessions(ctx context.Context, instanceID string) ([]Session, error)
}

// Drainer is an optional interface for providers that perform deferred
// cleanup work in detached goroutines (e.g. Azure run-command DELETEs that
// must complete server-side before their per-VM concurrency slot is freed).
// Long-running commands like `validate` should call Drain before returning so
// the OS doesn't kill in-flight cleanup goroutines on process exit, leaving
// orphan subresources that count against per-VM API limits.
type Drainer interface {
	Drain()
}

// InteractiveShell is an optional interface for providers that can open
// an interactive shell to an instance. AWS uses SSM Session Manager;
// Azure simulates one over Run Command. Region may be empty for providers
// that don't need it (e.g. Azure, where the resource ID is region-bound).
type InteractiveShell interface {
	StartInteractiveShell(ctx context.Context, instanceID, region string) error
}

// OutOfBandRunner is an optional interface for providers that can execute a
// script WITHOUT an in-guest network listener — via the cloud control plane
// (Azure Run Command, AWS SSM) rather than WinRM/SSH.
//
// This is deliberately separate from Provider.RunCommand. On Azure the latter
// goes over WinRM through a bastion tunnel: fast and fine for fan-out checks
// like validate and health-check, but useless for the one case that matters
// here — a host that is broken badly enough to stop answering on 5985. The
// control plane reaches the VM through its guest agent, so it still works.
//
// AWS's RunCommand is already SSM-backed and so is already out-of-band; it
// implements this by delegating. Callers that need the guarantee must type-
// assert for this interface rather than assuming RunCommand provides it.
type OutOfBandRunner interface {
	// RunCommandOutOfBand executes a script via the control plane and reports
	// which channel served it, for surfacing to the operator.
	RunCommandOutOfBand(ctx context.Context, instanceID, command string, timeout time.Duration) (*CommandResult, error)

	// OutOfBandChannel names the mechanism (e.g. "Azure Run Command", "AWS SSM").
	OutOfBandChannel() string
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

// SecurityCheckResult is one check's outcome in the security report.
type SecurityCheckResult struct {
	Name     string `json:"name"`
	Resource string `json:"resource"`
	Status   string `json:"status"`   // "OK", "FAIL", "WARN", "SKIP"
	Severity string `json:"severity"` // "critical", "high", "info"
	Detail   string `json:"detail"`
}

// SecurityReport is the --json payload for security-check.
type SecurityReport struct {
	Passed  int                   `json:"passed"`
	Failed  int                   `json:"failed"`
	Warned  int                   `json:"warned"`
	Skipped int                   `json:"skipped"`
	Checks  []SecurityCheckResult `json:"checks"`
}

// SecurityChecker is an optional interface for providers that can audit
// the network security posture of a deployed range. vpcCIDR is the
// expected VNet/VPC address space from the config.
type SecurityChecker interface {
	SecurityCheck(ctx context.Context, env, vpcCIDR string) ([]SecurityCheckResult, error)
}
