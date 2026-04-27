package ludus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// SSH connection reuse: when SSH is configured, the Client lazily opens one
// long-lived *ssh.Client (via nativeClient) and reuses it for every remote
// call. Replaces the OpenSSH ControlMaster dance the previous implementation
// needed to keep validate's command fan-out fast.

// VM represents a Ludus VM from range status output.
type VM struct {
	Name      string `json:"name"`
	ProxmoxID int    `json:"proxmoxID"`
	PoweredOn bool   `json:"poweredOn"`
	IP        string `json:"ip"`
}

// RangeStatus represents the JSON output of `ludus range status --json`.
type RangeStatus struct {
	RangeState  string `json:"rangeState"`
	RangeNumber int    `json:"rangeNumber"`
	RangeID     string `json:"rangeID"`
	VMs         []VM   `json:"VMs"`
}

// User represents a Ludus user from `ludus user list --json`.
type User struct {
	Name    string `json:"name"`
	UserID  string `json:"userID"`
	IsAdmin bool   `json:"isAdmin"`
}

// VersionInfo represents the JSON output of `ludus version --json`.
type VersionInfo struct {
	Version string `json:"version"`
}

// SSHConfig holds SSH connection parameters for remote Ludus execution.
type SSHConfig struct {
	Host     string // Remote Ludus host
	User     string // SSH user (default: root)
	KeyPath  string // Path to SSH private key
	Password string // SSH password (used by native SSH auth when set)
	Port     int    // SSH port (default: 22)
}

// IsConfigured returns true if SSH remote execution is enabled.
func (s SSHConfig) IsConfigured() bool { return s.Host != "" }

// maxConcurrentSSHSessions caps how many SSH sessions can be in flight
// against the multiplexed master at once. OpenSSH's default sshd MaxSessions
// is 10; exceeding it surfaces as "Session open refused by peer" and forces
// ssh to fall back to a fresh TCP handshake (with a noisy warning about the
// existing ControlSocket). Staying a few below the limit leaves headroom for
// sessions in teardown.
const maxConcurrentSSHSessions = 8

// Client wraps the Ludus CLI binary for API interaction.
type Client struct {
	apiKey         string
	labUser        string
	useImpersonate bool
	majorVersion   int
	ssh            SSHConfig
	sshSem         chan struct{}

	nativeOnce sync.Once
	native     *nativeClient
	nativeErr  error
}

// NewClient creates a Ludus client with the given API key.
func NewClient(ctx context.Context, apiKey string, useImpersonate bool, ssh SSHConfig) (*Client, error) {
	c := &Client{
		apiKey:         apiKey,
		useImpersonate: useImpersonate,
		majorVersion:   2,
		ssh:            ssh,
		sshSem:         make(chan struct{}, maxConcurrentSSHSessions),
	}

	// Only check for local ludus binary when not using SSH.
	// If missing, attempt to download and install it transparently.
	if !ssh.IsConfigured() {
		if _, err := EnsureCLI(ctx); err != nil {
			return nil, fmt.Errorf("ludus CLI setup failed: %w", err)
		}
	}

	// Detect Ludus version. If this fails, warn but default to v2 (current
	// release) rather than blocking entirely (the server may be temporarily
	// unreachable even though the CLI binary is fine).
	out, err := c.run(ctx, "version", "--json")
	if err != nil {
		fmt.Printf("warning: could not detect ludus version, defaulting to v2: %v\n", err)
	} else {
		var v VersionInfo
		if json.Unmarshal([]byte(out), &v) == nil && v.Version != "" {
			parts := strings.SplitN(v.Version, ".", 2)
			if len(parts) > 0 && parts[0] == "1" {
				c.majorVersion = 1
			}
		}
	}

	return c, nil
}

// MajorVersion returns the detected Ludus major version.
func (c *Client) MajorVersion() int { return c.majorVersion }

// SetLabUser sets the lab user for impersonation.
func (c *Client) SetLabUser(user string) { c.labUser = user }

// SSH returns the SSH configuration.
func (c *Client) SSH() SSHConfig { return c.ssh }

// run executes a ludus CLI command and returns stdout.
// When SSH is configured, the command is executed on the remote host.
// Stdout and stderr are captured separately so that Ludus v2 informational
// log lines (e.g. "[INFO]  Ludus client ...") written to stderr do not
// pollute JSON output parsed by callers such as RangeStatusJSON.
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	cmdArgs := c.buildArgs(args)

	if c.ssh.IsConfigured() {
		return c.runSSH(ctx, "ludus", cmdArgs...)
	}

	return c.runLocal(ctx, cmdArgs...)
}

