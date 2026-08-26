//go:build !windows

package azure

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestStartBastionTunnelProcessRequiresCommand(t *testing.T) {
	process, err := startBastionTunnelProcess(context.Background(), nil)
	if err == nil {
		if process != nil {
			killBastionTunnel(process)
		}
		t.Fatal("startBastionTunnelProcess() error = nil, want missing command error")
	}
	if !strings.Contains(err.Error(), "watchdog command is required") {
		t.Fatalf("startBastionTunnelProcess() error = %q, want missing command error", err)
	}
}

// processAlive reports whether pid still exists (signal 0 probes without
// delivering). A reaped process yields ESRCH.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// TestTerminateProcessGroupReapsChildTree is the regression guard for the tunnel
// leak: the real `az network bastion tunnel` is a shell wrapper that spawns a
// Python child, so killing only the wrapper leaves the child (and its tunnel)
// running. We reproduce that topology with `sh` (wrapper) spawning a
// backgrounded `sleep` (child), then assert terminateProcessGroup reaps BOTH.
func TestTerminateProcessGroupReapsChildTree(t *testing.T) {
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

	reaped := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(reaped)
	}()
	terminateProcessGroup(cmd, reaped)

	// The grandchild reparents to init and is reaped shortly after SIGKILL.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(childPID) {
			return // reaped — the leak is fixed
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("child process %d survived terminateProcessGroup — tunnel would leak", childPID)
}

// TestTerminateProcessGroupNilSafe guards early-error paths before start.
func TestTerminateProcessGroupNilSafe(t *testing.T) {
	terminateProcessGroup(nil, nil)
	terminateProcessGroup(&exec.Cmd{}, nil) // Process == nil
}

// TestProvisionTunnelCloseIsRaceFree pins the closeOnce guards. Concurrent
// Close calls must collapse to one pipe close and one wait for watchdog exit.
func TestProvisionTunnelCloseIsRaceFree(t *testing.T) {
	parentRead, parentWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	cmd := exec.Command("sh", "-c", "cat <&3")
	cmd.ExtraFiles = []*os.File{parentRead}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = parentRead.Close()
	// socks stays nil: this exercises the subprocess half, which is where the
	// race lives.
	process := &bastionTunnelProcess{
		cmd:         cmd,
		parentWrite: parentWrite,
		done:        make(chan struct{}),
	}
	go func() {
		process.waitErr = cmd.Wait()
		close(process.done)
	}()
	tunnel := &ProvisionTunnel{bastionProcess: process}

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tunnel.Close()
		}()
	}
	wg.Wait()

	if processAlive(cmd.Process.Pid) {
		t.Fatalf("process %d survived Close", cmd.Process.Pid)
	}
}

const (
	parentDeathRoleEnv     = "DREADGOAD_TEST_PARENT_DEATH_ROLE"
	parentDeathProcessFile = "DREADGOAD_TEST_PARENT_DEATH_PROCESS_FILE"
)

