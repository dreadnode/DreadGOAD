package azure

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dreadnode/dreadgoad/internal/ludus"
)

// ProvisionTunnel chains an Azure Bastion port-forward (laptop → controller:22)
// with a SOCKS5 proxy that dials from the controller's network position. The
// controller sits in the same VNet as the GOAD VMs, so SOCKS-routed WinRM
// traffic reaches private 5985 listeners that the laptop can't touch directly.
type ProvisionTunnel struct {
	socks          *ludus.SOCKSTunnel
	bastionProcess *bastionTunnelProcess
	localPort      int
	closeOnce      sync.Once
}

// bastionTunnelProcess is the parent-side handle for the watchdog subprocess.
// The write end is deliberately held only by dreadgoad: normal cleanup closes
// it explicitly, while abrupt process death makes the kernel close it. Either
// event wakes the watchdog and causes it to reap the complete az process group.
type bastionTunnelProcess struct {
	cmd         *exec.Cmd
	parentWrite *os.File
	closeOnce   sync.Once
	done        chan struct{}
	waitErr     error
}

// ProxyURL returns the SOCKS5 proxy URL Ansible's psrp connection plugin
// should use (ansible_psrp_proxy=...).
func (t *ProvisionTunnel) ProxyURL() string { return t.socks.ProxyURL() }

// SOCKSAddr returns "host:port" for the local SOCKS5 listener so non-Ansible
// callers (e.g. the Go winrm client) can build their own SOCKS5 dialer.
func (t *ProvisionTunnel) SOCKSAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", t.socks.Port)
}

// Close terminates the SOCKS5 listener, the underlying SSH connection to the
// controller, and the spawned `az network bastion tunnel` subprocess tree.
//
// Teardown runs exactly once even if Close is called concurrently. Callers
// reach Close through several paths (winrmRunner.close, the deferred Drain in
// `validate`, the deferred socksTunnel.Close in `provision`), so pipe closure
// and the wait for watchdog exit are serialized here rather than depending on
// every caller staying ordered.
func (t *ProvisionTunnel) Close() {
	t.closeOnce.Do(func() {
		if t.socks != nil {
			t.socks.Close()
		}
		killBastionTunnel(t.bastionProcess)
	})
}

// killGracePeriod is how long the tunnel process group gets to honor SIGTERM
// before the watchdog escalates to SIGKILL.
const killGracePeriod = 500 * time.Millisecond

func (p *bastionTunnelProcess) signalParentExit() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() {
		if p.parentWrite != nil {
			_ = p.parentWrite.Close()
		}
	})
}

// killBastionTunnel tells the watchdog to terminate the az process group and
// waits for the watchdog to reap it. Closing parentWrite is the same event the
// watchdog observes automatically if dreadgoad is killed without running its
// deferred cleanup.
func killBastionTunnel(p *bastionTunnelProcess) {
	if p == nil {
		return
	}
	p.signalParentExit()
	if p.done != nil {
		<-p.done
	} else if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Wait()
	}
}

// RunBastionWatchdog supervises command until it exits or parentLifetime is
// closed. It runs in a separate process so it survives a SIGKILL of dreadgoad.
// The command gets its own process group, allowing the watchdog to terminate
// both the az shell wrapper and the Python child without signalling itself.
//
// This is exported only for the hidden __bastion-watchdog CLI command.
func RunBastionWatchdog(parentLifetime *os.File, command []string) error {
	if parentLifetime == nil {
		return fmt.Errorf("parent lifetime pipe is unavailable")
	}
	defer func() {
		_ = parentLifetime.Close()
	}()
	if len(command) == 0 {
		return fmt.Errorf("watchdog command is required")
	}

	// The tunnel must not inherit the liveness descriptor. Only the watchdog
	// should own its read end; otherwise a descendant could keep it open after
	// the watchdog exits and obscure the ownership contract.
	syscall.CloseOnExec(int(parentLifetime.Fd()))

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start watched bastion tunnel: %w", err)
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil || pgid != cmd.Process.Pid {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("establish bastion tunnel process group: pgid=%d pid=%d err=%v", pgid, cmd.Process.Pid, err)
	}

	reaped := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(reaped)
	}()
	parentGone := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(io.Discard, parentLifetime)
		parentGone <- struct{}{}
	}()

	// The az launcher can exit while a descendant continues the tunnel in the
	// same process group. Keep the watchdog alive until the entire group exits,
	// not merely until cmd.Wait reports that the group leader is gone.
	poll := time.NewTicker(25 * time.Millisecond)
	defer poll.Stop()
	commandDone := (<-chan struct{})(reaped)
	leaderExited := false
	for {
		select {
		case <-commandDone:
			leaderExited = true
			commandDone = nil
			if !processGroupAlive(pgid) {
				return waitErr
			}
		case <-parentGone:
			terminateKnownProcessGroup(pgid, reaped)
			return nil
		case <-poll.C:
			if leaderExited && !processGroupAlive(pgid) {
				return waitErr
			}
		}
	}
}

// terminateProcessGroup reaps a command and every descendant that retained its
// process group. The az entry point is a shell wrapper that spawns Python, so a
// single-process kill is insufficient.
func terminateProcessGroup(cmd *exec.Cmd, reaped <-chan struct{}) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid

	// pgid == pid proves Setpgid took effect. Without that check, a cmd started
	// without SysProcAttr.Setpgid reports the watchdog's group, and the
	// negative-pid kill below would terminate the watchdog itself.
	pgid, err := syscall.Getpgid(pid)
	if err != nil || pgid != pid {
		_ = cmd.Process.Kill()
		<-reaped
		return
	}
	terminateKnownProcessGroup(pgid, reaped)
}