// runLocal executes a ludus command on the local machine.
func (c *Client) runLocal(ctx context.Context, cmdArgs ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "ludus", cmdArgs...)
	cmd.Env = append(cmd.Environ(), fmt.Sprintf("LUDUS_API_KEY=%s", c.apiKey))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ludus %s: %w\nstdout: %s\nstderr: %s",
			strings.Join(cmdArgs, " "), err, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// acquireSSHSlot blocks until a session slot is available or ctx is canceled.
// The matching release runs from a deferred call in the caller.
func (c *Client) acquireSSHSlot(ctx context.Context) error {
	select {
	case c.sshSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) releaseSSHSlot() { <-c.sshSem }

// ensureNative lazy-opens the long-lived SSH connection to the Ludus host.
// All callers go through this; the first one pays the handshake (and any
// 1Password biometric prompt), every subsequent call reuses the connection.
func (c *Client) ensureNative(ctx context.Context) (*nativeClient, error) {
	c.nativeOnce.Do(func() {
		c.native, c.nativeErr = dialNative(ctx, c.ssh)
	})
	return c.native, c.nativeErr
}

// Close releases the underlying SSH connection. Safe to call multiple times.
func (c *Client) Close() error {
	if c.native != nil {
		return c.native.Close()
	}
	return nil
}

// runSSH executes a command on the remote Ludus host via SSH and returns
// trimmed stdout. Error messages include stdout/stderr for diagnostics.
func (c *Client) runSSH(ctx context.Context, binary string, args ...string) (string, error) {
	if err := c.acquireSSHSlot(ctx); err != nil {
		return "", err
	}
	defer c.releaseSSHSlot()

	cli, err := c.ensureNative(ctx)
	if err != nil {
		return "", fmt.Errorf("ssh dial %s: %w", c.ssh.Host, err)
	}

	quotedArgs := make([]string, len(args))
	for i, a := range args {
		quotedArgs[i] = shellQuote(a)
	}
	remoteCmd := fmt.Sprintf("LUDUS_API_KEY=%s %s %s",
		shellQuote(c.apiKey), binary, strings.Join(quotedArgs, " "))

	stdout, stderr, runErr := cli.Run(ctx, remoteCmd)
	if runErr != nil {
		return "", fmt.Errorf("ssh %s %s: %w\nstdout: %s\nstderr: %s",
			c.ssh.Host, binary, runErr, stdout, stderr)
	}
	return strings.TrimSpace(stdout), nil
}

// RangeEtcHosts reads /opt/ludus/ranges/<rangeID>/etc-hosts on the Proxmox
// host and returns a name -> IP map. Ludus generates this file at deployment
// time, so it is the most reliable IP source: independent of the qemu guest
// agent, and resolved with a single round-trip rather than one per VM.
func (c *Client) RangeEtcHosts(ctx context.Context, rangeID string) (map[string]string, error) {
	if !c.ssh.IsConfigured() {
		return nil, fmt.Errorf("ssh not configured; cannot read range etc-hosts")
	}
	if rangeID == "" {
		return nil, fmt.Errorf("rangeID is empty")
	}
	path := "/opt/ludus/ranges/" + rangeID + "/etc-hosts"
	stdout, stderr, err := c.RunSSHCommand(ctx, "cat", path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w (stderr: %s)", path, err, strings.TrimSpace(stderr))
	}
	hosts := make(map[string]string)
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ip := fields[0]
		for _, name := range fields[1:] {
			hosts[name] = ip
		}
	}
	return hosts, nil
}

// RunSSHCommand executes an arbitrary command on the remote Ludus host via
// SSH and returns (stdout, stderr, error). Used by RangeEtcHosts and by the
// provider for running ansible commands remotely.
func (c *Client) RunSSHCommand(ctx context.Context, binary string, args ...string) (string, string, error) {
	if err := c.acquireSSHSlot(ctx); err != nil {
		return "", "", err
	}
	defer c.releaseSSHSlot()

	cli, err := c.ensureNative(ctx)
	if err != nil {
		return "", "", fmt.Errorf("ssh dial %s: %w", c.ssh.Host, err)
	}

	quotedArgs := make([]string, len(args))
	for i, a := range args {
		quotedArgs[i] = shellQuote(a)
	}
	remoteCmd := fmt.Sprintf("%s %s", binary, strings.Join(quotedArgs, " "))

	return cli.Run(ctx, remoteCmd)
}

// shellQuote wraps a string in single quotes, escaping any embedded single
// quotes. Used to assemble shell command strings sent over SSH; the remote
// side sees `sh -c <cmd>` semantics so each arg has to round-trip safely.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// buildArgs prepends version-specific flags and impersonation.
func (c *Client) buildArgs(args []string) []string {
	var prefix []string
	if c.majorVersion < 2 && len(args) > 0 && (args[0] == "user") {
		prefix = append(prefix, "--url", "https://127.0.0.1:8081")
	}
	if c.useImpersonate && c.labUser != "" {
		prefix = append(prefix, "--user", c.labUser)
	}
	return append(prefix, args...)
}

