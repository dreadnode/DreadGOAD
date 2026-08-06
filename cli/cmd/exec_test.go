package cmd

import (
	"encoding/json"
	"strings"
	"testing"

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
