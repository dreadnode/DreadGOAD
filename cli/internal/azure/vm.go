package azure

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
)

// Instance represents a discovered Azure VM relevant to dreadgoad.
type Instance struct {
	ID            string // full Azure resource ID (used for run-command targeting)
	Name          string // VM resource name (e.g. "test-goad-dreadgoad-kingslanding-vm")
	ResourceGroup string
	PrivateIP     string
	State         string            // "running", "stopped", etc. (normalized from PowerState/* )
	Tags          map[string]string // Azure resource tags (Role, Lab, Project, Environment, …)
}

// DiscoverInstances finds GOAD VMs for the given env (running by default).
// Mirrors the AWS provider's tag-based discovery: lab is identified by
// Project=DreadGOAD + Environment=<env> tags applied by Terraform.
//
// Implementation: ListAll across the subscription, client-side filter by tags
// (the Compute API has no server-side tag filter), then per-VM fan-out for
// the InstanceView (power state) and the first NIC's private IP. The fan-out
// is bounded but small in practice — a typical lab has ~5 VMs.
func (c *Client) DiscoverInstances(ctx context.Context, env string, includeStopped bool) ([]Instance, error) {
	if err := c.ensureSDK(ctx); err != nil {
		return nil, err
	}

	var matched []*armcompute.VirtualMachine
	pager := c.vmClient.NewListAllPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list VMs: %w", err)
		}
		for _, vm := range page.Value {
			if matchesEnvTags(vm.Tags, env) {
				matched = append(matched, vm)
			}
		}
	}

	if len(matched) == 0 {
		return nil, nil
	}

	// Per-VM enrichment: InstanceView for power state, NIC for private IP.
	type result struct {
		idx      int
		instance Instance
		err      error
	}
	results := make(chan result, len(matched))
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	for i, vm := range matched {
		wg.Add(1)
		go func(i int, vm *armcompute.VirtualMachine) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			inst, err := c.enrichInstance(ctx, vm)
			results <- result{idx: i, instance: inst, err: err}
		}(i, vm)
	}
	wg.Wait()
	close(results)

	enriched := make([]Instance, len(matched))
	for r := range results {
		if r.err != nil {
			return nil, r.err
		}
		enriched[r.idx] = r.instance
	}

	out := enriched[:0]
	for _, inst := range enriched {
		if !includeStopped && inst.State != "running" {
			continue
		}
		out = append(out, inst)
	}
	return out, nil
}

// enrichInstance fetches the per-VM data missing from ListAll (InstanceView
// for power state, first NIC's private IP) and assembles an Instance.
func (c *Client) enrichInstance(ctx context.Context, vm *armcompute.VirtualMachine) (Instance, error) {
	id := derefStr(vm.ID)
	name := derefStr(vm.Name)

	rid, err := arm.ParseResourceID(id)
	if err != nil {
		return Instance{}, fmt.Errorf("parse VM resource ID %q: %w", id, err)
	}

	ivResp, err := c.vmClient.InstanceView(ctx, rid.ResourceGroupName, name, nil)
	if err != nil {
		return Instance{}, fmt.Errorf("get instance view for %s: %w", name, err)
	}

	state := "unknown"
	for _, s := range ivResp.Statuses {
		if s == nil || s.Code == nil {
			continue
		}
		if strings.HasPrefix(*s.Code, "PowerState/") {
			state = normalizePowerState(strings.TrimPrefix(*s.Code, "PowerState/"))
			break
		}
	}

	privateIP := ""
	if vm.Properties != nil && vm.Properties.NetworkProfile != nil {
		for _, nicRef := range vm.Properties.NetworkProfile.NetworkInterfaces {
			if nicRef == nil || nicRef.ID == nil {
				continue
			}
			ip, err := c.firstNICPrivateIP(ctx, *nicRef.ID)
			if err != nil {
				return Instance{}, err
			}
			if ip != "" {
				privateIP = ip
				break
			}
		}
	}

	return Instance{
		ID:            id,
		Name:          name,
		ResourceGroup: rid.ResourceGroupName,
		PrivateIP:     privateIP,
		State:         state,
		Tags:          stringMap(vm.Tags),
	}, nil
}