// VerifyConnection checks that the Ludus API is reachable and the key is valid.
func (c *Client) VerifyConnection(ctx context.Context) (string, error) {
	out, err := c.run(ctx, "version", "--json")
	if err != nil {
		return "", fmt.Errorf("cannot reach Ludus API: %w", err)
	}
	if strings.Contains(out, "No API key loaded") {
		return "", fmt.Errorf("invalid or missing Ludus API key")
	}
	var info struct {
		Version string `json:"version"`
		Result  string `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &info); err == nil && info.Result != "" {
		return info.Result, nil
	}
	return out, nil
}

// ListUsers returns all Ludus users.
func (c *Client) ListUsers(ctx context.Context) ([]User, error) {
	args := []string{"user", "list"}
	if c.majorVersion >= 2 {
		args = append(args, "--json")
	} else {
		args = append(args, "all", "--json")
	}

	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}

	var users []User
	if err := json.Unmarshal([]byte(out), &users); err != nil {
		return nil, fmt.Errorf("parse user list: %w", err)
	}
	return users, nil
}

// RangeStatusJSON returns the parsed range status.
func (c *Client) RangeStatusJSON(ctx context.Context) (*RangeStatus, error) {
	out, err := c.run(ctx, "range", "status", "--json")
	if err != nil {
		return nil, err
	}

	var rs RangeStatus
	if err := json.Unmarshal([]byte(out), &rs); err != nil {
		return nil, fmt.Errorf("parse range status: %w", err)
	}
	return &rs, nil
}

// RangeStatus prints human-readable range status.
func (c *Client) RangeStatusText(ctx context.Context) (string, error) {
	return c.run(ctx, "range", "status")
}

// RangeDeploy deploys the configured range.
func (c *Client) RangeDeploy(ctx context.Context) error {
	_, err := c.run(ctx, "range", "deploy")
	return err
}

// RangeSetConfig sets the range configuration from a YAML file.
func (c *Client) RangeSetConfig(ctx context.Context, configPath string) error {
	_, err := c.run(ctx, "range", "config", "set", "-f", configPath)
	return err
}

// RangeDestroy removes the range.
func (c *Client) RangeDestroy(ctx context.Context) error {
	_, err := c.run(ctx, "range", "rm")
	return err
}

// PowerOn starts VMs by name (or "all").
func (c *Client) PowerOn(ctx context.Context, name string) error {
	_, err := c.run(ctx, "power", "on", "-n", name)
	return err
}

// PowerOff stops VMs by name (or "all").
func (c *Client) PowerOff(ctx context.Context, name string) error {
	_, err := c.run(ctx, "power", "off", "-n", name)
	return err
}

// VMDestroy destroys a VM by Proxmox ID (Ludus v2+ only).
func (c *Client) VMDestroy(ctx context.Context, proxmoxID int) error {
	if c.majorVersion < 2 {
		return fmt.Errorf("VM destroy requires Ludus v2+")
	}
	_, err := c.run(ctx, "vm", "destroy", fmt.Sprintf("%d", proxmoxID), "--no-prompt")
	return err
}

// WaitForDeployment polls range status until SUCCESS, ERROR, or timeout.
func (c *Client) WaitForDeployment(ctx context.Context, pollInterval, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	start := time.Now()
	polls := 0
	for time.Now().Before(deadline) {
		status, err := c.RangeStatusJSON(ctx)
		if err != nil {
			return err
		}

		polls++
		elapsed := time.Since(start).Truncate(time.Second)
		vmSummary := formatVMProgress(status.VMs)

		switch status.RangeState {
		case "SUCCESS":
			fmt.Printf("\r  [%s] deployment complete (%d VMs)          \n", elapsed, len(status.VMs))
			return nil
		case "ERROR":
			errOut, _ := c.run(ctx, "range", "errors")
			return fmt.Errorf("deployment failed after %s: %s", elapsed, errOut)
		case "DEPLOYING":
			fmt.Printf("\r  [%s] deploying... %s", elapsed, vmSummary)
		default:
			return fmt.Errorf("unknown range state: %s", status.RangeState)
		}

		select {
		case <-ctx.Done():
			fmt.Println() // newline after progress
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return fmt.Errorf("deployment timed out after %s", timeout)
}

// formatVMProgress summarizes VM power states for progress display.
func formatVMProgress(vms []VM) string {
	if len(vms) == 0 {
		return "waiting for VMs..."
	}
	on := 0
	for _, vm := range vms {
		if vm.PoweredOn {
			on++
		}
	}
	return fmt.Sprintf("%d/%d VMs powered on", on, len(vms))
}
