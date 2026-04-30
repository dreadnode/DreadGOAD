package azure

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
)

// CommandResult holds the output of a Run Command invocation.
type CommandResult struct {
	Status string
	Stdout string
	Stderr string
}

// runCommandPollFreq overrides the SDK's 30s default poll cadence on
// long-running operations. With trivial probes the script itself often
// finishes in 1–3 seconds; polling at 30s would dominate wall-clock time
// and erase the win of swapping off the `az` CLI.
const runCommandPollFreq = time.Second

// RunPowerShellCommand executes a PowerShell script on a VM via Azure Managed
// Run Commands using the armcompute SDK.
//
// Flow per call:
//  1. Acquire per-VM and global ARM semaphores. The per-VM slot is held
//     across the entire create→execute→delete-complete lifecycle so that
//     existing run-command subresources per VM never exceed the cap (Azure
//     enforces a hard limit of 25 per VM).
//  2. BeginCreateOrUpdate (synchronous: AsyncExecution=false, so the LRO
//     completes only after the script has actually run on the VM).
//  3. PollUntilDone with 1s frequency (vs the SDK's 30s default).
//  4. GetByVirtualMachine with Expand=instanceView to retrieve script output.
//  5. Detached cleanup goroutine issues BeginDelete and waits for the LRO
//     to complete before releasing the per-VM slot. Caller returns as soon
//     as output is read; downstream callers for the same VM block on the
//     slot until cleanup finishes.
//
// Managed Run Commands are independent subresources (one per call, random
// name), so concurrent invocations against the same VM execute in parallel
// without 409 conflicts. The validator can fan out 16 checks even when
// several target the same VM.
func (c *Client) RunPowerShellCommand(ctx context.Context, vmID, script string, timeout time.Duration) (*CommandResult, error) {
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	if err := c.ensureSDK(ctx); err != nil {
		return nil, err
	}
	rg, vmName, err := parseVMResourceID(vmID)
	if err != nil {
		return nil, err
	}

	name := "dreadgoad-" + randHex(8)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	perVM, releaseGlobal, err := c.acquireRunCommandSlots(runCtx, vmID)
	if err != nil {
		return nil, err
	}
	defer releaseGlobal()
	// perVM is released by scheduleRunCommandDelete after BeginDelete LRO
	// completes. On early-error paths below we release it ourselves.

	payload := armcompute.VirtualMachineRunCommand{
		Location: to.Ptr(c.Region),
		Properties: &armcompute.VirtualMachineRunCommandProperties{
			AsyncExecution:   to.Ptr(false),
			TimeoutInSeconds: to.Ptr(int32(timeout / time.Second)),
			Source: &armcompute.VirtualMachineRunCommandScriptSource{
				Script: to.Ptr(script),
			},
		},
	}

	poller, err := c.rcClient.BeginCreateOrUpdate(runCtx, rg, vmName, name, payload, nil)
	if err != nil {
		// PUT can fail after the resource has been partially created on
		// ARM's side, so always schedule cleanup. The detached goroutine
		// also releases the per-VM slot once delete completes.
		c.scheduleRunCommandDelete(name, vmName, rg, perVM)
		return nil, fmt.Errorf("run-command create: %w", err)
	}
	if _, err := poller.PollUntilDone(runCtx, &runtime.PollUntilDoneOptions{Frequency: runCommandPollFreq}); err != nil {
		c.scheduleRunCommandDelete(name, vmName, rg, perVM)
		return nil, fmt.Errorf("run-command poll: %w", err)
	}
	defer c.scheduleRunCommandDelete(name, vmName, rg, perVM)

	got, err := c.rcClient.GetByVirtualMachine(runCtx, rg, vmName, name,
		&armcompute.VirtualMachineRunCommandsClientGetByVirtualMachineOptions{
			Expand: to.Ptr("instanceView"),
		})
	if err != nil {
		return nil, fmt.Errorf("run-command get: %w", err)
	}
	if got.Properties == nil || got.Properties.InstanceView == nil {
		return nil, fmt.Errorf("run-command response missing instance view")
	}
	return resultFromInstanceView(got.Properties.InstanceView), nil
}

