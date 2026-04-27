package ludus

import (
	"fmt"
	"net"
	"os/exec"
	"time"
)

// SOCKSTunnel manages an SSH dynamic port-forwarding tunnel (SOCKS5 proxy)
// that lets a local ansible-playbook reach WinRM endpoints on the Ludus VLAN.
type SOCKSTunnel struct {
	Port int
	cmd  *exec.Cmd
}

// StartSOCKSTunnel opens an SSH SOCKS5 tunnel to the given host. The tunnel
// runs in the background and must be closed with Close() when done.
func StartSOCKSTunnel(sshCfg SSHConfig) (*SOCKSTunnel, error) {
	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("find free port for SOCKS tunnel: %w", err)
	}

	args := buildSOCKSArgs(sshCfg, port)

	bin := "ssh"
	if sshCfg.Password != "" {
		args = append([]string{"-p", sshCfg.Password, "ssh"}, args...)
		bin = "sshpass"
	}

	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start SOCKS tunnel: %w", err)
	}

	// Wait briefly for the tunnel to establish, then verify it's listening.
	if err := waitForPort(port, 10*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("SOCKS tunnel failed to start on port %d: %w", port, err)
	}

	return &SOCKSTunnel{Port: port, cmd: cmd}, nil
}

// ProxyURL returns the SOCKS5 proxy URL for use with ansible_psrp_proxy.
func (t *SOCKSTunnel) ProxyURL() string {
	return fmt.Sprintf("socks5h://localhost:%d", t.Port)
}

// Close terminates the SSH tunnel process.
func (t *SOCKSTunnel) Close() {
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}
}

// buildSOCKSArgs constructs SSH arguments for a SOCKS5 dynamic port-forward.
// It mirrors buildSSHArgs from client.go but adds -D (dynamic forward) and
// -N (no remote command).
func buildSOCKSArgs(cfg SSHConfig, port int) []string {
	var args []string

	args = append(args, "-D", fmt.Sprintf("%d", port))
	args = append(args, "-N")                         // no remote command
	args = append(args, "-o", "LogLevel=ERROR")       // suppress banners
	args = append(args, "-o", "ExitOnForwardFailure=yes")

	hasOverrides := cfg.User != "" || cfg.Port != 0 || cfg.KeyPath != "" || cfg.Password != ""

	if hasOverrides {
		args = append(args, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null")
		if cfg.Password != "" {
			args = append(args, "-o", "IdentitiesOnly=yes")
		}
		if cfg.KeyPath != "" {
			args = append(args, "-i", cfg.KeyPath)
		}
		if cfg.Port != 0 && cfg.Port != 22 {
			args = append(args, "-p", fmt.Sprintf("%d", cfg.Port))
		}

		user := cfg.User
		if user == "" {
			user = "root"
		}
		args = append(args, fmt.Sprintf("%s@%s", user, cfg.Host))
	} else {
		args = append(args, cfg.Host)
	}

	return args
}

// freePort asks the OS for an available TCP port.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

// waitForPort polls until something is listening on localhost:port.
func waitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("port %d not reachable after %s", port, timeout)
}
