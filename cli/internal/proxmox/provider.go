package proxmox

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

func init() {
	provider.Register(provider.NameProxmox, func(ctx context.Context, opts provider.ConstructorOpts) (provider.Provider, error) {
		if opts.ProxmoxAPIURL == "" {
			return nil, fmt.Errorf("proxmox API URL is required")
		}
		if opts.ProxmoxPass == "" {
			return nil, fmt.Errorf("proxmox password is required (set DREADGOAD_PROXMOX_PASSWORD or proxmox.password in config)")
		}
		user := opts.ProxmoxUser
		if user == "" {
			user = "root@pam"
		}
		node := opts.ProxmoxNode
		if node == "" {
			node = "pve"
		}
		pool := opts.ProxmoxPool
		if pool == "" {
			pool = "GOAD"
		}

		client, err := NewClient(ctx, opts.ProxmoxAPIURL, user, opts.ProxmoxPass, node, pool)
		if err != nil {
			return nil, err
		}
		return &ProxmoxProvider{client: client}, nil
	})
}

// ProxmoxProvider implements the Provider interface for Proxmox VE.
type ProxmoxProvider struct {
	client *Client
}

func (p *ProxmoxProvider) Name() string { return provider.NameProxmox }

func (p *ProxmoxProvider) VerifyCredentials(ctx context.Context) (string, error) {
	// Authentication happens during client creation. If we're here, it worked.
	return fmt.Sprintf("Proxmox node %s (user: %s)", p.client.Node(), p.client.User()), nil
}

func (p *ProxmoxProvider) DiscoverInstances(ctx context.Context, env string) ([]provider.Instance, error) {
	members, err := p.client.PoolMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover instances: %w", err)
	}

	var instances []provider.Instance
	for _, vm := range members {
		if vm.Type != "qemu" && vm.Type != "lxc" {
			continue
		}

		status, err := p.client.VMStatus(ctx, vm.Type, vm.VMID)
		if err != nil {
			continue
		}

		if status != "running" {
			continue
		}

		inst := provider.Instance{
			ID:    strconv.Itoa(vm.VMID),
			Name:  vm.Name,
			State: status,
		}

		// Try to get the IP via guest agent for QEMU VMs.
		if vm.Type == "qemu" {
			if ip := p.getVMIP(ctx, vm.VMID); ip != "" {
				inst.PrivateIP = ip
			}
		}

		instances = append(instances, inst)
	}
	return instances, nil
}

func (p *ProxmoxProvider) DiscoverAllInstances(ctx context.Context, env string) ([]provider.Instance, error) {
	members, err := p.client.PoolMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover instances: %w", err)
	}

	var instances []provider.Instance
	for _, vm := range members {
		if vm.Type != "qemu" && vm.Type != "lxc" {
			continue
		}

		status, err := p.client.VMStatus(ctx, vm.Type, vm.VMID)
		if err != nil {
			status = "unknown"
		}

		inst := provider.Instance{
			ID:    strconv.Itoa(vm.VMID),
			Name:  vm.Name,
			State: status,
		}

		if status == "running" && vm.Type == "qemu" {
			if ip := p.getVMIP(ctx, vm.VMID); ip != "" {
				inst.PrivateIP = ip
			}
		}

		instances = append(instances, inst)
	}
	return instances, nil
}

