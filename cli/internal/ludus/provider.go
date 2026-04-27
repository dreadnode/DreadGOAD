package ludus

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

func init() {
	provider.Register(provider.NameLudus, func(ctx context.Context, opts provider.ConstructorOpts) (provider.Provider, error) {
		if opts.LudusAPIKey == "" {
			return nil, fmt.Errorf("ludus API key is required (set ludus.api_key in dreadgoad.yaml or export LUDUS_API_KEY)")
		}

		sshCfg := SSHConfig{
			Host:     opts.LudusSSHHost,
			User:     opts.LudusSSHUser,
			KeyPath:  opts.LudusSSHKeyPath,
			Password: opts.LudusSSHPassword,
			Port:     opts.LudusSSHPort,
		}

		client, err := NewClient(ctx, opts.LudusAPIKey, opts.LudusUseImpersonation, sshCfg)
		if err != nil {
			return nil, err
		}

		return &LudusProvider{client: client, inventoryPath: opts.InventoryPath}, nil
	})
}

// LudusProvider implements the Provider interface for Ludus.
type LudusProvider struct {
	client        *Client
	inventoryPath string

	// Cached range status to avoid repeated API calls during bulk operations
	// (e.g., validate runs dozens of commands).
	statusMu    sync.Mutex
	cachedVMs   map[string]VM // proxmoxID (string) -> VM
	statusStale time.Time     // refresh after this time
}

const statusCacheTTL = 30 * time.Minute

// refreshVMs fetches range status and populates the VM cache. When SSH is
// configured and any VM has no IP from `range status` (Ludus indexes IPs
// lazily, especially right after deployment), we patch IPs from the
// authoritative /opt/ludus/ranges/<rangeID>/etc-hosts file in one shot.
func (p *LudusProvider) refreshVMs(ctx context.Context) error {
	status, err := p.client.RangeStatusJSON(ctx)
	if err != nil {
		return err
	}
	m := make(map[string]VM, len(status.VMs))
	missingIP := false
	for _, vm := range status.VMs {
		m[strconv.Itoa(vm.ProxmoxID)] = vm
		if vm.IP == "" || vm.IP == "null" {
			missingIP = true
		}
	}

	if missingIP && p.client.SSH().IsConfigured() && status.RangeID != "" {
		hosts, herr := p.client.RangeEtcHosts(ctx, status.RangeID)
		if herr == nil {
			for id, vm := range m {
				if vm.IP != "" && vm.IP != "null" {
					continue
				}
				if ip, ok := hosts[vm.Name]; ok {
					vm.IP = ip
					m[id] = vm
				}
			}
		}
	}

	p.cachedVMs = m
	p.statusStale = time.Now().Add(statusCacheTTL)
	return nil
}

// getVMs returns the cached VM map, refreshing if stale.
func (p *LudusProvider) getVMs(ctx context.Context) (map[string]VM, error) {
	p.statusMu.Lock()
	defer p.statusMu.Unlock()
	if p.cachedVMs == nil || time.Now().After(p.statusStale) {
		if err := p.refreshVMs(ctx); err != nil {
			return nil, err
		}
	}
	return p.cachedVMs, nil
}

// invalidateCache forces the next getVMs call to refresh.
func (p *LudusProvider) invalidateCache() {
	p.statusMu.Lock()
	defer p.statusMu.Unlock()
	p.cachedVMs = nil
}

func (p *LudusProvider) Name() string { return provider.NameLudus }

func (p *LudusProvider) VerifyCredentials(ctx context.Context) (string, error) {
	out, err := p.client.VerifyConnection(ctx)
	if err != nil {
		return "", err
	}
	mode := "local"
	if p.client.SSH().IsConfigured() {
		mode = fmt.Sprintf("ssh:%s", p.client.SSH().Host)
	}
	return fmt.Sprintf("Ludus v%d (%s) [%s]", p.client.MajorVersion(), strings.TrimSpace(out), mode), nil
}

func (p *LudusProvider) DiscoverInstances(ctx context.Context, env string) ([]provider.Instance, error) {
	return p.discoverInstances(ctx, true)
}

func (p *LudusProvider) DiscoverAllInstances(ctx context.Context, env string) ([]provider.Instance, error) {
	return p.discoverInstances(ctx, false)
}

func (p *LudusProvider) discoverInstances(ctx context.Context, runningOnly bool) ([]provider.Instance, error) {
	vms, err := p.getVMs(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover instances: %w", err)
	}

	var instances []provider.Instance
	for _, vm := range vms {
		state := "stopped"
		if vm.PoweredOn {
			state = "running"
		}
		if runningOnly && state != "running" {
			continue
		}

		instances = append(instances, provider.Instance{
			ID:        strconv.Itoa(vm.ProxmoxID),
			Name:      vm.Name,
			PrivateIP: vm.IP,
			State:     state,
		})
	}
	return instances, nil
}

