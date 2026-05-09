package azure

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v5"
)

// fakeTransport routes HTTP requests by URL path so individual tests can
// stub specific ARM endpoints. It is the test seam recommended by the
// Azure SDK for Go integration testing guide; method-level interface fakes
// would skip over real query-parameter / payload assertions.
type fakeTransport struct {
	t       *testing.T
	routes  []fakeRoute
	calls   atomic.Int32
	unknown atomic.Int32
}

type fakeRoute struct {
	matches func(*http.Request) bool
	respond func(*http.Request) *http.Response
}

func (f *fakeTransport) Do(req *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	for _, r := range f.routes {
		if r.matches(req) {
			resp := r.respond(req)
			if resp.Request == nil {
				resp.Request = req
			}
			return resp, nil
		}
	}
	f.unknown.Add(1)
	f.t.Errorf("fakeTransport: no route for %s %s", req.Method, req.URL.Path)
	resp := jsonResponse(404, `{"error":{"code":"NotFound","message":"no route"}}`)
	resp.Request = req
	return resp, nil
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func pathContains(substr string) func(*http.Request) bool {
	return func(req *http.Request) bool { return strings.Contains(req.URL.Path, substr) }
}

func pathHasSuffix(suffix string) func(*http.Request) bool {
	return func(req *http.Request) bool { return strings.HasSuffix(req.URL.Path, suffix) }
}

// noOpCred returns a static access token. The fake transport intercepts
// requests before any auth actually matters, so we just need something that
// satisfies azcore.TokenCredential without panicking.
type noOpCred struct{}

func (noOpCred) GetToken(_ context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "fake", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

// newTestClient builds a Client wired to the given fake transport, with
// SDK clients eagerly constructed and sdkOnce tripped so ensureSDK is a
// no-op. Tests can call DiscoverInstances/WaitForInstanceStopped directly.
func newTestClient(t *testing.T, transport *fakeTransport) *Client {
	t.Helper()
	cred := noOpCred{}
	opts := &arm.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Transport: transport,
			// Disable the default retry policy so a test that returns 5xx
			// doesn't loop. Tests that want to exercise retry can override.
			Retry: policy.RetryOptions{MaxRetries: -1},
		},
	}
	vm, err := armcompute.NewVirtualMachinesClient("sub-1", cred, opts)
	if err != nil {
		t.Fatalf("compute client: %v", err)
	}
	nic, err := armnetwork.NewInterfacesClient("sub-1", cred, opts)
	if err != nil {
		t.Fatalf("network client: %v", err)
	}
	c := &Client{
		Region:         "centralus",
		SubscriptionID: "sub-1",
		cred:           cred,
		armOpts:        opts,
		vmClient:       vm,
		nicClient:      nic,
		rcSem:          make(chan struct{}, 16),
	}
	// Trip sdkOnce so any code that calls ensureSDK skips re-init.
	c.sdkOnce.Do(func() {})
	return c
}

func TestMatchesEnvTags(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []struct {
		name string
		tags map[string]*string
		env  string
		want bool
	}{
		{"match", map[string]*string{"Project": s("DreadGOAD"), "Environment": s("test")}, "test", true},
		{"wrong project", map[string]*string{"Project": s("Other"), "Environment": s("test")}, "test", false},
		{"wrong env", map[string]*string{"Project": s("DreadGOAD"), "Environment": s("prod")}, "test", false},
		{"missing project", map[string]*string{"Environment": s("test")}, "test", false},
		{"missing env", map[string]*string{"Project": s("DreadGOAD")}, "test", false},
		{"nil project value", map[string]*string{"Project": nil, "Environment": s("test")}, "test", false},
		{"empty tags", map[string]*string{}, "test", false},
		{"nil tags", nil, "test", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesEnvTags(tc.tags, tc.env); got != tc.want {
				t.Fatalf("matchesEnvTags(%v, %q) = %v, want %v", tc.tags, tc.env, got, tc.want)
			}
		})
	}
}

func TestNormalizePowerState(t *testing.T) {
	cases := map[string]string{
		"running":        "running",
		"deallocated":    "stopped",
		"stopped":        "stopped",
		"VM running":     "running",
		"VM deallocated": "stopped",
		"  Running  ":    "running",
		"starting":       "starting",
	}
	for in, want := range cases {
		if got := normalizePowerState(in); got != want {
			t.Errorf("normalizePowerState(%q) = %q, want %q", in, got, want)
		}
	}
}