func (p *ProxmoxProvider) FindInstanceByHostname(ctx context.Context, env, hostname string) (*provider.Instance, error) {
	instances, err := p.DiscoverAllInstances(ctx, env)
	if err != nil {
		return nil, err
	}
	hostname = strings.ToUpper(hostname)
	for _, inst := range instances {
		if strings.Contains(strings.ToUpper(inst.Name), hostname) {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("VM not found for hostname %s in pool %s", hostname, p.client.pool)
}

func (p *ProxmoxProvider) StartInstances(ctx context.Context, ids []string) error {
	for _, id := range ids {
		vmid, vmType, err := p.resolveVMID(ctx, id)
		if err != nil {
			return fmt.Errorf("start VM %s: %w", id, err)
		}
		if err := p.client.StartVM(ctx, vmType, vmid); err != nil {
			return fmt.Errorf("start VM %d: %w", vmid, err)
		}
	}
	return nil
}

func (p *ProxmoxProvider) StopInstances(ctx context.Context, ids []string) error {
	for _, id := range ids {
		vmid, vmType, err := p.resolveVMID(ctx, id)
		if err != nil {
			return fmt.Errorf("stop VM %s: %w", id, err)
		}
		if err := p.client.StopVM(ctx, vmType, vmid); err != nil {
			return fmt.Errorf("stop VM %d: %w", vmid, err)
		}
	}
	return nil
}

func (p *ProxmoxProvider) WaitForInstanceStopped(ctx context.Context, id string) error {
	vmid, vmType, err := p.resolveVMID(ctx, id)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		status, err := p.client.VMStatus(ctx, vmType, vmid)
		if err != nil {
			return err
		}
		if status == "stopped" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return fmt.Errorf("VM %d did not stop within 5 minutes", vmid)
}

func (p *ProxmoxProvider) DestroyInstances(ctx context.Context, ids []string) error {
	for _, id := range ids {
		vmid, vmType, err := p.resolveVMID(ctx, id)
		if err != nil {
			return fmt.Errorf("destroy VM %s: %w", id, err)
		}
		// Stop first if running.
		status, _ := p.client.VMStatus(ctx, vmType, vmid)
		if status == "running" {
			if err := p.client.StopVM(ctx, vmType, vmid); err != nil {
				return fmt.Errorf("stop VM %d before destroy: %w", vmid, err)
			}
			if err := p.WaitForInstanceStopped(ctx, id); err != nil {
				return err
			}
		}
		if err := p.client.DestroyVM(ctx, vmType, vmid); err != nil {
			return fmt.Errorf("destroy VM %d: %w", vmid, err)
		}
	}
	return nil
}

func (p *ProxmoxProvider) RunCommand(ctx context.Context, instanceID, command string, timeout time.Duration) (*provider.CommandResult, error) {
	vmid, err := strconv.Atoi(instanceID)
	if err != nil {
		return nil, fmt.Errorf("invalid VMID %q: %w", instanceID, err)
	}

	stdout, stderr, err := p.client.QEMUAgentExec(ctx, vmid, command, timeout)
	if err != nil {
		return nil, err
	}

	status := "Success"
	if stderr != "" && stdout == "" {
		status = "Failed"
	}

	return &provider.CommandResult{
		Status: status,
		Stdout: stdout,
		Stderr: stderr,
	}, nil
}

func (p *ProxmoxProvider) RunCommandOnMultiple(ctx context.Context, instanceIDs []string, command string, timeout time.Duration) (map[string]*provider.CommandResult, error) {
	results := make(map[string]*provider.CommandResult, len(instanceIDs))
	for _, id := range instanceIDs {
		result, err := p.RunCommand(ctx, id, command, timeout)
		if err != nil {
			results[id] = &provider.CommandResult{Status: "Error", Stderr: err.Error()}
		} else {
			results[id] = result
		}
	}
	return results, nil
}

// getVMIP tries to get the first non-loopback IP from a QEMU VM's guest agent.
func (p *ProxmoxProvider) getVMIP(ctx context.Context, vmid int) string {
	ifaces, err := p.client.QEMUAgentGetInterfaces(ctx, vmid)
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		for _, addr := range iface.IPAddresses {
			ip := addr.IPAddress
			if ip == "" || strings.HasPrefix(ip, "127.") || strings.HasPrefix(ip, "::1") || strings.HasPrefix(ip, "fe80") {
				continue
			}
			return ip
		}
	}
	return ""
}

// resolveVMID converts a string ID to VMID and determines the VM type (qemu/lxc).
func (p *ProxmoxProvider) resolveVMID(ctx context.Context, id string) (int, string, error) {
	vmid, err := strconv.Atoi(id)
	if err != nil {
		return 0, "", fmt.Errorf("invalid VMID %q: %w", id, err)
	}

	members, err := p.client.PoolMembers(ctx)
	if err != nil {
		return 0, "", err
	}

	for _, m := range members {
		if m.VMID == vmid {
			return vmid, m.Type, nil
		}
	}

	// Default to qemu if not found in pool.
	return vmid, "qemu", nil
}

// Compile-time interface check.
var _ provider.Provider = (*ProxmoxProvider)(nil)