// acquireRunCommandSlots takes the per-VM slot then the global ARM slot.
// Returns the per-VM channel (so callers can hand it to scheduleRunCommandDelete)
// and a release func for the global slot. The per-VM slot must be released
// elsewhere — either via scheduleRunCommandDelete or directly on early errors.
func (c *Client) acquireRunCommandSlots(ctx context.Context, vmID string) (chan struct{}, func(), error) {
	perVM := c.perVMSem(vmID)
	select {
	case perVM <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}

	// Bound concurrent ARM requests across all VMs. The global cap protects
	// against bursting past the subscription's per-second write throttle;
	// the SDK's retry policy handles transient 429s.
	select {
	case c.rcSem <- struct{}{}:
		return perVM, func() { <-c.rcSem }, nil
	case <-ctx.Done():
		<-perVM
		return nil, nil, ctx.Err()
	}
}

// resultFromInstanceView shapes the RunCommand instance view into a
// CommandResult, mapping ExecutionState to Success/Failed.
func resultFromInstanceView(iv *armcompute.VirtualMachineRunCommandInstanceView) *CommandResult {
	result := &CommandResult{
		Stdout: derefStr(iv.Output),
		Stderr: derefStr(iv.Error),
	}
	state := ""
	if iv.ExecutionState != nil {
		state = string(*iv.ExecutionState)
	}
	if state == string(armcompute.ExecutionStateSucceeded) {
		result.Status = "Success"
		return result
	}
	result.Status = "Failed"
	if result.Stderr == "" {
		result.Stderr = fmt.Sprintf("execution state: %s", state)
	}
	return result
}

// RunPowerShellOnMultiple runs the same script across multiple VMs in parallel.
// Managed Run Commands have no per-VM single-flight constraint, so callers can
// fan out freely.
func (c *Client) RunPowerShellOnMultiple(ctx context.Context, vmIDs []string, script string, timeout time.Duration) (map[string]*CommandResult, error) {
	results := make(map[string]*CommandResult, len(vmIDs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, id := range vmIDs {
		wg.Add(1)
		go func(vmID string) {
			defer wg.Done()
			res, err := c.RunPowerShellCommand(ctx, vmID, script, timeout)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				results[vmID] = &CommandResult{Status: "Error", Stderr: err.Error()}
			} else {
				results[vmID] = res
			}
		}(id)
	}
	wg.Wait()
	return results, nil
}

// scheduleRunCommandDelete releases the per-VM active slot immediately, then
// fires a detached goroutine that issues BeginDelete and waits for the LRO
// to complete. The LRO wait is for cleanupWG accounting (so Drain can block
// the process from exiting mid-delete), not for slot accounting — by the
// time we get here, the active phase is already done and the next caller
// can start its PUT.
//
// Total existing subresources per VM in steady state stays under Azure's
// 25-per-VM limit because perVMActiveConcurrency is small enough that
// (active + tail-deleting) fits — see the constant's docstring for the math.
//
// On any error the goroutine still drains so cleanupWG hits zero. The slot
// has already been released regardless.
func (c *Client) scheduleRunCommandDelete(name, vmName, rg string, perVM chan struct{}) {
	<-perVM
	if c.rcClient == nil {
		return
	}
	c.cleanupWG.Add(1)
	go func() {
		defer c.cleanupWG.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		poller, err := c.rcClient.BeginDelete(ctx, rg, vmName, name, nil)
		if err != nil {
			slog.Debug("run-command cleanup delete failed",
				"vm", vmName, "rg", rg, "name", name, "error", err)
			return
		}
		if _, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: runCommandPollFreq}); err != nil {
			slog.Debug("run-command cleanup delete poll failed",
				"vm", vmName, "rg", rg, "name", name, "error", err)
		}
	}()
}

// parseVMResourceID extracts (resourceGroup, vmName) from a full ARM resource
// ID of the form
// /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/virtualMachines/{vm}.
func parseVMResourceID(id string) (string, string, error) {
	parts := strings.Split(strings.TrimPrefix(id, "/"), "/")
	rgIdx, vmIdx := -1, -1
	for i, p := range parts {
		switch strings.ToLower(p) {
		case "resourcegroups":
			rgIdx = i + 1
		case "virtualmachines":
			vmIdx = i + 1
		}
	}
	if rgIdx < 0 || rgIdx >= len(parts) || vmIdx < 0 || vmIdx >= len(parts) {
		return "", "", fmt.Errorf("invalid VM resource ID: %q", id)
	}
	return parts[rgIdx], parts[vmIdx], nil
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
