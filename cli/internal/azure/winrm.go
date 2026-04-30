package azure

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/masterzen/winrm"
	"golang.org/x/net/proxy"
)

// winrmRunner executes PowerShell on Azure GOAD VMs via WinRM-over-NTLM
// routed through the Bastion → controller → SOCKS5 chain that already exists
// for Ansible (see provision_tunnel.go). It is the Azure analogue of the
// AWS path through SSM: opaque to the validator, which only ever sees
// AzureProvider.RunCommand returning a CommandResult.
//
// Why this exists: Azure managed Run Commands take 15–30s per call (PUT →
// LRO → GET → DELETE → 404-poll), and the 25-per-VM hard limit caps
// per-VM throughput at ~36 calls/min — fine for occasional ops, terrible
// for the validator's 100+ tiny probes. WinRM through the existing tunnel
// is sub-second per command and parallelizes freely.
type winrmRunner struct {
	client        *Client
	env           string
	inventoryPath string

	initOnce sync.Once
	initErr  error

	tunnel *ProvisionTunnel
	dialer proxy.Dialer

	// ipByVMID maps ARM resource ID → private IP, populated once at init
	// from DiscoverInstances. The validator hands us ARM IDs; the inventory
	// keys hosts by IP, so this map bridges the two namespaces.
	ipByVMID map[string]string
	// credsByIP maps private IP → (user, password) sourced from the same
	// Ansible inventory used by `provision`.
	credsByIP map[string]hostCreds

	// winrmByVMID caches a *winrm.Client per VM. Each client owns an
	// http.Client whose Transport pools connections to the SOCKS5
	// listener, so subsequent calls reuse the established TCP/NTLM
	// state instead of paying handshake cost per command.
	mu          sync.Mutex
	winrmByVMID map[string]*winrm.Client
}

type hostCreds struct {
	user     string
	password string
}

func newWinRMRunner(c *Client, env, inventoryPath string) *winrmRunner {
	return &winrmRunner{
		client:        c,
		env:           env,
		inventoryPath: inventoryPath,
		winrmByVMID:   make(map[string]*winrm.Client),
	}
}

// init lazily brings up the provision tunnel and assembles the lookup maps
// on first use. Errors are cached in initErr so a busted setup fails every
// check identically with one root-cause message rather than retrying tunnel
// setup per call.
func (r *winrmRunner) init(ctx context.Context) error {
	r.initOnce.Do(func() {
		r.initErr = r.doInit(ctx)
	})
	return r.initErr
}

func (r *winrmRunner) doInit(ctx context.Context) error {
	if err := r.validateConfig(); err != nil {
		return err
	}

	credsByIP, err := r.loadInventoryCreds()
	if err != nil {
		return err
	}

	ipByVMID, err := r.discoverIPMapping(ctx)
	if err != nil {
		return err
	}

	slog.Info("opening Bastion → controller → SOCKS5 chain for WinRM validation", "env", r.env)
	tunnel, err := StartProvisionTunnel(ctx, r.client, r.env)
	if err != nil {
		return fmt.Errorf("start provision tunnel: %w", err)
	}

	dialer, err := proxy.SOCKS5("tcp", tunnel.SOCKSAddr(), nil, proxy.Direct)
	if err != nil {
		tunnel.Close()
		return fmt.Errorf("build SOCKS5 dialer: %w", err)
	}

	r.tunnel = tunnel
	r.dialer = dialer
	r.ipByVMID = ipByVMID
	r.credsByIP = credsByIP
	return nil
}

func (r *winrmRunner) validateConfig() error {
	if r.env == "" {
		return fmt.Errorf("winrm runner requires env (was the AzureProvider constructed without it?)")
	}
	if r.inventoryPath == "" {
		return fmt.Errorf("winrm runner requires inventory path; ensure provision was run for env=%s", r.env)
	}
	if _, err := os.Stat(r.inventoryPath); err != nil {
		return fmt.Errorf("inventory not found at %s: %w (run `dreadgoad infra apply` + `dreadgoad provision` first)", r.inventoryPath, err)
	}
	return nil
}