func (p *LudusProvider) FindInstanceByHostname(ctx context.Context, env, hostname string) (*provider.Instance, error) {
	instances, err := p.DiscoverAllInstances(ctx, env)
	if err != nil {
		return nil, err
	}
	hostname = strings.ToUpper(hostname)
	var matches []provider.Instance
	for _, inst := range instances {
		// Extract the role suffix (last segment after "-") for exact matching.
		parts := strings.Split(inst.Name, "-")
		role := strings.ToUpper(parts[len(parts)-1])
		if role == hostname {
			matches = append(matches, inst)
		}
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return nil, fmt.Errorf("hostname %q matched multiple VMs: %s", hostname, strings.Join(names, ", "))
	}
	return nil, fmt.Errorf("VM not found for hostname %s", hostname)
}

func (p *LudusProvider) StartInstances(ctx context.Context, ids []string) error {
	vms, err := p.getVMs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		vm, ok := vms[id]
		if !ok {
			return fmt.Errorf("VM with Proxmox ID %s not found in range", id)
		}
		if err := p.client.PowerOn(ctx, vm.Name); err != nil {
			return fmt.Errorf("start VM %s (%s): %w", vm.Name, id, err)
		}
	}
	p.invalidateCache()
	return nil
}

func (p *LudusProvider) StopInstances(ctx context.Context, ids []string) error {
	vms, err := p.getVMs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		vm, ok := vms[id]
		if !ok {
			return fmt.Errorf("VM with Proxmox ID %s not found in range", id)
		}
		if err := p.client.PowerOff(ctx, vm.Name); err != nil {
			return fmt.Errorf("stop VM %s (%s): %w", vm.Name, id, err)
		}
	}
	p.invalidateCache()
	return nil
}

func (p *LudusProvider) WaitForInstanceStopped(ctx context.Context, id string) error {
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		// Must bypass cache to get fresh power state.
		p.invalidateCache()
		vms, err := p.getVMs(ctx)
		if err != nil {
			return err
		}
		if vm, ok := vms[id]; ok && !vm.PoweredOn {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return fmt.Errorf("VM %s did not stop within 5 minutes", id)
}

func (p *LudusProvider) DestroyInstances(ctx context.Context, ids []string) error {
	for _, id := range ids {
		vmid, err := strconv.Atoi(id)
		if err != nil {
			return fmt.Errorf("invalid Proxmox ID %q: %w", id, err)
		}
		if err := p.client.VMDestroy(ctx, vmid); err != nil {
			return fmt.Errorf("destroy VM %d: %w", vmid, err)
		}
	}
	p.invalidateCache()
	return nil
}

// resolveHostname maps a Proxmox VMID to the Ansible inventory hostname
// (e.g. "104" -> "dc01") by extracting the role suffix from the Ludus VM
// name. Ludus VM names follow the pattern "{range_id}-{lab}-{ROLE}"
// (e.g. "DG-GOAD-DC01"), so the role is the last hyphen-separated segment.
func (p *LudusProvider) resolveHostname(ctx context.Context, instanceID string) (string, error) {
	vms, err := p.getVMs(ctx)
	if err != nil {
		return "", err
	}
	vm, ok := vms[instanceID]
	if !ok {
		return "", fmt.Errorf("VM with Proxmox ID %s not found", instanceID)
	}

	// Extract role from the last segment: "DG-GOAD-DC01" -> "dc01"
	parts := strings.Split(vm.Name, "-")
	if len(parts) < 2 {
		return "", fmt.Errorf("VM name %q does not follow expected naming pattern (RANGEID-LAB-ROLE)", vm.Name)
	}
	role := strings.ToLower(parts[len(parts)-1])
	if role == "" {
		return "", fmt.Errorf("VM name %q has empty role suffix", vm.Name)
	}
	return role, nil
}

// resolveVM returns both the hostname and IP for a given instance ID. IPs
// are populated up front by refreshVMs (including the etc-hosts fallback
// when Ludus's range status returns null), so no per-call fallback runs here.
func (p *LudusProvider) resolveVM(ctx context.Context, instanceID string) (hostname, ip string, err error) {
	vms, err := p.getVMs(ctx)
	if err != nil {
		return "", "", err
	}
	vm, ok := vms[instanceID]
	if !ok {
		return "", "", fmt.Errorf("VM with Proxmox ID %s not found", instanceID)
	}

	parts := strings.Split(vm.Name, "-")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("VM name %q does not follow expected naming pattern (RANGEID-LAB-ROLE)", vm.Name)
	}
	role := strings.ToLower(parts[len(parts)-1])
	if role == "" {
		return "", "", fmt.Errorf("VM name %q has empty role suffix", vm.Name)
	}
	return role, vm.IP, nil
}

func (p *LudusProvider) RunCommand(ctx context.Context, instanceID, command string, timeout time.Duration) (*provider.CommandResult, error) {
	if p.client.SSH().IsConfigured() {
		return p.runCommandSSH(ctx, instanceID, command, timeout)
	}
	return p.runCommandLocal(ctx, instanceID, command, timeout)
}

