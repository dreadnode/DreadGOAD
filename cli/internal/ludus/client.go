package ludus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

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
	Password string // SSH password (uses sshpass when set)
	Port     int    // SSH port (default: 22)
}

// IsConfigured returns true if SSH remote execution is enabled.
func (s SSHConfig) IsConfigured() bool { return s.Host != "" }

// Client wraps the Ludus CLI binary for API interaction.
type Client struct {
	apiKey         string
	labUser        string
	useImpersonate bool
	majorVersion   int
	ssh            SSHConfig
}

// NewClient creates a Ludus client with the given API key.
func NewClient(ctx context.Context, apiKey string, useImpersonate bool, ssh SSHConfig) (*Client, error) {
	c := &Client{
		apiKey:         apiKey,
		useImpersonate: useImpersonate,
		majorVersion:   1,
		ssh:            ssh,
	}

	// Only check for local ludus binary when not using SSH.
	if !ssh.IsConfigured() {
		if _, err := exec.LookPath("ludus"); err != nil {
			return nil, fmt.Errorf("ludus CLI not found in PATH: %w", err)
		}
	}

	// Detect Ludus version.
	out, err := c.run(ctx, "version", "--json")
	if err == nil {
		var v VersionInfo
		if json.Unmarshal([]byte(out), &v) == nil && v.Version != "" {
			parts := strings.SplitN(v.Version, ".", 2)
			if len(parts) > 0 {
				if major := parts[0]; major == "2" || strings.HasPrefix(major, "2") {
					c.majorVersion = 2
				}
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

// runSSH executes a command on the remote Ludus host via SSH.
func (c *Client) runSSH(ctx context.Context, binary string, args ...string) (string, error) {
	// Build the remote command with proper quoting.
	// Single-quote each argument to prevent shell interpretation on the remote side.
	quotedArgs := make([]string, len(args))
	for i, a := range args {
		quotedArgs[i] = shellQuote(a)
	}
	remoteCmd := fmt.Sprintf("LUDUS_API_KEY=%s %s %s",
		shellQuote(c.apiKey), binary, strings.Join(quotedArgs, " "))

	bin, cmdArgs := buildSSHCommand(c.ssh, remoteCmd)

	cmd := exec.CommandContext(ctx, bin, cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ssh %s %s: %w\nstdout: %s\nstderr: %s",
			c.ssh.Host, binary, err, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// RunSSHCommand executes an arbitrary command on the remote Ludus host via SSH.
// This is used by the provider for running ansible commands remotely.
func (c *Client) RunSSHCommand(ctx context.Context, binary string, args ...string) (string, string, error) {
	quotedArgs := make([]string, len(args))
	for i, a := range args {
		quotedArgs[i] = shellQuote(a)
	}
	remoteCmd := fmt.Sprintf("%s %s", binary, strings.Join(quotedArgs, " "))

	bin, cmdArgs := buildSSHCommand(c.ssh, remoteCmd)

	cmd := exec.CommandContext(ctx, bin, cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// buildSSHCommand constructs the full command (binary + args) for SSH execution.
// When a password is configured, the command is wrapped with sshpass.
func buildSSHCommand(cfg SSHConfig, remoteCmd string) (string, []string) {
	sshArgs := buildSSHArgs(cfg, remoteCmd)

	if cfg.Password != "" {
		// Wrap with sshpass for password-based auth.
		return "sshpass", append([]string{"-p", cfg.Password, "ssh"}, sshArgs...)
	}
	return "ssh", sshArgs
}

// buildSSHArgs constructs the ssh command arguments from SSHConfig.
//
// When only Host is set (the new ssh_config-alias path), we pass the target
// through verbatim and let the user's ssh_config drive the rest — including
// IdentityAgent (1Password), ProxyJump, custom ports, etc. The override
// fields (User/Port/KeyPath/Password) are only emitted when explicitly set,
// so CI/automation contexts that can't rely on ssh_config still work.
func buildSSHArgs(cfg SSHConfig, remoteCmd string) []string {
	var sshArgs []string

	// Always quiet the noise; this is safe and doesn't override auth.
	sshArgs = append(sshArgs, "-o", "LogLevel=ERROR")

	// The "explicit override" flags below should only kick in when the user
	// has bypassed ssh_config — typically because they're providing a raw
	// hostname plus credentials in dreadgoad.yaml.
	hasOverrides := cfg.User != "" || cfg.Port != 0 || cfg.KeyPath != "" || cfg.Password != ""

	if hasOverrides {
		// Explicit-override path: behave like the legacy ssh_host config —
		// disable host-key checking (Ludus servers are typically ephemeral)
		// and force the supplied credentials.
		sshArgs = append(sshArgs, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null")

		if cfg.Password != "" {
			sshArgs = append(sshArgs, "-o", "IdentitiesOnly=yes")
		}
		if cfg.KeyPath != "" {
			sshArgs = append(sshArgs, "-i", cfg.KeyPath)
		}
		if cfg.Port != 0 && cfg.Port != 22 {
			sshArgs = append(sshArgs, "-p", fmt.Sprintf("%d", cfg.Port))
		}

		user := cfg.User
		if user == "" {
			user = "root"
		}
		sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", user, cfg.Host), remoteCmd)
		return sshArgs
	}

	// ssh_config-alias path: trust the user's ssh setup completely.
	sshArgs = append(sshArgs, cfg.Host, remoteCmd)
	return sshArgs
}

// shellQuote wraps a string in single quotes, escaping any embedded single quotes.
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
	for time.Now().Before(deadline) {
		status, err := c.RangeStatusJSON(ctx)
		if err != nil {
			return err
		}

		switch status.RangeState {
		case "SUCCESS":
			return nil
		case "ERROR":
			errOut, _ := c.run(ctx, "range", "errors")
			return fmt.Errorf("deployment failed: %s", errOut)
		case "DEPLOYING":
			// continue polling
		default:
			return fmt.Errorf("unknown range state: %s", status.RangeState)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return fmt.Errorf("deployment timed out after %s", timeout)
}
