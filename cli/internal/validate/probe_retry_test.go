package validate

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

// stubProvider implements provider.Provider but only RunCommand is live; the
// rest are no-ops sufficient to satisfy the interface for unit tests.
type stubProvider struct {
	run func(call int, command string) (*provider.CommandResult, error)
	n   atomic.Int64
}

func (s *stubProvider) RunCommand(_ context.Context, _, command string, _ time.Duration) (*provider.CommandResult, error) {
	call := int(s.n.Add(1))
	return s.run(call, command)
}

func (s *stubProvider) Name() string                                      { return "stub" }
func (s *stubProvider) VerifyCredentials(context.Context) (string, error) { return "stub", nil }
func (s *stubProvider) DiscoverInstances(context.Context, string) ([]provider.Instance, error) {
	return nil, nil
}
func (s *stubProvider) DiscoverAllInstances(context.Context, string) ([]provider.Instance, error) {
	return nil, nil
}
func (s *stubProvider) FindInstanceByHostname(context.Context, string, string) (*provider.Instance, error) {
	return nil, nil
}
func (s *stubProvider) StartInstances(context.Context, []string) error       { return nil }
func (s *stubProvider) StopInstances(context.Context, []string) error        { return nil }
func (s *stubProvider) WaitForInstanceStopped(context.Context, string) error { return nil }
func (s *stubProvider) DestroyInstances(context.Context, []string) error     { return nil }
func (s *stubProvider) RunCommandOnMultiple(context.Context, []string, string, time.Duration) (map[string]*provider.CommandResult, error) {
	return nil, nil
}
func (s *stubProvider) CleanupStaleSessions(context.Context, []string, time.Duration, bool) (int, error) {
	return 0, nil
}
func (s *stubProvider) DescribeActiveSessions(context.Context, string) ([]provider.Session, error) {
	return nil, nil
}
func (s *stubProvider) Drain() {}

func newStubValidator(t *testing.T, run func(call int, command string) (*provider.CommandResult, error)) (*Validator, *stubProvider) {
	t.Helper()
	// Shrink the retry backoff so retry paths don't sleep whole seconds.
	old := backoffBase
	backoffBase = time.Millisecond
	t.Cleanup(func() { backoffBase = old })

	prov := &stubProvider{run: run}
	v := &Validator{
		provider: prov,
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		hosts:    map[string]string{"SRV03": "i-123"},
	}
	return v, prov
}

// A completed query with zero rows must be reported as a genuine negative
// (ok=true, empty rows) on the very first call — no retries, no false WARN.
func TestMSSQLProbe_GenuineEmptyIsDefinitive(t *testing.T) {
	v, prov := newStubValidator(t, func(_ int, _ string) (*provider.CommandResult, error) {
		// Query ran, returned no rows, but the sentinel still prints.
		return &provider.CommandResult{Status: "Success", Stdout: sqlProbeSentinel + "\n"}, nil
	})

	rows, ok := v.mssqlProbe(context.Background(), "SRV03", "SELECT 1 WHERE 1=0", nil)
	if !ok {
		t.Fatal("genuine empty result must return ok=true (definitive), got ok=false")
	}
	if rows != "" {
		t.Errorf("expected empty rows, got %q", rows)
	}
	if got := prov.n.Load(); got != 1 {
		t.Errorf("expected exactly 1 call (no retry on definitive result), got %d", got)
	}
}

// Empty stdout (sentinel absent) is the post-provision settling signature: the
// probe must retry and, if it never settles, return ok=false so the caller
// WARNs instead of emitting a bogus "NOT configured" FAIL.
func TestMSSQLProbe_TransientEmptyRetriesThenWarns(t *testing.T) {
	v, prov := newStubValidator(t, func(_ int, _ string) (*provider.CommandResult, error) {
		return &provider.CommandResult{Status: "Success", Stdout: ""}, nil
	})

	rows, ok := v.mssqlProbe(context.Background(), "SRV03", "SELECT 1", nil)
	if ok {
		t.Fatal("sentinel-absent output must return ok=false, got ok=true")
	}
	if rows != "" {
		t.Errorf("expected empty rows on transient failure, got %q", rows)
	}
	if got := prov.n.Load(); got != int64(transientRetries) {
		t.Errorf("expected %d attempts, got %d", transientRetries, got)
	}
}

// A host that settles mid-probe (empty, then a real row with the sentinel)
// must recover and report the real result with ok=true.
func TestMSSQLProbe_RecoversAfterSettling(t *testing.T) {
	v, _ := newStubValidator(t, func(call int, _ string) (*provider.CommandResult, error) {
		if call < transientRetries {
			return &provider.CommandResult{Status: "Success", Stdout: ""}, nil
		}
		return &provider.CommandResult{Status: "Success", Stdout: "ESSOS\\khal.drogo\n" + sqlProbeSentinel}, nil
	})

	rows, ok := v.mssqlProbe(context.Background(), "SRV03", "SELECT m.name ...", nil)
	if !ok {
		t.Fatal("probe should recover once the host settles, got ok=false")
	}
	if rows != `ESSOS\khal.drogo` {
		t.Errorf("expected row %q, got %q", `ESSOS\khal.drogo`, rows)
	}
}