func terminateKnownProcessGroup(pgid int, reaped <-chan struct{}) {
	// Negative pid targets the entire process group (wrapper + python).
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	if !awaitGroupExit(pgid, killGracePeriod) {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	<-reaped
}

// awaitGroupExit polls until no member of pgid remains, or timeout elapses.
// Returns true if the group went away on its own, letting the caller skip the
// SIGKILL escalation — a tunnel that honors SIGTERM promptly costs a few
// milliseconds here instead of the full grace period.
func awaitGroupExit(pgid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !processGroupAlive(pgid) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func processGroupAlive(pgid int) bool {
	return syscall.Kill(-pgid, 0) != syscall.ESRCH
}

func startBastionTunnelProcess(ctx context.Context, command []string) (*bastionTunnelProcess, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("watchdog command is required")
	}

	parentRead, parentWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create watchdog pipe: %w", err)
	}

	executable, err := os.Executable()
	if err != nil {
		_ = parentRead.Close()
		_ = parentWrite.Close()
		return nil, fmt.Errorf("locate dreadgoad executable: %w", err)
	}

	args := append([]string{"__bastion-watchdog"}, command...)
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{parentRead}
	// The watchdog must survive the console killing dreadgoad's process group
	// long enough to observe pipe EOF and reap its own az child group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	process := &bastionTunnelProcess{
		cmd:         cmd,
		parentWrite: parentWrite,
		done:        make(chan struct{}),
	}
	// Context cancellation follows the same graceful watchdog path as Close.
	// WaitDelay is only a last-resort guard against a broken watchdog.
	cmd.Cancel = func() error {
		process.signalParentExit()
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	if err := cmd.Start(); err != nil {
		_ = parentRead.Close()
		process.signalParentExit()
		return nil, fmt.Errorf("start bastion watchdog: %w", err)
	}
	_ = parentRead.Close()
	go func() {
		process.waitErr = cmd.Wait()
		close(process.done)
	}()
	return process, nil
}

// StartProvisionTunnel discovers the in-VNet controller, opens a Bastion port
// tunnel to it, then layers a Go SOCKS5 listener on top whose dials are routed
// via SSH through the controller. Caller MUST Close() to release resources.
func StartProvisionTunnel(ctx context.Context, c *Client, env string) (*ProvisionTunnel, error) {
	bastion, err := c.DiscoverBastion(ctx, env)
	if err != nil {
		return nil, fmt.Errorf("discover bastion: %w", err)
	}
	if bastion == nil {
		return nil, fmt.Errorf("no Bastion deployed for env=%s; provisioning needs --with-bastion infra", env)
	}

	controller, err := c.findControllerInstance(ctx, env)
	if err != nil {
		return nil, err
	}

	localPort, err := pickFreePort()
	if err != nil {
		return nil, fmt.Errorf("pick free port: %w", err)
	}

	keyPath := defaultControllerKeyPath(env, controller.Name)
	if keyPath == "" {
		return nil, fmt.Errorf("controller ephemeral key not found at expected path; was 'infra apply' run?")
	}

	process, err := startBastionTunnelProcess(ctx, []string{
		"az", "network", "bastion", "tunnel",
		"--name", bastion.Name,
		"--resource-group", bastion.ResourceGroup,
		"--target-resource-id", controller.ID,
		"--resource-port", "22",
		"--port", strconv.Itoa(localPort),
	})
	if err != nil {
		return nil, err
	}

	if err := waitForLocalPort(ctx, process, localPort, 60*time.Second); err != nil {
		killBastionTunnel(process)
		return nil, fmt.Errorf("bastion tunnel never came up on :%d: %w", localPort, err)
	}

	sshCfg := ludus.SSHConfig{
		Host:                  "127.0.0.1",
		Port:                  localPort,
		User:                  "dreadadmin",
		KeyPath:               keyPath,
		InsecureIgnoreHostKey: true, // Bastion tunnel rebinds a fresh port per session.
		IdentitiesOnly:        true, // Skip ssh-agent so its keys don't blow MaxAuthTries.
	}
	socks, err := ludus.StartSOCKSTunnel(sshCfg)
	if err != nil {
		killBastionTunnel(process)
		return nil, fmt.Errorf("start SOCKS5 over controller: %w", err)
	}

	return &ProvisionTunnel{socks: socks, bastionProcess: process, localPort: localPort}, nil
}

// findControllerInstance locates the Ansible controller VM (Role=AnsibleController
// tag) for the given env. Required to know which target-resource-id to feed
// `az network bastion tunnel`.
func (c *Client) findControllerInstance(ctx context.Context, env string) (*Instance, error) {
	instances, err := c.DiscoverInstances(ctx, env, true)
	if err != nil {
		return nil, fmt.Errorf("discover instances: %w", err)
	}
	for _, inst := range instances {
		if inst.Tags["Role"] == "AnsibleController" {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("no Ansible controller VM found for env=%s", env)
}

// defaultControllerKeyPath mirrors cmd/bastion.go's controllerKeyPath. Kept
// here (rather than imported) so this package has no dep back into cmd/.
func defaultControllerKeyPath(env, vmName string) string {
	deployment := strings.TrimSuffix(strings.TrimPrefix(vmName, env+"-"), "-controller-vm")
	if deployment == "" || deployment == vmName {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".dreadgoad", "keys", fmt.Sprintf("azure-%s-%s-controller", env, deployment))
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

func pickFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		return 0, err
	}
	return port, nil
}

func waitForLocalPort(ctx context.Context, process *bastionTunnelProcess, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-process.done:
			if process.waitErr != nil {
				return fmt.Errorf("bastion watchdog exited before tunnel became ready: %w", process.waitErr)
			}
			return fmt.Errorf("bastion watchdog exited before tunnel became ready")
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timed out waiting for %s", addr)
}