// firstNICPrivateIP fetches a NIC by its ARM resource ID and returns the
// first IPConfiguration's private IPv4 address. Empty string if none found.
func (c *Client) firstNICPrivateIP(ctx context.Context, nicID string) (string, error) {
	rid, err := arm.ParseResourceID(nicID)
	if err != nil {
		return "", fmt.Errorf("parse NIC resource ID %q: %w", nicID, err)
	}
	resp, err := c.nicClient.Get(ctx, rid.ResourceGroupName, rid.Name, nil)
	if err != nil {
		return "", fmt.Errorf("get NIC %s: %w", rid.Name, err)
	}
	if resp.Properties == nil {
		return "", nil
	}
	for _, cfg := range resp.Properties.IPConfigurations {
		if cfg == nil || cfg.Properties == nil || cfg.Properties.PrivateIPAddress == nil {
			continue
		}
		if ip := *cfg.Properties.PrivateIPAddress; ip != "" {
			return ip, nil
		}
	}
	return "", nil
}

// FindInstanceByHostname locates a VM whose name contains the hostname.
func (c *Client) FindInstanceByHostname(ctx context.Context, env, hostname string) (*Instance, error) {
	instances, err := c.DiscoverInstances(ctx, env, true)
	if err != nil {
		return nil, err
	}
	wanted := strings.ToUpper(hostname)
	for _, inst := range instances {
		if strings.Contains(strings.ToUpper(inst.Name), wanted) {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("instance not found for hostname %s", hostname)
}

// StartInstances starts the given VMs (by full resource IDs). Operations run
// in parallel and the call blocks until all have completed (matching the
// default `az vm start` semantics).
func (c *Client) StartInstances(ctx context.Context, ids []string) error {
	return c.runVMLifecycle(ctx, ids, "start", func(ctx context.Context, rg, name string) (lifecyclePoller, error) {
		p, err := c.vmClient.BeginStart(ctx, rg, name, nil)
		if err != nil {
			return nil, err
		}
		return pollerAdapter[armcompute.VirtualMachinesClientStartResponse]{p: p}, nil
	})
}

// StopInstances deallocates the given VMs (stops billing for compute).
func (c *Client) StopInstances(ctx context.Context, ids []string) error {
	return c.runVMLifecycle(ctx, ids, "deallocate", func(ctx context.Context, rg, name string) (lifecyclePoller, error) {
		p, err := c.vmClient.BeginDeallocate(ctx, rg, name, nil)
		if err != nil {
			return nil, err
		}
		return pollerAdapter[armcompute.VirtualMachinesClientDeallocateResponse]{p: p}, nil
	})
}

// WaitForInstanceStopped polls until the VM reaches a deallocated state.
func (c *Client) WaitForInstanceStopped(ctx context.Context, id string) error {
	if err := c.ensureSDK(ctx); err != nil {
		return err
	}
	rid, err := arm.ParseResourceID(id)
	if err != nil {
		return fmt.Errorf("parse VM resource ID %q: %w", id, err)
	}

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
		resp, err := c.vmClient.InstanceView(ctx, rid.ResourceGroupName, rid.Name, nil)
		if err != nil {
			return fmt.Errorf("instance view for %s: %w", rid.Name, err)
		}
		for _, s := range resp.Statuses {
			if s == nil || s.Code == nil {
				continue
			}
			if *s.Code == "PowerState/deallocated" || *s.Code == "PowerState/stopped" {
				return nil
			}
		}
	}
	return fmt.Errorf("timed out waiting for VM %s to stop", id)
}

// DestroyInstances deletes the given VMs (does not delete NICs/disks/RG).
// For full cleanup, prefer terragrunt destroy.
func (c *Client) DestroyInstances(ctx context.Context, ids []string) error {
	return c.runVMLifecycle(ctx, ids, "delete", func(ctx context.Context, rg, name string) (lifecyclePoller, error) {
		p, err := c.vmClient.BeginDelete(ctx, rg, name, nil)
		if err != nil {
			return nil, err
		}
		return pollerAdapter[armcompute.VirtualMachinesClientDeleteResponse]{p: p}, nil
	})
}

// lifecyclePoller is the common subset of armcompute LRO pollers used by
// Start/Deallocate/Delete. Each Begin* returns a typed poller; we only need
// PollUntilDone so a tiny interface is enough to share runVMLifecycle.
type lifecyclePoller interface {
	PollUntilDone(ctx context.Context, options *runtime.PollUntilDoneOptions) (any, error)
}

// pollerAdapter wraps the SDK's typed pollers behind lifecyclePoller so the
// fan-out helper can drive them uniformly. The SDK's typed PollUntilDone
// returns a typed response we don't inspect, so we discard it.
type pollerAdapter[T any] struct {
	p *runtime.Poller[T]
}

func (a pollerAdapter[T]) PollUntilDone(ctx context.Context, opts *runtime.PollUntilDoneOptions) (any, error) {
	return a.p.PollUntilDone(ctx, opts)
}

// runVMLifecycle fans out a Begin* over the given resource IDs and waits for
// every poller to finish. Errors are aggregated; partial success is reported
// alongside the failures.
func (c *Client) runVMLifecycle(ctx context.Context, ids []string, op string, begin func(context.Context, string, string) (lifecyclePoller, error)) error {
	if len(ids) == 0 {
		return nil
	}
	if err := c.ensureSDK(ctx); err != nil {
		return err
	}

	type opErr struct {
		id  string
		err error
	}
	errs := make(chan opErr, len(ids))
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			rid, err := arm.ParseResourceID(id)
			if err != nil {
				errs <- opErr{id, fmt.Errorf("parse VM resource ID %q: %w", id, err)}
				return
			}
			poller, err := begin(ctx, rid.ResourceGroupName, rid.Name)
			if err != nil {
				errs <- opErr{id, fmt.Errorf("vm %s %s: %w", op, rid.Name, err)}
				return
			}
			if _, err := poller.PollUntilDone(ctx, nil); err != nil {
				errs <- opErr{id, fmt.Errorf("vm %s poll %s: %w", op, rid.Name, err)}
				return
			}
		}(id)
	}
	wg.Wait()
	close(errs)

	var combined []string
	for e := range errs {
		combined = append(combined, e.err.Error())
	}
	if len(combined) > 0 {
		return fmt.Errorf("%s: %s", op, strings.Join(combined, "; "))
	}
	return nil
}

// matchesEnvTags returns true when the VM tags match Project=DreadGOAD and
// Environment=<env>. Azure stores tag values as *string; missing or nil
// values do not match.
func matchesEnvTags(tags map[string]*string, env string) bool {
	proj, hasProj := tags["Project"]
	envTag, hasEnv := tags["Environment"]
	if !hasProj || !hasEnv || proj == nil || envTag == nil {
		return false
	}
	return *proj == "DreadGOAD" && *envTag == env
}

// stringMap dereferences a map[string]*string into map[string]string,
// dropping nil values. Output is never nil so callers can range freely.
func stringMap(in map[string]*string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if v != nil {
			out[k] = *v
		}
	}
	return out
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// normalizePowerState maps Azure's "running"/"deallocated" status suffixes
// to the dreadgoad-canonical lowercase verbs the rest of the CLI uses.
func normalizePowerState(s string) string {
	s = strings.TrimPrefix(s, "VM ")
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "running":
		return "running"
	case "deallocated":
		return "stopped"
	case "stopped":
		return "stopped"
	default:
		return s
	}
}
