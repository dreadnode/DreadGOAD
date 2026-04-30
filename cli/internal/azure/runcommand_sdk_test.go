package azure

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
)

// rcTestClient extends newTestClient by also wiring the run-command SDK
// client through the same fake transport.
func rcTestClient(t *testing.T, transport *fakeTransport) *Client {
	t.Helper()
	c := newTestClient(t, transport)
	rc, err := armcompute.NewVirtualMachineRunCommandsClient(c.SubscriptionID, c.cred,
		&arm.ClientOptions{ClientOptions: azcore.ClientOptions{
			Transport: transport,
			Retry:     policy.RetryOptions{MaxRetries: -1},
		}})
	if err != nil {
		t.Fatalf("rc client: %v", err)
	}
	c.rcClient = rc
	return c
}

// successCreateBody is the PUT response shape that the body-pattern poller
// treats as terminal-success: 200 OK + provisioningState=Succeeded means
// PollUntilDone returns immediately without an extra GET roundtrip.
const rcCreateSuccessBody = `{
  "id": "/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/test-vm/runCommands/dreadgoad-x",
  "name": "dreadgoad-x",
  "location": "centralus",
  "properties": {
    "provisioningState": "Succeeded",
    "asyncExecution": false,
    "source": {"script": "Write-Host hi"}
  }
}`

const rcGetSucceededBody = `{
  "id": "/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/test-vm/runCommands/dreadgoad-x",
  "name": "dreadgoad-x",
  "properties": {
    "provisioningState": "Succeeded",
    "instanceView": {
      "executionState": "Succeeded",
      "exitCode": 0,
      "output": "hello from azure",
      "error": ""
    }
  }
}`

const rcGetFailedBody = `{
  "id": "/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/test-vm/runCommands/dreadgoad-x",
  "name": "dreadgoad-x",
  "properties": {
    "provisioningState": "Succeeded",
    "instanceView": {
      "executionState": "Failed",
      "exitCode": 1,
      "output": "",
      "error": "boom"
    }
  }
}`

// rcCounters bundles the request counters used to assert RunCommand traffic.
type rcCounters struct {
	puts, gets, deletes atomic.Int32
}

// rcSuccessRoutes builds the route table for a successful PUT/GET/DELETE
// lifecycle. The cleanup-side GET (waitRunCommandGone) returns 404 to
// satisfy the body-pattern poller.
func rcSuccessRoutes(c *rcCounters) []fakeRoute {
	return []fakeRoute{
		{
			matches: func(r *http.Request) bool {
				return r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/runCommands/dreadgoad-")
			},
			respond: func(*http.Request) *http.Response {
				c.puts.Add(1)
				return jsonResponse(200, rcCreateSuccessBody)
			},
		},
		{
			matches: func(r *http.Request) bool {
				return r.Method == http.MethodGet &&
					strings.Contains(r.URL.Path, "/runCommands/dreadgoad-") &&
					r.URL.Query().Get("$expand") == "instanceView"
			},
			respond: func(*http.Request) *http.Response {
				c.gets.Add(1)
				return jsonResponse(200, rcGetSucceededBody)
			},
		},
		{
			matches: func(r *http.Request) bool {
				return r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runCommands/dreadgoad-")
			},
			respond: func(*http.Request) *http.Response {
				return jsonResponse(404, `{"error":{"code":"ResourceNotFound","message":"gone"}}`)
			},
		},
		{
			matches: func(r *http.Request) bool {
				return r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/runCommands/dreadgoad-")
			},
			respond: func(*http.Request) *http.Response {
				c.deletes.Add(1)
				return jsonResponse(200, "{}")
			},
		},
	}
}

func TestRunPowerShellCommand_Success(t *testing.T) {
	var counters rcCounters
	transport := &fakeTransport{t: t, routes: rcSuccessRoutes(&counters)}
	c := rcTestClient(t, transport)
	vmID := "/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/test-vm"

	res, err := c.RunPowerShellCommand(context.Background(), vmID, "Write-Host hi", 30*time.Second)
	if err != nil {
		t.Fatalf("RunPowerShellCommand: %v", err)
	}
	assertSuccessResult(t, res)
	assertCounter(t, "PUT", counters.puts.Load(), 1)
	assertCounter(t, "GET (instance view)", counters.gets.Load(), 1)
	waitForDelete(t, &counters.deletes)
	assertCounter(t, "DELETE (cleanup)", counters.deletes.Load(), 1)
	if transport.unknown.Load() != 0 {
		t.Errorf("transport saw %d unrouted requests", transport.unknown.Load())
	}
}