// A row value that contains the sentinel substring but is not the final line
// must be treated as sentinel-absent (ok=false): the probe anchors on the last
// line so truncated output or a poisoned row cannot fake completion.
func TestMSSQLProbe_SentinelMustBeFinalLine(t *testing.T) {
	v, _ := newStubValidator(t, func(_ int, _ string) (*provider.CommandResult, error) {
		// Sentinel appears embedded in a row, but the final line is a real
		// row rather than the sentinel — the query never completed.
		return &provider.CommandResult{Status: "Success", Stdout: "prefix " + sqlProbeSentinel + " suffix\nrow-after-sentinel\n"}, nil
	})

	rows, ok := v.mssqlProbe(context.Background(), "SRV03", "SELECT 1", nil)
	if ok {
		t.Fatalf("sentinel not on final line must return ok=false, got ok=true rows=%q", rows)
	}
	if rows != "" {
		t.Errorf("expected empty rows on incomplete probe, got %q", rows)
	}
}

// A row whose value happens to contain the sentinel substring must survive
// intact once the real sentinel arrives on its own final line.
func TestMSSQLProbe_PreservesRowContainingSentinelSubstring(t *testing.T) {
	poisoned := "value-with-" + sqlProbeSentinel + "-inside"
	v, _ := newStubValidator(t, func(_ int, _ string) (*provider.CommandResult, error) {
		return &provider.CommandResult{Status: "Success", Stdout: poisoned + "\n" + sqlProbeSentinel + "\n"}, nil
	})

	rows, ok := v.mssqlProbe(context.Background(), "SRV03", "SELECT 1", nil)
	if !ok {
		t.Fatal("completed probe must return ok=true even when rows contain the sentinel substring")
	}
	if rows != poisoned {
		t.Errorf("row corrupted by sentinel stripping: expected %q, got %q", poisoned, rows)
	}
}

// A transport error (slow/dead host) must NOT trigger the outer retry loop —
// runPSErr already retried it, and re-running multiplies latency against a
// host that is timing out. An unknown host makes runPSErr return immediately
// without ever reaching RunCommand, so the probe must give up after one pass.
func TestMSSQLProbe_TransportErrorDoesNotAmplify(t *testing.T) {
	v, prov := newStubValidator(t, func(_ int, _ string) (*provider.CommandResult, error) {
		t.Fatal("RunCommand must not be reached for an unknown host")
		return nil, nil
	})

	rows, ok := v.mssqlProbe(context.Background(), "GHOST", "SELECT 1", nil)
	if ok {
		t.Fatal("transport error must return ok=false")
	}
	if rows != "" {
		t.Errorf("expected empty rows, got %q", rows)
	}
	if got := prov.n.Load(); got != 0 {
		t.Errorf("RunCommand should never run for unknown host, got %d calls", got)
	}
}

// runPSErr is the central fix: a generic probe whose output is empty-but-
// successful (a settling host) must be retried so checks across every category
// — not just MSSQL — stop reading a healthy lab as broken. A definitive result
// returns immediately without retrying.
func TestRunPSErr_RetriesEmptySuccessfulOutput(t *testing.T) {
	v, prov := newStubValidator(t, func(call int, _ string) (*provider.CommandResult, error) {
		if call < transientRetries {
			return &provider.CommandResult{Status: "Success", Stdout: "   "}, nil
		}
		return &provider.CommandResult{Status: "Success", Stdout: "USER_FOUND"}, nil
	})

	out, err := v.runPSErr(context.Background(), "SRV03", "Get-ADUser ...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "USER_FOUND" {
		t.Errorf("expected recovered output, got %q", out)
	}
	if n := prov.n.Load(); n != int64(transientRetries) {
		t.Errorf("expected %d attempts, got %d", transientRetries, n)
	}
}

func TestRunPSErr_NonEmptyReturnsImmediately(t *testing.T) {
	v, prov := newStubValidator(t, func(_ int, _ string) (*provider.CommandResult, error) {
		return &provider.CommandResult{Status: "Success", Stdout: "USER_NOT_FOUND"}, nil
	})

	out, err := v.runPSErr(context.Background(), "SRV03", "Get-ADUser ...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "USER_NOT_FOUND" {
		t.Errorf("expected definitive output, got %q", out)
	}
	if n := prov.n.Load(); n != 1 {
		t.Errorf("a definitive (non-empty) result must not retry, got %d calls", n)
	}
}

