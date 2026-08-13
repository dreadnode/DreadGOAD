package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

// recordingProvider implements only the lifecycle methods execVMAction uses.
// The embedded interface is nil, so any *other* call panics loudly rather than
// silently returning a zero value — if execVMAction grows a dependency, the
// test fails instead of quietly not covering it.
type recordingProvider struct {
	provider.Provider

	calls []string
	// Output written before each call. The fix is that the operator is told
	// what is happening *before* the multi-minute wait, so what matters is not
	// that a line is printed but that it is printed first.
	seenAt map[string]string
	// Path of the file standing in for stdout. Read synchronously at each call
	// — an os.Pipe with a reader goroutine races here, because fmt.Printf can
	// return before the reader has copied anything, making a correctly-ordered
	// print look absent.
	outPath string
}

func (p *recordingProvider) record(name string) {
	p.calls = append(p.calls, name)
	if p.seenAt == nil {
		p.seenAt = map[string]string{}
	}
	b, _ := os.ReadFile(p.outPath)
	p.seenAt[name] = string(b)
}

func (p *recordingProvider) StartInstances(_ context.Context, _ []string) error {
	p.record("start")
	return nil
}

func (p *recordingProvider) StopInstances(_ context.Context, _ []string) error {
	p.record("stop")
	return nil
}

func (p *recordingProvider) WaitForInstanceStopped(_ context.Context, _ string) error {
	p.record("wait")
	return nil
}

func (p *recordingProvider) DestroyInstances(_ context.Context, _ []string) error {
	p.record("destroy")
	return nil
}

// captureStdout points os.Stdout at a real file for the duration of fn and
// returns everything written. A file rather than a pipe so that what has been
// printed is observable mid-run, synchronously, from inside a provider call.
func captureStdout(t *testing.T, fn func(outPath string) error) (string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdout")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	orig := os.Stdout
	os.Stdout = f
	runErr := fn(path)
	os.Stdout = orig
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b), runErr
}

func TestRestartAnnouncesBeforeItWaits(t *testing.T) {
	// A restart blocks for minutes inside StopInstances/StartInstances, which
	// poll the Azure operation to completion. The prints used to come after,
	// so the command showed nothing at all for the whole wait.
	prov := &recordingProvider{}
	inst := &provider.Instance{Name: "DC01", ID: "/subscriptions/x/DC01", State: "running"}

	_, err := captureStdout(t, func(outPath string) error {
		prov.outPath = outPath
		return execVMAction(context.Background(), prov, inst, "restart", false)
	})
	if err != nil {
		t.Fatalf("execVMAction: %v", err)
	}

	if got := prov.seenAt["stop"]; !strings.Contains(got, "Stopping DC01") {
		t.Errorf("stop began with no announcement; stdout was %q", got)
	}
	if got := prov.seenAt["start"]; !strings.Contains(got, "Starting DC01") {
		t.Errorf("start began with no announcement; stdout was %q", got)
	}

	// The wait must stay, and must sit between the two. Whether StopInstances
	// blocks is per-provider — Azure polls the deallocate to completion, AWS
	// returns as soon as the EC2 call is accepted (internal/aws/ec2.go). Drop
	// it and the start is issued against an instance still stopping, which AWS
	// rejects. This is provider-agnostic code, so it must hold for the weakest
	// guarantee, not Azure's.
	want := []string{"stop", "wait", "start"}
	if len(prov.calls) != len(want) {
		t.Fatalf("call order = %v, want %v", prov.calls, want)
	}
	for i := range want {
		if prov.calls[i] != want[i] {
			t.Errorf("call order = %v, want %v", prov.calls, want)
			break
		}
	}
}

func TestRestartOfStoppedVMSkipsTheStop(t *testing.T) {
	// Nothing to deallocate: a stopped VM should go straight to starting.
	prov := &recordingProvider{}
	inst := &provider.Instance{Name: "DC01", ID: "id", State: "stopped"}

	out, err := captureStdout(t, func(outPath string) error {
		prov.outPath = outPath
		return execVMAction(context.Background(), prov, inst, "restart", false)
	})
	if err != nil {
		t.Fatalf("execVMAction: %v", err)
	}

	if len(prov.calls) != 1 || prov.calls[0] != "start" {
		t.Errorf("calls = %v, want [start]", prov.calls)
	}
	if strings.Contains(out, "Stopping") {
		t.Errorf("announced a stop for an already-stopped VM: %q", out)
	}
}