func assertSuccessResult(t *testing.T, res *CommandResult) {
	t.Helper()
	if res.Status != "Success" {
		t.Errorf("Status = %q, want Success", res.Status)
	}
	if res.Stdout != "hello from azure" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "hello from azure")
	}
	if res.Stderr != "" {
		t.Errorf("Stderr = %q, want empty", res.Stderr)
	}
}

func assertCounter(t *testing.T, label string, got, want int32) {
	t.Helper()
	if got != want {
		t.Errorf("expected %d %s, got %d", want, label, got)
	}
}

// waitForDelete polls briefly while the fire-and-forget cleanup goroutine
// issues its DELETE so tests can assert against deletes.Load() afterwards.
func waitForDelete(t *testing.T, deletes *atomic.Int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && deletes.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRunPowerShellCommand_FailedExecution(t *testing.T) {
	var deletes atomic.Int32
	transport := &fakeTransport{
		t: t,
		routes: []fakeRoute{
			{
				matches: func(r *http.Request) bool {
					return r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/runCommands/")
				},
				respond: func(*http.Request) *http.Response { return jsonResponse(200, rcCreateSuccessBody) },
			},
			{
				matches: func(r *http.Request) bool {
					return r.Method == http.MethodGet &&
						strings.Contains(r.URL.Path, "/runCommands/") &&
						r.URL.Query().Get("$expand") == "instanceView"
				},
				respond: func(*http.Request) *http.Response { return jsonResponse(200, rcGetFailedBody) },
			},
			{
				matches: func(r *http.Request) bool {
					return r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runCommands/")
				},
				respond: func(*http.Request) *http.Response {
					return jsonResponse(404, `{"error":{"code":"ResourceNotFound","message":"gone"}}`)
				},
			},
			{
				matches: func(r *http.Request) bool {
					return r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/runCommands/")
				},
				respond: func(*http.Request) *http.Response {
					deletes.Add(1)
					return jsonResponse(200, "{}")
				},
			},
		},
	}
	c := rcTestClient(t, transport)
	vmID := "/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/test-vm"

	res, err := c.RunPowerShellCommand(context.Background(), vmID, "throw", 30*time.Second)
	if err != nil {
		t.Fatalf("RunPowerShellCommand: %v", err)
	}
	if res.Status != "Failed" {
		t.Errorf("Status = %q, want Failed", res.Status)
	}
	if res.Stderr != "boom" {
		t.Errorf("Stderr = %q, want %q", res.Stderr, "boom")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && deletes.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if deletes.Load() != 1 {
		t.Errorf("expected 1 DELETE on Failed status, got %d", deletes.Load())
	}
}

func TestRunPowerShellCommand_CreateFailureStillCleansUp(t *testing.T) {
	var puts, deletes atomic.Int32
	transport := &fakeTransport{
		t: t,
		routes: []fakeRoute{
			{
				matches: func(r *http.Request) bool {
					return r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/runCommands/")
				},
				respond: func(*http.Request) *http.Response {
					puts.Add(1)
					return jsonResponse(400, `{"error":{"code":"BadRequest","message":"nope"}}`)
				},
			},
			{
				matches: func(r *http.Request) bool {
					return r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runCommands/")
				},
				respond: func(*http.Request) *http.Response {
					return jsonResponse(404, `{"error":{"code":"ResourceNotFound","message":"gone"}}`)
				},
			},
			{
				matches: func(r *http.Request) bool {
					return r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/runCommands/")
				},
				respond: func(*http.Request) *http.Response {
					deletes.Add(1)
					return jsonResponse(200, "{}")
				},
			},
		},
	}
	c := rcTestClient(t, transport)
	vmID := "/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/test-vm"

	_, err := c.RunPowerShellCommand(context.Background(), vmID, "x", 30*time.Second)
	if err == nil {
		t.Fatal("expected error on 400 BadRequest")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && deletes.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if deletes.Load() != 1 {
		t.Errorf("expected cleanup DELETE even when create fails, got %d", deletes.Load())
	}
}