// vmListPayload, instanceViewPayload, nicPayload are sample ARM responses.
// Hand-written rather than generated to keep test failures readable.
const vmListPayload = `{
  "value": [
    {
      "id": "/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/dreadgoad-vm",
      "name": "dreadgoad-vm",
      "location": "centralus",
      "tags": {"Project": "DreadGOAD", "Environment": "test", "Role": "DC01"},
      "properties": {
        "networkProfile": {
          "networkInterfaces": [
            {"id": "/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/dreadgoad-nic"}
          ]
        }
      }
    },
    {
      "id": "/subscriptions/sub-1/resourceGroups/rg2/providers/Microsoft.Compute/virtualMachines/other-vm",
      "name": "other-vm",
      "location": "centralus",
      "tags": {"Project": "OtherProject", "Environment": "test"},
      "properties": {"networkProfile": {"networkInterfaces": []}}
    },
    {
      "id": "/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/wrong-env-vm",
      "name": "wrong-env-vm",
      "location": "centralus",
      "tags": {"Project": "DreadGOAD", "Environment": "prod"},
      "properties": {"networkProfile": {"networkInterfaces": []}}
    }
  ]
}`

const runningInstanceView = `{
  "statuses": [
    {"code": "ProvisioningState/succeeded", "level": "Info"},
    {"code": "PowerState/running", "level": "Info", "displayStatus": "VM running"}
  ]
}`

const stoppedInstanceView = `{
  "statuses": [
    {"code": "PowerState/deallocated", "level": "Info", "displayStatus": "VM deallocated"}
  ]
}`

const nicPayload = `{
  "id": "/subscriptions/sub-1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/dreadgoad-nic",
  "name": "dreadgoad-nic",
  "location": "centralus",
  "properties": {
    "ipConfigurations": [
      {"name": "ipconfig1", "properties": {"privateIPAddress": "10.0.0.5", "primary": true}}
    ]
  }
}`

func TestDiscoverInstances_FiltersByTags(t *testing.T) {
	transport := &fakeTransport{
		t: t,
		routes: []fakeRoute{
			{
				matches: pathHasSuffix("/providers/Microsoft.Compute/virtualMachines"),
				respond: func(*http.Request) *http.Response { return jsonResponse(200, vmListPayload) },
			},
			{
				matches: pathHasSuffix("/dreadgoad-vm/instanceView"),
				respond: func(*http.Request) *http.Response { return jsonResponse(200, runningInstanceView) },
			},
			{
				matches: pathContains("/networkInterfaces/dreadgoad-nic"),
				respond: func(*http.Request) *http.Response { return jsonResponse(200, nicPayload) },
			},
		},
	}
	c := newTestClient(t, transport)

	got, err := c.DiscoverInstances(context.Background(), "test", false)
	if err != nil {
		t.Fatalf("DiscoverInstances: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 matching VM, got %d (%+v)", len(got), got)
	}
	inst := got[0]
	if inst.Name != "dreadgoad-vm" {
		t.Errorf("Name = %q, want dreadgoad-vm", inst.Name)
	}
	if inst.ResourceGroup != "rg1" {
		t.Errorf("ResourceGroup = %q, want rg1", inst.ResourceGroup)
	}
	if inst.PrivateIP != "10.0.0.5" {
		t.Errorf("PrivateIP = %q, want 10.0.0.5", inst.PrivateIP)
	}
	if inst.State != "running" {
		t.Errorf("State = %q, want running", inst.State)
	}
	if inst.Tags["Role"] != "DC01" {
		t.Errorf("Tags[Role] = %q, want DC01", inst.Tags["Role"])
	}
	if transport.unknown.Load() != 0 {
		t.Errorf("transport saw %d unrouted requests", transport.unknown.Load())
	}
}

func TestDiscoverInstances_ExcludeStoppedByDefault(t *testing.T) {
	transport := &fakeTransport{
		t: t,
		routes: []fakeRoute{
			{
				matches: pathHasSuffix("/providers/Microsoft.Compute/virtualMachines"),
				respond: func(*http.Request) *http.Response { return jsonResponse(200, vmListPayload) },
			},
			{
				matches: pathHasSuffix("/dreadgoad-vm/instanceView"),
				respond: func(*http.Request) *http.Response { return jsonResponse(200, stoppedInstanceView) },
			},
			{
				matches: pathContains("/networkInterfaces/"),
				respond: func(*http.Request) *http.Response { return jsonResponse(200, nicPayload) },
			},
		},
	}
	c := newTestClient(t, transport)

	got, err := c.DiscoverInstances(context.Background(), "test", false)
	if err != nil {
		t.Fatalf("DiscoverInstances: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 running VMs (stopped should be excluded), got %d", len(got))
	}

	got, err = c.DiscoverInstances(context.Background(), "test", true)
	if err != nil {
		t.Fatalf("DiscoverInstances includeStopped=true: %v", err)
	}
	if len(got) != 1 || got[0].State != "stopped" {
		t.Fatalf("expected 1 stopped VM with includeStopped=true, got %+v", got)
	}
}
