package azure

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/ludus"
)

// ProvisionTunnel chains an Azure Bastion port-forward (laptop → controller:22)
// with a SOCKS5 proxy that dials from the controller's network position. The
// controller sits in the same VNet as the GOAD VMs, so SOCKS-routed WinRM
// traffic reaches private 5985 listeners that the laptop can't touch directly.
type ProvisionTunnel struct {
	socks      *ludus.SOCKSTunnel
	bastionCmd *exec.Cmd
	localPort  int
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
// controller, and the spawned `az network bastion tunnel` subprocess.
func (t *ProvisionTunnel) Close() {
	if t.socks != nil {
		t.socks.Close()
	}
	if t.bastionCmd != nil && t.bastionCmd.Process != nil {
		_ = t.bastionCmd.Process.Kill()
		_ = t.bastionCmd.Wait()
	}
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

	cmd := exec.Command("az", "network", "bastion", "tunnel",
		"--name", bastion.Name,
		"--resource-group", bastion.ResourceGroup,
		"--target-resource-id", controller.ID,
		"--resource-port", "22",
		"--port", strconv.Itoa(localPort))
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start bastion tunnel: %w", err)
	}

	if err := waitForLocalPort(ctx, localPort, 60*time.Second); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
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
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("start SOCKS5 over controller: %w", err)
	}

	return &ProvisionTunnel{socks: socks, bastionCmd: cmd, localPort: localPort}, nil
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

func waitForLocalPort(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
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
