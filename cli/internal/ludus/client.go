package ludus

import (
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

// Client wraps the Ludus CLI binary for API interaction.
type Client struct {
	apiKey         string
	labUser        string
	useImpersonate bool
	majorVersion   int
}

// NewClient creates a Ludus client with the given API key.
func NewClient(ctx context.Context, apiKey string, useImpersonate bool) (*Client, error) {
	if _, err := exec.LookPath("ludus"); err != nil {
		return nil, fmt.Errorf("ludus CLI not found in PATH: %w", err)
	}

	c := &Client{
		apiKey:         apiKey,
		useImpersonate: useImpersonate,
		majorVersion:   1,
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

// run executes a ludus CLI command and returns stdout.
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	cmdArgs := c.buildArgs(args)
	cmd := exec.CommandContext(ctx, "ludus", cmdArgs...)
	cmd.Env = append(cmd.Environ(), fmt.Sprintf("LUDUS_API_KEY=%s", c.apiKey))

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ludus %s: %w\noutput: %s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
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
	out, err := c.run(ctx, "version")
	if err != nil {
		return "", fmt.Errorf("cannot reach Ludus API: %w", err)
	}
	if strings.Contains(out, "No API key loaded") {
		return "", fmt.Errorf("invalid or missing Ludus API key")
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