// runCommandLocal executes a command via local ansible (original behavior).
func (p *LudusProvider) runCommandLocal(ctx context.Context, instanceID, command string, timeout time.Duration) (*provider.CommandResult, error) {
	hostname, err := p.resolveHostname(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	if p.inventoryPath == "" {
		return nil, fmt.Errorf("inventory path not configured; ensure provider was created with InventoryPath set")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		hostname,
		"-i", p.inventoryPath,
		"-m", "win_shell",
		"-a", command,
	}

	cmd := exec.CommandContext(cmdCtx, "ansible", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	outStr := stdout.String()

	res := parseAnsibleOutput(outStr, stderr.String(), err)
	if res.Status == "Failed" {
		return res, fmt.Errorf("ansible win_shell on %s failed: %s", hostname, firstNonEmpty(res.Stderr, outStr))
	}
	return res, nil
}

// runCommandSSH executes a command on a VM via SSH to the Ludus host, using
// an inline ansible inventory constructed from the VM's IP address. This
// removes the need for an inventory file on the remote host.
func (p *LudusProvider) runCommandSSH(ctx context.Context, instanceID, command string, timeout time.Duration) (*provider.CommandResult, error) {
	_, ip, err := p.resolveVM(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if ip == "" || ip == "null" {
		return nil, fmt.Errorf("VM %s has no IP address; is it running?", instanceID)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Use ansible with an inline inventory (IP followed by comma) and
	// pass WinRM connection variables directly. This avoids needing an
	// inventory file on the remote Ludus host.
	args := []string{
		"all",
		"-i", ip + ",",
		"-m", "win_shell",
		"-a", command,
		"-e", "ansible_user=localuser",
		"-e", "ansible_password=password",
		"-e", "ansible_connection=winrm",
		"-e", "ansible_winrm_server_cert_validation=ignore",
		"-e", "ansible_winrm_transport=ntlm",
		"-e", "ansible_winrm_scheme=http",
		"-e", "ansible_port=5985",
		"-e", "ansible_winrm_operation_timeout_sec=400",
		"-e", "ansible_winrm_read_timeout_sec=500",
	}

	stdoutStr, stderrStr, runErr := p.client.RunSSHCommand(cmdCtx, "ansible", args...)

	res := parseAnsibleOutput(stdoutStr, stderrStr, runErr)
	if res.Status == "Failed" {
		return res, fmt.Errorf("ansible win_shell on %s failed: %s", ip, firstNonEmpty(res.Stderr, stdoutStr))
	}
	return res, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// parseAnsibleOutput extracts command results from ansible ad-hoc output.
func parseAnsibleOutput(stdoutStr, stderrStr string, runErr error) *provider.CommandResult {
	result := &provider.CommandResult{
		Status: "Success",
		Stderr: stderrStr,
	}

	lines := strings.SplitN(stdoutStr, "\n", 2)
	if len(lines) > 0 {
		header := lines[0]
		if strings.Contains(header, "FAILED") || strings.Contains(header, "UNREACHABLE") {
			result.Status = "Failed"
		}
		if len(lines) > 1 {
			result.Stdout = strings.TrimSpace(lines[1])
		}
	}

	if runErr != nil && result.Status == "Success" {
		result.Status = "Failed"
		if result.Stdout == "" {
			result.Stdout = stdoutStr
		}
	}

	return result
}

func (p *LudusProvider) RunCommandOnMultiple(ctx context.Context, instanceIDs []string, command string, timeout time.Duration) (map[string]*provider.CommandResult, error) {
	results := make(map[string]*provider.CommandResult, len(instanceIDs))
	for _, id := range instanceIDs {
		result, err := p.RunCommand(ctx, id, command, timeout)
		if err != nil {
			results[id] = &provider.CommandResult{
				Status: "Failed",
				Stderr: err.Error(),
			}
			continue
		}
		results[id] = result
	}
	return results, nil
}

// IPRange returns the Ludus range IP prefix (e.g. "10.2.10") for inventory generation.
func (p *LudusProvider) IPRange(ctx context.Context) (string, error) {
	status, err := p.client.RangeStatusJSON(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("10.%d.10", status.RangeNumber), nil
}

// Client returns the underlying Ludus client for range-level operations.
func (p *LudusProvider) Client() *Client { return p.client }

// IsSSH returns true when the provider is configured for remote execution.
func (p *LudusProvider) IsSSH() bool { return p.client.SSH().IsConfigured() }

// SSHConfig returns the SSH configuration for the underlying client.
// This is used by the provision command to set up a SOCKS proxy tunnel.
func (p *LudusProvider) SSHConfig() SSHConfig { return p.client.SSH() }

// Compile-time interface checks.
var _ provider.Provider = (*LudusProvider)(nil)
