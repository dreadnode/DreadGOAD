package azure

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// vmActionMatcher matches POST/DELETE requests for VM lifecycle actions.
// Path examples:
//
//	POST .../virtualMachines/<vm>/start
//	POST .../virtualMachines/<vm>/deallocate
//	DELETE .../virtualMachines/<vm>
func vmActionMatcher(method, suffix string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		if r.Method != method {
			return false
		}
		if suffix == "" {
			return strings.Contains(r.URL.Path, "/virtualMachines/") &&
				!strings.HasSuffix(r.URL.Path, "/start") &&
				!strings.HasSuffix(r.URL.Path, "/deallocate") &&
				!strings.HasSuffix(r.URL.Path, "/instanceView")
		}
		return strings.HasSuffix(r.URL.Path, suffix)
	}
}

// terminalAccepted returns a 200 OK with empty JSON body — this is the
// LRO terminal-success response for an action that completed synchronously.
// The SDK's poller framework treats it as immediately done.
func terminalAccepted() *http.Response {
	return jsonResponse(200, `{}`)
}

func TestStartInstances_ParallelSuccess(t *testing.T) {
	var calls atomic.Int32
	transport := &fakeTransport{
		t: t,
		routes: []fakeRoute{
			{
				matches: vmActionMatcher(http.MethodPost, "/start"),
				respond: func(*http.Request) *http.Response {
					calls.Add(1)
					return terminalAccepted()
				},
			},
		},
	}
	c := newTestClient(t, transport)
	ids := []string{
		"/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm-a",
		"/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm-b",
	}
	if err := c.StartInstances(context.Background(), ids); err != nil {
		t.Fatalf("StartInstances: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 start POSTs, got %d", calls.Load())
	}
	if transport.unknown.Load() != 0 {
		t.Errorf("transport saw %d unrouted requests", transport.unknown.Load())
	}
}

func TestStopInstances_ParallelSuccess(t *testing.T) {
	var calls atomic.Int32
	transport := &fakeTransport{
		t: t,
		routes: []fakeRoute{
			{
				matches: vmActionMatcher(http.MethodPost, "/deallocate"),
				respond: func(*http.Request) *http.Response {
					calls.Add(1)
					return terminalAccepted()
				},
			},
		},
	}
	c := newTestClient(t, transport)
	ids := []string{
		"/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm-a",
	}
	if err := c.StopInstances(context.Background(), ids); err != nil {
		t.Fatalf("StopInstances: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 deallocate POST, got %d", calls.Load())
	}
}

func TestDestroyInstances_AggregatesErrors(t *testing.T) {
	transport := &fakeTransport{
		t: t,
		routes: []fakeRoute{
			{
				matches: func(r *http.Request) bool {
					return r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/virtualMachines/vm-a")
				},
				respond: func(*http.Request) *http.Response { return terminalAccepted() },
			},
			{
				matches: func(r *http.Request) bool {
					return r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/virtualMachines/vm-b")
				},
				respond: func(*http.Request) *http.Response {
					return jsonResponse(403, `{"error":{"code":"AuthorizationFailed","message":"no perms"}}`)
				},
			},
		},
	}
	c := newTestClient(t, transport)
	ids := []string{
		"/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm-a",
		"/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm-b",
	}
	err := c.DestroyInstances(context.Background(), ids)
	if err == nil {
		t.Fatal("expected error from vm-b 403")
	}
	if !strings.Contains(err.Error(), "vm-b") {
		t.Errorf("error should name failing VM, got: %v", err)
	}
	if strings.Contains(err.Error(), "vm-a") {
		t.Errorf("error should not name successful VM, got: %v", err)
	}
}

func TestStartInstances_EmptyIsNoOp(t *testing.T) {
	transport := &fakeTransport{t: t}
	c := newTestClient(t, transport)
	if err := c.StartInstances(context.Background(), nil); err != nil {
		t.Fatalf("StartInstances(nil): %v", err)
	}
	if transport.calls.Load() != 0 {
		t.Errorf("expected 0 HTTP calls, got %d", transport.calls.Load())
	}
}
