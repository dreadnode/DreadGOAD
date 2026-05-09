// Package azure implements the dreadgoad provider interface for Microsoft Azure.
//
// Hot-path control-plane operations (VM discovery, lifecycle, Run Command) are
// being migrated to the Azure SDK for Go (azcore/azidentity/armcompute) to
// eliminate `az` CLI subprocess startup cost. A small set of cold-path or
// data-plane operations — Bastion ssh/rdp/tunnel and one bootstrap
// `az account show` for subscription discovery — remain on the `az` CLI
// because they have no clean SDK equivalent.
//
// Authentication piggybacks on the user's existing `az login` session via
// azidentity.DefaultAzureCredential, which chains AzureCLICredential among
// other sources. No new env vars are required for the laptop UX.
package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v5"
)

// runCommandConcurrency caps in-flight Managed Run Command requests against
// ARM. The previous cap (16) existed to bound `az` CLI Python process count;
// on the SDK path the only remaining concern is the subscription's
// per-second write throttle, which the SDK retry policy already absorbs.
// 64 lets the validator's 16-way fan-out run unthrottled while leaving
// headroom for deploy/destroy paths that also issue writes.
const runCommandConcurrency = 64

// perVMActiveConcurrency caps concurrent *active* (PUT + execute) Run
// Commands per VM. Two competing constraints:
//
//  1. Azure's hard 25-existing-per-VM limit (existing = active + pending-delete).
//  2. Each concurrent script spawns a worker process under the run-command
//     extension on the VM; running ≥5 in parallel against a DC saturated CPU
//     enough that individual call latency exceeded the validator's 180s
//     deadline, which marked the host dead for the rest of the run (validate-10
//     at cap=18 and validate-11 at cap=5 both hit this on DC02/DC03).
//
// Cap=2 keeps VM-side pressure low and bounds total existing well under 25:
//
//	steady_total ≈ cap_active + (cap_active / exec_time) * delete_LRO_time
//	            ≈ 2         + (2 / 10s)              * 30s         = 8
const perVMActiveConcurrency = 2

// Client wraps Azure control-plane operations against a specific
// subscription/region. Hot-path methods use the Azure SDK for Go; a few
// cold-path methods still shell out to the `az` CLI.
type Client struct {
	Region         string
	SubscriptionID string                 // populated by VerifyCredentials
	cred           azcore.TokenCredential // shared credential for SDK clients
	armOpts        *arm.ClientOptions     // optional; tests inject a fake transport here
	rcSem          chan struct{}          // bounds concurrent ARM run-command requests
	perVMSems      sync.Map               // vmID -> chan struct{}; bounds per-VM run-command resources
	cleanupWG      sync.WaitGroup         // tracks in-flight run-command DELETE goroutines

	// SDK clients are constructed lazily once SubscriptionID is known. Guarded
	// by sdkOnce so concurrent first-use doesn't double-build them.
	sdkOnce   sync.Once
	sdkErr    error
	vmClient  *armcompute.VirtualMachinesClient
	nicClient *armnetwork.InterfacesClient
	rcClient  *armcompute.VirtualMachineRunCommandsClient
}

// ensureSDK populates the lazy SDK clients. Callers must guarantee that
// SubscriptionID is set (typically by VerifyCredentials running first via
// requireInfra). For pure-SDK callers that bypass VerifyCredentials, this
// method calls it on demand to keep the public API forgiving.
func (c *Client) ensureSDK(ctx context.Context) error {
	c.sdkOnce.Do(func() {
		if c.SubscriptionID == "" {
			if _, err := c.VerifyCredentials(ctx); err != nil {
				c.sdkErr = err
				return
			}
		}
		vm, err := armcompute.NewVirtualMachinesClient(c.SubscriptionID, c.cred, c.armOpts)
		if err != nil {
			c.sdkErr = fmt.Errorf("init compute client: %w", err)
			return
		}
		nic, err := armnetwork.NewInterfacesClient(c.SubscriptionID, c.cred, c.armOpts)
		if err != nil {
			c.sdkErr = fmt.Errorf("init network client: %w", err)
			return
		}
		rc, err := armcompute.NewVirtualMachineRunCommandsClient(c.SubscriptionID, c.cred, c.armOpts)
		if err != nil {
			c.sdkErr = fmt.Errorf("init run-command client: %w", err)
			return
		}
		c.vmClient = vm
		c.nicClient = nic
		c.rcClient = rc
	})
	return c.sdkErr
}

// NewClient constructs a Client. Region is required. The credential chain is
// initialized eagerly via azidentity.DefaultAzureCredential, which picks up
// the user's `az login` session (among other sources). Subscription ID is
// resolved later by VerifyCredentials so we can surface a friendly error if
// the user hasn't run `az login`.
func NewClient(region string) (*Client, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("init Azure credential chain: %w", err)
	}
	return &Client{
		Region: region,
		cred:   cred,
		rcSem:  make(chan struct{}, runCommandConcurrency),
	}, nil
}

// Drain blocks until all in-flight run-command DELETE goroutines have
// finished. Callers should invoke this before process exit so the operating
// system doesn't kill goroutines mid-flight, which would leave the
// corresponding subresources in "Deleting" state and re-introduce the very
// orphan accumulation that perVMRunCommandConcurrency exists to prevent.
func (c *Client) Drain() {
	c.cleanupWG.Wait()
}

// perVMSem returns the per-VM active-phase semaphore for vmID, lazily
// constructing it on first use. The same chan is returned for the same vmID
// across calls so concurrent invocations against one VM share a slot pool.
func (c *Client) perVMSem(vmID string) chan struct{} {
	if existing, ok := c.perVMSems.Load(vmID); ok {
		return existing.(chan struct{})
	}
	fresh := make(chan struct{}, perVMActiveConcurrency)
	actual, _ := c.perVMSems.LoadOrStore(vmID, fresh)
	return actual.(chan struct{})
}

// run invokes `az` and returns stdout. Stderr is bundled into the returned
// error so callers can see CLI diagnostics without enabling verbose logging.
func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "az", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("az %s: %w (stderr: %s)",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// runJSON invokes `az` and unmarshals stdout into out.
func (c *Client) runJSON(ctx context.Context, out any, args ...string) error {
	data, err := c.run(ctx, args...)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode az output: %w", err)
	}
	return nil
}

// AccountInfo is the relevant slice of `az account show`.
type AccountInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"tenantId"`
	User     struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"user"`
}

// VerifyCredentials resolves and caches the subscription ID.
//
// Resolution order:
//  1. AZURE_SUBSCRIPTION_ID env var — preferred; skips spawning `az`. Returns
//     a minimal AccountInfo with only ID populated.
//  2. `az account show` — fallback that also yields tenant/user metadata for
//     a richer startup display when the user is interactively logged in.
//
// The credential chain itself (azidentity.DefaultAzureCredential) is set up
// in NewClient and is what the SDK actually uses for ARM calls; this method
// just decides which subscription to scope clients against.
func (c *Client) VerifyCredentials(ctx context.Context) (*AccountInfo, error) {
	if sub := strings.TrimSpace(os.Getenv("AZURE_SUBSCRIPTION_ID")); sub != "" {
		c.SubscriptionID = sub
		return &AccountInfo{ID: sub}, nil
	}
	var info AccountInfo
	if err := c.runJSON(ctx, &info, "account", "show", "-o", "json"); err != nil {
		return nil, fmt.Errorf("azure credentials invalid or not configured (set AZURE_SUBSCRIPTION_ID or run `az login`): %w", err)
	}
	c.SubscriptionID = info.ID
	return &info, nil
}