func (r *winrmRunner) loadInventoryCreds() (map[string]hostCreds, error) {
	inv, err := inventory.Parse(r.inventoryPath)
	if err != nil {
		return nil, fmt.Errorf("parse inventory: %w", err)
	}
	defaultUser := inv.Vars["ansible_user"]
	if defaultUser == "" {
		defaultUser = "ansible"
	}
	credsByIP := make(map[string]hostCreds, len(inv.Hosts))
	for _, h := range inv.Hosts {
		if h.InstanceID == "" || h.Password == "" {
			continue
		}
		credsByIP[h.InstanceID] = buildHostCreds(h.User, h.Password, defaultUser)
	}
	if len(credsByIP) == 0 {
		return nil, fmt.Errorf("no usable credentials in %s (need ansible_host + ansible_password per host)", r.inventoryPath)
	}
	return credsByIP, nil
}

// buildHostCreds normalizes the username for NTLM. On member servers `ansible`
// is a local-SAM account; sending NTLM Type 3 with no Domain hint causes the
// server to forward auth to its AD domain, where `ansible` doesn't exist (1326
// access denied). On a DC there is no separate local SAM — `ansible` lives in
// AD — so the `.` hint is harmless. Prefix any bare username (no `\` or `@`)
// with `.\` to force local-first lookup uniformly.
func buildHostCreds(user, password, defaultUser string) hostCreds {
	if user == "" {
		user = defaultUser
	}
	if !strings.ContainsAny(user, "\\@") {
		user = `.\` + user
	}
	return hostCreds{user: user, password: password}
}

func (r *winrmRunner) discoverIPMapping(ctx context.Context) (map[string]string, error) {
	instances, err := r.client.DiscoverInstances(ctx, r.env, true)
	if err != nil {
		return nil, fmt.Errorf("discover instances for env=%s: %w", r.env, err)
	}
	ipByVMID := make(map[string]string, len(instances))
	for _, inst := range instances {
		if inst.PrivateIP != "" {
			ipByVMID[inst.ID] = inst.PrivateIP
		}
	}
	if len(ipByVMID) == 0 {
		return nil, fmt.Errorf("no Azure instances with private IPs found for env=%s", r.env)
	}
	return ipByVMID, nil
}

// runPS executes a PowerShell script on the VM identified by ARM resource
// ID. Returns a CommandResult shaped identically to the managed Run Command
// path so the provider seam doesn't change.
func (r *winrmRunner) runPS(ctx context.Context, vmID, script string, timeout time.Duration) (*CommandResult, error) {
	if err := r.init(ctx); err != nil {
		return nil, err
	}
	ip, ok := r.ipByVMID[vmID]
	if !ok {
		return nil, fmt.Errorf("vm %s not in azure-discovered map for env=%s", vmID, r.env)
	}
	creds, ok := r.credsByIP[ip]
	if !ok {
		return nil, fmt.Errorf("no inventory credentials for vm %s (ip=%s)", vmID, ip)
	}

	cli, err := r.clientFor(vmID, ip, creds)
	if err != nil {
		return nil, err
	}

	callCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	stdout, stderr, exitCode, err := cli.RunPSWithContext(callCtx, script)
	if err != nil {
		return nil, fmt.Errorf("winrm run on %s: %w", ip, err)
	}
	status := "Success"
	if exitCode != 0 {
		status = "Failed"
	}
	return &CommandResult{Status: status, Stdout: stdout, Stderr: stderr}, nil
}

// clientFor returns a cached winrm client for vmID, building one on first
// use. The TransportDecorator captures r.dialer so every NTLM-wrapped HTTP
// request flows out through the SOCKS5 listener.
func (r *winrmRunner) clientFor(vmID, ip string, creds hostCreds) (*winrm.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cli, ok := r.winrmByVMID[vmID]; ok {
		return cli, nil
	}
	endpoint := winrm.NewEndpoint(ip, 5985, false, true, nil, nil, nil, 0)
	params := *winrm.DefaultParameters
	dialer := r.dialer
	params.TransportDecorator = func() winrm.Transporter {
		return winrm.NewClientNTLMWithDial(dialer.Dial)
	}
	cli, err := winrm.NewClientWithParameters(endpoint, creds.user, creds.password, &params)
	if err != nil {
		return nil, fmt.Errorf("winrm client for %s: %w", ip, err)
	}
	r.winrmByVMID[vmID] = cli
	return cli, nil
}

// close tears down the tunnel and clears caches. Safe to call multiple times.
func (r *winrmRunner) close() {
	r.mu.Lock()
	r.winrmByVMID = make(map[string]*winrm.Client)
	r.mu.Unlock()
	if r.tunnel != nil {
		r.tunnel.Close()
		r.tunnel = nil
	}
}
