//go:build !windows

package azure

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// processAlive reports whether pid still exists (signal 0 probes without
// delivering). A reaped process yields ESRCH.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// TestKillBastionTunnelReapsChildTree is the regression guard for the tunnel
// leak: the real `az network bastion tunnel` is a shell wrapper that spawns a
// python child, so killing only the wrapper leaves the child (and its tunnel)
// running. We reproduce that topology with `sh` (wrapper) spawning a
// backgrounded `sleep` (child), then assert killBastionTunnel reaps BOTH by
// signalling the whole process group.
func TestKillBastionTunnelReapsChildTree(t *testing.T) {
	// sh backgrounds a long sleep (the "python child"), prints its PID, then
	// waits — mirroring a wrapper that outlives nothing of its own but holds a
	// child that must die with it.
	cmd := exec.Command("sh", "-c", "sleep 120 & echo $!; wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Read the grandchild (sleep) PID the wrapper printed.
	buf := make([]byte, 64)
	n, err := stdout.Read(buf)
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil {
		t.Fatalf("parse child pid %q: %v", string(buf[:n]), err)
	}

	if !processAlive(childPID) {
		t.Fatalf("precondition failed: child %d not alive after start", childPID)
	}

	killBastionTunnel(cmd)

	// The grandchild reparents to init and is reaped shortly after SIGKILL.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(childPID) {
			return // reaped — the leak is fixed
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("child process %d survived killBastionTunnel — tunnel would leak", childPID)
}

// TestKillBastionTunnelNilSafe guards the early-error paths that may call
// Close() before the command was started.
func TestKillBastionTunnelNilSafe(t *testing.T) {
	killBastionTunnel(nil)
	killBastionTunnel(&exec.Cmd{}) // Process == nil
}