func runParentDeathWatchdogProcess() {
	parentLifetime := os.NewFile(3, "test-parent-lifetime")
	err := RunBastionWatchdog(parentLifetime, []string{
		"sh", "-c",
		`sleep 120 & printf '%s %s\n' "$$" "$!" > "$DREADGOAD_TEST_PARENT_DEATH_PROCESS_FILE"; wait`,
	})
	if err != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func runParentDeathParentProcess() {
	parentRead, parentWrite, err := os.Pipe()
	if err != nil {
		os.Exit(5)
	}
	executable, err := os.Executable()
	if err != nil {
		os.Exit(6)
	}
	watchdog := exec.Command(executable, "-test.run=^TestBastionWatchdogReapsOnParentDeath$")
	watchdog.Env = append(os.Environ(), parentDeathRoleEnv+"=watchdog")
	watchdog.ExtraFiles = []*os.File{parentRead}
	watchdog.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := watchdog.Start(); err != nil {
		os.Exit(7)
	}
	_ = parentRead.Close()
	watchdogFile := os.Getenv(parentDeathProcessFile) + ".watchdog"
	if err := os.WriteFile(watchdogFile, []byte(strconv.Itoa(watchdog.Process.Pid)), 0o600); err != nil {
		os.Exit(8)
	}
	for {
		// Keep the writer live until the outer test kills this process. Its
		// kernel-driven close is the event under test.
		runtime.KeepAlive(parentWrite)
		time.Sleep(time.Second)
	}
}

// TestBastionWatchdogReapsOnParentDeath exercises the failure mode that
// in-process cleanup cannot cover. The outer test starts a simulated dreadgoad
// parent, which starts the watchdog, which starts a shell wrapper and child.
// SIGKILLing the simulated parent closes the liveness pipe; the independently
// running watchdog must then terminate and reap the complete command group.
func TestBastionWatchdogReapsOnParentDeath(t *testing.T) {
	role := os.Getenv(parentDeathRoleEnv)
	if role == "watchdog" {
		runParentDeathWatchdogProcess()
	}

	if role == "parent" {
		runParentDeathParentProcess()
	}

	tempDir := t.TempDir()
	processFile := filepath.Join(tempDir, "processes")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test executable: %v", err)
	}
	parent := exec.Command(executable, "-test.run=^TestBastionWatchdogReapsOnParentDeath$")
	parent.Env = append(os.Environ(),
		parentDeathRoleEnv+"=parent",
		parentDeathProcessFile+"="+processFile,
	)
	if err := parent.Start(); err != nil {
		t.Fatalf("start simulated parent: %v", err)
	}
	defer func() {
		if parent.ProcessState == nil {
			_ = parent.Process.Kill()
			_ = parent.Wait()
		}
	}()

	commandPIDs := waitForPIDFile(t, processFile, 2)
	watchdogPIDs := waitForPIDFile(t, processFile+".watchdog", 1)
	allChildren := make([]int, 0, len(watchdogPIDs)+len(commandPIDs))
	allChildren = append(allChildren, watchdogPIDs...)
	allChildren = append(allChildren, commandPIDs...)
	for _, pid := range allChildren {
		if !processAlive(pid) {
			t.Fatalf("precondition failed: process %d not alive", pid)
		}
	}

	if err := parent.Process.Kill(); err != nil {
		t.Fatalf("SIGKILL simulated parent: %v", err)
	}
	_ = parent.Wait()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		alive := false
		for _, pid := range allChildren {
			alive = alive || processAlive(pid)
		}
		if !alive {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("watchdog or tunnel processes survived parent death: %v", allChildren)
}

// TestBastionWatchdogTracksGroupAfterLeaderExit mirrors the observed orphan
// topology: the process-group leader is gone but a descendant still owns the
// tunnel. The watchdog must continue supervising the group and reap that
// descendant when the parent-lifetime pipe closes.
func TestBastionWatchdogTracksGroupAfterLeaderExit(t *testing.T) {
	processFile := filepath.Join(t.TempDir(), "child-pid")
	t.Setenv(parentDeathProcessFile, processFile)
	parentRead, parentWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() {
		_ = parentWrite.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- RunBastionWatchdog(parentRead, []string{
			"sh", "-c",
			`sleep 120 & printf '%s\n' "$!" > "$DREADGOAD_TEST_PARENT_DEATH_PROCESS_FILE"; exit 0`,
		})
	}()

	childPID := waitForPIDFile(t, processFile, 1)[0]
	childPGID, err := syscall.Getpgid(childPID)
	if err != nil {
		t.Fatalf("get descendant process group: %v", err)
	}
	defer func() {
		_ = syscall.Kill(-childPGID, syscall.SIGKILL)
	}()
	time.Sleep(100 * time.Millisecond) // let the short-lived group leader exit
	select {
	case err := <-done:
		t.Fatalf("watchdog exited with descendant %d still alive: %v", childPID, err)
	default:
	}
	if !processAlive(childPID) {
		t.Fatalf("precondition failed: descendant %d exited early", childPID)
	}

	if err := parentWrite.Close(); err != nil {
		t.Fatalf("close parent lifetime: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watchdog cleanup: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not exit after parent lifetime closed")
	}
	if processAlive(childPID) {
		t.Fatalf("descendant %d survived watchdog cleanup", childPID)
	}
}

func waitForPIDFile(t *testing.T, path string, count int) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			fields := strings.Fields(string(data))
			if len(fields) == count {
				pids := make([]int, 0, count)
				for _, field := range fields {
					pid, err := strconv.Atoi(field)
					if err != nil {
						t.Fatalf("parse pid %q from %s: %v", field, path, err)
					}
					pids = append(pids, pid)
				}
				return pids
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d pids in %s", count, path)
	return nil
}

// pgidGuardEnv re-enters this test binary as a subprocess for the guard check
// below. A regression there SIGKILLs the caller's whole process group, so the
// dangerous half runs isolated in its own group rather than taking down
// `go test` (and the developer's shell) with it.
const pgidGuardEnv = "DREADGOAD_TEST_PGID_GUARD_CHILD"

// pgidGuardOK is the exit code the child reports when it survived the kill.
const pgidGuardOK = 7

// TestTerminateProcessGroupSparesOwnProcessGroup pins the `pgid == pid` guard
// in terminateProcessGroup. Given a command started WITHOUT Setpgid,
// syscall.Getpgid returns the *caller's* group — so an unguarded
// kill(-pgid, SIGKILL) would take down dreadgoad itself. The guard must detect
// that and fall back to killing only the single process.
func TestTerminateProcessGroupSparesOwnProcessGroup(t *testing.T) {
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
		reaped := make(chan struct{})
		go func() {
			_ = victim.Wait()
			close(reaped)
		}()
		terminateProcessGroup(victim, reaped)
		// Still executing => the guard held and we did not signal our own group.
		os.Exit(pgidGuardOK)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=TestTerminateProcessGroupSparesOwnProcessGroup")
	cmd.Env = append(os.Environ(), pgidGuardEnv+"=1")

	err = cmd.Run()
	code := cmd.ProcessState.ExitCode()
	if code == pgidGuardOK {
		return // guard held
	}
	if code == -1 {
		t.Fatalf("subprocess was killed by a signal (%v) — terminateProcessGroup "+
			"signalled its own process group; the pgid == pid guard is missing", cmd.ProcessState)
	}
	t.Fatalf("subprocess exited %d (err=%v), want %d", code, err, pgidGuardOK)
}
