//go:build !windows

package azure

import (
	"bufio"
	"os"
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

	// Read the grandchild (sleep) PID the wrapper printed. Read through the
	// newline rather than trusting one Read to return the whole line, so a
	// split write can't turn into a flaky parse.
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read child pid (got %q): %v", line, err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("parse child pid %q: %v", line, err)
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

// pgidGuardEnv re-enters this test binary as a subprocess for the guard check
// below. A regression there SIGKILLs the caller's whole process group, so the
// dangerous half runs isolated in its own group rather than taking down
// `go test` (and the developer's shell) with it.
const pgidGuardEnv = "DREADGOAD_TEST_PGID_GUARD_CHILD"

// pgidGuardOK is the exit code the child reports when it survived the kill.
const pgidGuardOK = 7

// TestKillBastionTunnelSpareOwnProcessGroup pins the `pgid == pid` guard in
// killBastionTunnel. Given a command started WITHOUT SysProcAttr.Setpgid,
// syscall.Getpgid returns the *caller's* group — so an unguarded
// kill(-pgid, SIGKILL) would take down dreadgoad itself. The guard must detect
// that and fall back to killing only the single process.
func TestKillBastionTunnelSpareOwnProcessGroup(t *testing.T) {
	if os.Getenv(pgidGuardEnv) == "1" {
		// Detach into our own process group so a regression's group-kill is
		// contained to this subprocess.
		if err := syscall.Setpgid(0, 0); err != nil {
			os.Exit(3)
		}
		// No Setpgid here: the child inherits OUR pgid, which is exactly the
		// condition the guard exists to catch.
		victim := exec.Command("sleep", "120")
		if err := victim.Start(); err != nil {
			os.Exit(4)
		}
		killBastionTunnel(victim)
		// Still executing => the guard held and we did not signal our own group.
		os.Exit(pgidGuardOK)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=TestKillBastionTunnelSpareOwnProcessGroup")
	cmd.Env = append(os.Environ(), pgidGuardEnv+"=1")

	err = cmd.Run()
	code := cmd.ProcessState.ExitCode()
	if code == pgidGuardOK {
		return // guard held
	}
	if code == -1 {
		t.Fatalf("subprocess was killed by a signal (%v) — killBastionTunnel "+
			"signalled its own process group; the pgid == pid guard is missing", cmd.ProcessState)
	}
	t.Fatalf("subprocess exited %d (err=%v), want %d", code, err, pgidGuardOK)
}
