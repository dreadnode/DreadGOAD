package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/dreadnode/dreadgoad/internal/azure"
	"github.com/dreadnode/dreadgoad/internal/provider"
)

// Realistic Azure VM names — the console's agent says "dc02", the provider
// reports "dreadindex-dreadgoad-DC02-vm".
func execFixture() []provider.Instance {
	return []provider.Instance{
		{Name: "dreadindex-dreadgoad-DC01-vm", ID: "/subs/x/DC01"},
		{Name: "dreadindex-dreadgoad-DC02-vm", ID: "/subs/x/DC02"},
		{Name: "dreadindex-dreadgoad-DC03-vm", ID: "/subs/x/DC03"},
		{Name: "dreadindex-dreadgoad-SRV02-vm", ID: "/subs/x/SRV02"},
	}
}

func TestResolveExecTargetsSegmentMatch(t *testing.T) {
	got, err := resolveExecTargets(execFixture(), "dc02")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "/subs/x/DC02" {
		t.Fatalf("expected only DC02, got %+v", got)
	}
}

// The regression this verb exists to avoid: ssm/runcmd substring-match, so
// "dc0" selects dc01+dc02+dc03. For a mutating command that must be an error.
func TestResolveExecTargetsRejectsPartialToken(t *testing.T) {
	_, err := resolveExecTargets(execFixture(), "dc0")
	if err == nil {
		t.Fatal("expected 'dc0' to be rejected, not fan out to three DCs")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected a not-found error, got: %v", err)
	}

	// Contrast: the shared helper used by ssm/runcmd still fans out. If this
	// ever changes, the comment on resolveExecTargets needs revisiting.
	ids, _ := filterProviderInstances(execFixture(), "dc0")
	if len(ids) == 0 {
		t.Fatal("filterProviderInstances no longer substring-matches; update exec.go's rationale")
	}
}

func TestResolveExecTargetsExactNameWins(t *testing.T) {
	got, err := resolveExecTargets(execFixture(), "dreadindex-dreadgoad-SRV02-vm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "/subs/x/SRV02" {
		t.Fatalf("expected SRV02, got %+v", got)
	}
}

func TestResolveExecTargetsMultipleAndDedup(t *testing.T) {
	got, err := resolveExecTargets(execFixture(), "dc01, dc03 ,dc01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 deduped targets, got %d: %+v", len(got), got)
	}
}

func TestResolveExecTargetsRequiresHosts(t *testing.T) {
	for _, in := range []string{"", "   ", ",,"} {
		if _, err := resolveExecTargets(execFixture(), in); err == nil {
			t.Fatalf("expected %q to be rejected", in)
		}
	}
}

func TestResolveExecTargetsUnknownHostListsKnown(t *testing.T) {
	_, err := resolveExecTargets(execFixture(), "dc99")
	if err == nil {
		t.Fatal("expected unknown host to error")
	}
	// The message must name real hosts — an agent that guessed wrong needs to
	// see the actual inventory rather than guess again.
	if !strings.Contains(err.Error(), "DC01") {
		t.Fatalf("error should list known hosts, got: %v", err)
	}
}

// An ambiguous token must stop the run rather than pick one arbitrarily.
func TestResolveExecTargetsAmbiguousIsAnError(t *testing.T) {
	dupes := []provider.Instance{
		{Name: "env-a-web-vm", ID: "1"},
		{Name: "env-b-web-vm", ID: "2"},
	}
	_, err := resolveExecTargets(dupes, "web")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected an ambiguity error, got: %v", err)
	}
}

// fakeOOB records which channel served each call.
type fakeOOB struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]bool
}

func (f *fakeOOB) RunCommandOutOfBand(
	_ context.Context, id, _ string, _ time.Duration,
) (*provider.CommandResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, id)
	f.mu.Unlock()
	if f.fail[id] {
		return nil, errors.New("guest agent unreachable")
	}
	return &provider.CommandResult{Status: "Succeeded", Stdout: "ok"}, nil
}

func (f *fakeOOB) OutOfBandChannel() string { return "test channel" }

// The regression that shipped: exec called Provider.RunCommandOnMultiple, which
// on Azure goes over WinRM — the exact dependency the verb claims to avoid.
func TestRunOutOfBandOnAllUsesTheControlPlaneForEveryHost(t *testing.T) {
	f := &fakeOOB{}
	ids := []string{"/subs/x/DC01", "/subs/x/DC02", "/subs/x/DC03"}
	got := runOutOfBandOnAll(context.Background(), f, ids, "Get-Service", time.Minute)

	if len(got) != len(ids) {
		t.Fatalf("expected %d results, got %d", len(ids), len(got))
	}
	if len(f.calls) != len(ids) {
		t.Fatalf("expected one out-of-band call per host, got %d", len(f.calls))
	}
	for _, id := range ids {
		if got[id] == nil || got[id].Status != "Succeeded" {
			t.Fatalf("host %s: %+v", id, got[id])
		}
	}
}

// One broken host must not discard the others' output — with several hosts the
// healthy ones are the context that makes the broken one legible.
func TestRunOutOfBandOnAllIsolatesPerHostFailure(t *testing.T) {
	f := &fakeOOB{fail: map[string]bool{"/subs/x/DC02": true}}
	ids := []string{"/subs/x/DC01", "/subs/x/DC02"}
	got := runOutOfBandOnAll(context.Background(), f, ids, "x", time.Minute)

	if got["/subs/x/DC01"].Status != "Succeeded" {
		t.Fatalf("healthy host lost: %+v", got["/subs/x/DC01"])
	}
	bad := got["/subs/x/DC02"]
	if bad.Status != "Error" || !strings.Contains(bad.Stderr, "guest agent") {
		t.Fatalf("failure not reported on its own host: %+v", bad)
	}
}

// Both cloud providers must satisfy the interface, or exec refuses them at
// runtime with "no control-plane execution channel".
func TestCloudProvidersImplementOutOfBandRunner(t *testing.T) {
	var _ provider.OutOfBandRunner = (*azure.AzureProvider)(nil)
	var _ provider.OutOfBandRunner = (*aws.AWSProvider)(nil)
}

// The bug this pins: exec compared against "Succeeded" while every provider in
// the tree emits "Success", so a successful run was counted as a failure and
// exec exited non-zero. It stayed invisible because the host under test was
// broken on every attempt.
func TestIsCommandSuccessMatchesTheProviderVocabulary(t *testing.T) {
	for _, ok := range []string{"Success", "success", "SUCCESS", "Succeeded", "succeeded"} {
		if !isCommandSuccess(ok) {
			t.Fatalf("%q must count as success", ok)
		}
	}
	for _, bad := range []string{"Failed", "Error", "no result", "", "Succeededish"} {
		if isCommandSuccess(bad) {
			t.Fatalf("%q must NOT count as success", bad)
		}
	}
	// Guard the real coupling: azure's WinRM and Run Command paths both emit
	// this literal, so a rename there must break this test.
	if !isCommandSuccess("Success") {
		t.Fatal("azure winrm.go:265 and runcommand.go:150 both emit \"Success\"")
	}
}

// The JSON contract the console parses (console/backend/summary.py).
func TestExecResultJSONShape(t *testing.T) {
	b, err := json.Marshal([]execResult{{
		Host: "dreadindex-dreadgoad-DC02-vm", InstanceID: "/subs/x/DC02",
		Status: "Succeeded", Stdout: "Running", Stderr: "",
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"host"`, `"instance_id"`, `"status"`, `"stdout"`, `"stderr"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("missing %s in %s", key, b)
		}
	}
}
