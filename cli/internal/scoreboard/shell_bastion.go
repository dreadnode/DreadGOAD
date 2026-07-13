package scoreboard

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// BastionShellRunner executes shell commands on an Azure Linux VM via
// `az network bastion ssh`. The command is run non-interactively by
// appending `-- <command>` to the ssh invocation.
type BastionShellRunner struct {
	BastionName     string // Azure Bastion resource name
	ResourceGroup   string // Bastion's resource group
	VMResourceID    string // Full Azure resource ID of the Kali VM
	SSHKeyPath      string // Path to the SSH private key
	Username        string // SSH username (default: "kali")
}

// bastionOverhead is extra time budgeted for Bastion API call, tunnel setup,
// and SSH handshake before the remote command starts executing.
const bastionOverhead = 60 * time.Second

// RunShell executes a shell command on the Kali VM via Bastion SSH and
// returns stdout. The command is passed as a single argument to bash -c
// on the remote side via ssh's `-- bash -c '<command>'` mechanism.
func (r *BastionShellRunner) RunShell(ctx context.Context, command string, timeout time.Duration) (string, error) {
	if r.BastionName == "" || r.ResourceGroup == "" {
		return "", fmt.Errorf("bastion name and resource group are required")
	}
	if r.VMResourceID == "" {
		return "", fmt.Errorf("VM resource ID is required")
	}
	if r.SSHKeyPath == "" {
		return "", fmt.Errorf("--ssh-key is required for Azure Bastion SSH (auth-type is ssh-key)")
	}

	username := r.Username
	if username == "" {
		username = "kali"
	}

	ctx, cancel := context.WithTimeout(ctx, timeout+bastionOverhead)
	defer cancel()

	args := []string{
		"network", "bastion", "ssh",
		"--name", r.BastionName,
		"--resource-group", r.ResourceGroup,
		"--target-resource-id", r.VMResourceID,
		"--auth-type", "ssh-key",
		"--username", username,
		"--ssh-key", r.SSHKeyPath,
	}
	// Everything after -- is forwarded to the underlying ssh process.
	// -o IdentitiesOnly=yes prevents ssh-agent from burning through
	// MaxAuthTries with unrelated keys.
	// The command is passed as a single argument — SSH concatenates all
	// args with spaces before sending to the remote shell, so passing
	// "bash", "-c", command as 3 args would break (bash -c only takes the
	// next word as the script). Passing the command directly works because
	// SSH runs it via the remote user's login shell.
	args = append(args, "--", "-o", "IdentitiesOnly=yes", command)

	cmd := exec.CommandContext(ctx, "az", args...)
	cmd.Stdin = nil // prevent hangs if ssh prompts for passphrase
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Like SSM, the command may return non-zero (e.g., nxc auth
		// failure) but still produce useful stdout.
		if stdout.Len() > 0 {
			return stdout.String(), nil
		}
		return "", fmt.Errorf("bastion ssh: %w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}
