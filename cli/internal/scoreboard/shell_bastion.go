package scoreboard

import (
	"context"
	"fmt"
	"time"
)

// BastionShellRunner executes shell commands on an Azure Linux VM via Bastion
// SSH tunnel. Not yet implemented — Azure live verification requires the
// Bastion SSH tunnel plumbing to be wired up.
type BastionShellRunner struct {
	VMResource string
}

// RunShell is not yet implemented for Azure.
func (r *BastionShellRunner) RunShell(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "", fmt.Errorf("azure live verification not yet implemented (VM: %s)", r.VMResource)
}