func TestStartAndStopAnnounceBeforeTheyWait(t *testing.T) {
	for _, tc := range []struct {
		action string
		call   string
		before string
	}{
		{"start", "start", "Starting DC01"},
		{"stop", "stop", "Stopping DC01"},
	} {
		prov := &recordingProvider{}
		inst := &provider.Instance{Name: "DC01", ID: "id", State: "running"}

		if _, err := captureStdout(t, func(outPath string) error {
			prov.outPath = outPath
			return execVMAction(context.Background(), prov, inst, tc.action, false)
		}); err != nil {
			t.Fatalf("%s: %v", tc.action, err)
		}
		if got := prov.seenAt[tc.call]; !strings.Contains(got, tc.before) {
			t.Errorf("%s began with no announcement; stdout was %q", tc.action, got)
		}
	}
}

func TestVMActionTimeoutIsBounded(t *testing.T) {
	// The regression this guards: ctx was context.Background(), so a stalled
	// Azure long-running operation hung the command with no deadline and no
	// output — indistinguishable from one that is merely slow.
	if vmActionTimeout <= 0 {
		t.Fatal("vmActionTimeout must be positive")
	}
	if vmActionTimeout.Minutes() < 5 {
		t.Errorf("vmActionTimeout %v is below a real deallocate+start", vmActionTimeout)
	}
	if vmActionTimeout.Minutes() > 60 {
		t.Errorf("vmActionTimeout %v is long enough to lose an afternoon", vmActionTimeout)
	}
}

// TestDestroyVMWithoutYesAbortsWhenNobodyCanAnswer covers the failure that made
// this flag necessary: the confirmation reads stdin, so a caller with no
// terminal cannot answer it. Scanln fails, the abort branch prints "Aborted."
// and returns nil — exit 0 for an instance that still exists. Any caller that
// trusts the exit code is told the VM was destroyed.
func TestDestroyVMWithoutYesAbortsWhenNobodyCanAnswer(t *testing.T) {
	prov := &recordingProvider{}
	inst := &provider.Instance{Name: "DC01", ID: "/subscriptions/x/DC01", State: "running"}

	// stdin at EOF is what a piped, non-interactive caller looks like.
	origStdin := os.Stdin
	empty, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	os.Stdin = empty
	defer func() { os.Stdin = origStdin; empty.Close() }()

	out, runErr := captureStdout(t, func(outPath string) error {
		prov.outPath = outPath
		return execVMAction(context.Background(), prov, inst, "destroy", false)
	})
	if runErr != nil {
		t.Fatalf("unexpected error: %v", runErr)
	}
	if len(prov.calls) != 0 {
		t.Errorf("destroyed the VM without a confirmation: calls = %v", prov.calls)
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("expected an abort notice, got %q", out)
	}
}

// ...and with --yes it proceeds, which is the entire reason the flag exists.
func TestDestroyVMWithYesSkipsThePrompt(t *testing.T) {
	prov := &recordingProvider{}
	inst := &provider.Instance{Name: "DC01", ID: "/subscriptions/x/DC01", State: "running"}

	origStdin := os.Stdin
	empty, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	os.Stdin = empty
	defer func() { os.Stdin = origStdin; empty.Close() }()

	out, runErr := captureStdout(t, func(outPath string) error {
		prov.outPath = outPath
		return execVMAction(context.Background(), prov, inst, "destroy", true)
	})
	if runErr != nil {
		t.Fatalf("destroy with --yes: %v", runErr)
	}
	if len(prov.calls) != 1 || prov.calls[0] != "destroy" {
		t.Fatalf("calls = %v, want [destroy]", prov.calls)
	}
	if strings.Contains(out, "Type the instance ID") {
		t.Errorf("--yes still prompted: %q", out)
	}
}

// The GetBool-on-an-unregistered-flag pattern silently returns (false, err).
// That exact shape caused a real bug elsewhere in this CLI, so pin the value
// each *-vm command actually sees: only destroy-vm registers --yes, and the
// other three must resolve false rather than anything surprising.
func TestYesResolvesFalseForCommandsThatDoNotRegisterIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		reg  bool
	}{
		{"start-vm", false}, {"stop-vm", false},
		{"restart-vm", false}, {"destroy-vm", true},
	} {
		var cmd = labStartVMCmd
		switch tc.name {
		case "stop-vm":
			cmd = labStopVMCmd
		case "restart-vm":
			cmd = labRestartVMCmd
		case "destroy-vm":
			cmd = labDestroyVMCmd
		}
		got, err := cmd.Flags().GetBool("yes")
		if tc.reg {
			if err != nil {
				t.Errorf("%s: destroy-vm must register --yes, got err %v", tc.name, err)
			}
		} else if err == nil {
			t.Errorf("%s: unexpectedly registers --yes", tc.name)
		}
		if got {
			t.Errorf("%s: --yes defaulted true; destroy would skip its prompt", tc.name)
		}
	}
}