// retryMustExist must return immediately on a positive result without retrying.
func TestRetryMustExist_PositiveShortCircuits(t *testing.T) {
	v, _ := newStubValidator(t, func(int, string) (*provider.CommandResult, error) {
		return &provider.CommandResult{Status: "Success", Stdout: "x"}, nil
	})
	calls := 0
	out := v.retryMustExist(context.Background(), func() probeOutcome {
		calls++
		return probePositive
	})
	if out != probePositive {
		t.Fatalf("expected probePositive, got %d", out)
	}
	if calls != 1 {
		t.Errorf("positive must not retry, got %d calls", calls)
	}
}

// A must-exist entity reported absent is retried; a negative that persists
// across every attempt is ultimately trusted (a genuine misconfiguration).
func TestRetryMustExist_NegativeRetriedThenTrusted(t *testing.T) {
	v, _ := newStubValidator(t, func(int, string) (*provider.CommandResult, error) {
		return &provider.CommandResult{Status: "Success", Stdout: "x"}, nil
	})
	calls := 0
	out := v.retryMustExist(context.Background(), func() probeOutcome {
		calls++
		return probeNegative
	})
	if out != probeNegative {
		t.Fatalf("expected probeNegative after retries, got %d", out)
	}
	if calls != transientRetries {
		t.Errorf("expected %d attempts, got %d", transientRetries, calls)
	}
}

// A momentary negative that clears (DC mid-replication, share re-registering)
// must recover to positive rather than reporting a false FAIL.
func TestRetryMustExist_RecoversFromTransientNegative(t *testing.T) {
	v, _ := newStubValidator(t, func(int, string) (*provider.CommandResult, error) {
		return &provider.CommandResult{Status: "Success", Stdout: "x"}, nil
	})
	calls := 0
	out := v.retryMustExist(context.Background(), func() probeOutcome {
		calls++
		if calls < transientRetries {
			return probeNegative
		}
		return probePositive
	})
	if out != probePositive {
		t.Fatalf("expected recovery to probePositive, got %d", out)
	}
}

// A probeIncomplete (transport error) must NOT be retried — runPSErr already
// retried it and it feeds dead-host marking, so re-running would amplify
// latency against a slow or dead host.
func TestRetryMustExist_IncompleteDoesNotRetry(t *testing.T) {
	v, _ := newStubValidator(t, func(int, string) (*provider.CommandResult, error) {
		return &provider.CommandResult{Status: "Success", Stdout: "x"}, nil
	})
	calls := 0
	out := v.retryMustExist(context.Background(), func() probeOutcome {
		calls++
		return probeIncomplete
	})
	if out != probeIncomplete {
		t.Fatalf("expected probeIncomplete, got %d", out)
	}
	if calls != 1 {
		t.Errorf("incomplete must not retry (avoid amplifying a dead host), got %d calls", calls)
	}
}

// A ctx cancellation during backoff must surface as probeIncomplete (WARN),
// not the last probeNegative — a negative observed mid-cancellation is not
// authoritative and must not become a false FAIL.
func TestRetryMustExist_CtxCancelDuringBackoffIsIncomplete(t *testing.T) {
	v, _ := newStubValidator(t, func(int, string) (*provider.CommandResult, error) {
		return &provider.CommandResult{Status: "Success", Stdout: "x"}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	out := v.retryMustExist(ctx, func() probeOutcome {
		calls++
		// Cancel after the first probe so the backoff between attempts
		// bails out with ctx.Err().
		cancel()
		return probeNegative
	})
	if out != probeIncomplete {
		t.Fatalf("ctx cancel during backoff must return probeIncomplete, got %d", out)
	}
	if calls != 1 {
		t.Errorf("expected exactly one probe before cancellation, got %d", calls)
	}
}

// runScriptJSON must retry when the JSON envelope is missing (a settling host
// returns banner-only or empty stdout) and succeed once it appears.
func TestRunScriptJSON_RetriesUntilEnvelopeAppears(t *testing.T) {
	type payload struct {
		X int `json:"x"`
	}
	v, prov := newStubValidator(t, func(call int, _ string) (*provider.CommandResult, error) {
		if call < transientRetries {
			return &provider.CommandResult{Status: "Success", Stdout: "WinRM banner, no envelope yet"}, nil
		}
		return &provider.CommandResult{Status: "Success", Stdout: jsonBegin + `{"x":7}` + jsonEnd}, nil
	})

	got, err := runScriptJSON[payload](context.Background(), v, "SRV03", "irrelevant", nil)
	if err != nil {
		t.Fatalf("expected success after envelope appears, got %v", err)
	}
	if got.X != 7 {
		t.Errorf("expected x=7, got %d", got.X)
	}
	if n := prov.n.Load(); n != int64(transientRetries) {
		t.Errorf("expected %d attempts, got %d", transientRetries, n)
	}
}
