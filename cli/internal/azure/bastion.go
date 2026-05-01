package azure

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// BastionHost describes a deployed Azure Bastion resource.
type BastionHost struct {
	ID               string
	Name             string
	ResourceGroup    string
	Location         string
	SKU              string
	TunnelingEnabled bool
	IPConnectEnabled bool
}

// bastionListItem mirrors the relevant slice of `az network bastion list -o json`.
type bastionListItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ResourceGroup string `json:"resourceGroup"`
	Location      string `json:"location"`
	Sku           struct {
		Name string `json:"name"`
	} `json:"sku"`
	EnableTunneling bool              `json:"enableTunneling"`
	EnableIPConnect bool              `json:"enableIpConnect"`
	Tags            map[string]string `json:"tags"`
}

// DiscoverBastion finds the Bastion host tagged for this DreadGOAD env. Returns
// (nil, nil) when no matching host exists; the caller decides whether that's
// an error (e.g. `bastion ssh` should fail; `bastion status` should report it).
func (c *Client) DiscoverBastion(ctx context.Context, env string) (*BastionHost, error) {
	var raw []bastionListItem
	if err := c.runJSON(ctx, &raw,
		"network", "bastion", "list", "-o", "json",
		"--query", fmt.Sprintf(
			"[?tags.Project=='DreadGOAD' && tags.Environment=='%s']", env)); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > 1 {
		return nil, fmt.Errorf("multiple Bastion hosts tagged for env=%s; expected exactly one", env)
	}
	b := raw[0]
	return &BastionHost{
		ID:               b.ID,
		Name:             b.Name,
		ResourceGroup:    b.ResourceGroup,
		Location:         b.Location,
		SKU:              b.Sku.Name,
		TunnelingEnabled: b.EnableTunneling,
		IPConnectEnabled: b.EnableIPConnect,
	}, nil
}

// OpenBastionSSH spawns `az network bastion ssh` with the parent process's
// stdio so the SSH session is real-time. authType is one of "password",
// "ssh-key", or "AAD"; sshKeyPath is required when authType=="ssh-key".
func (c *Client) OpenBastionSSH(ctx context.Context, b *BastionHost, vmID, username, authType, sshKeyPath string) error {
	args := []string{
		"network", "bastion", "ssh",
		"--name", b.Name,
		"--resource-group", b.ResourceGroup,
		"--target-resource-id", vmID,
		"--auth-type", authType,
	}
	if username != "" {
		args = append(args, "--username", username)
	}
	if authType == "ssh-key" && sshKeyPath != "" {
		args = append(args, "--ssh-key", sshKeyPath)
		// Pin to the specified key so a populated ssh-agent doesn't burn
		// through MaxAuthTries with unrelated keys before --ssh-key is
		// offered. Anything after `--` is forwarded to the underlying ssh.
		args = append(args, "--", "-o", "IdentitiesOnly=yes")
	}
	return runInteractive(ctx, args...)
}

// OpenBastionRDP spawns `az network bastion rdp`. Windows clients only.
func (c *Client) OpenBastionRDP(ctx context.Context, b *BastionHost, vmID string) error {
	args := []string{
		"network", "bastion", "rdp",
		"--name", b.Name,
		"--resource-group", b.ResourceGroup,
		"--target-resource-id", vmID,
	}
	return runInteractive(ctx, args...)
}

// OpenBastionTunnel forwards a remote port to a local port via Bastion. Blocks
// until the user terminates the tunnel (Ctrl+C). Requires tunneling_enabled
// on the Bastion SKU (Standard or Premium with the flag set).
func (c *Client) OpenBastionTunnel(ctx context.Context, b *BastionHost, vmID string, remotePort, localPort int) error {
	args := []string{
		"network", "bastion", "tunnel",
		"--name", b.Name,
		"--resource-group", b.ResourceGroup,
		"--target-resource-id", vmID,
		"--resource-port", strconv.Itoa(remotePort),
		"--port", strconv.Itoa(localPort),
	}
	return runInteractive(ctx, args...)
}

// runInteractive shells out to `az` with the parent stdio attached so that
// interactive prompts (auth, MFA) and streaming output work. exec.CommandContext
// is intentionally not used: ctx-cancellation kills the child mid-session,
// which is exactly what we want when the user Ctrl+C's the wrapper.
func runInteractive(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "az", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
