package validate

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeCheck simulates a check that takes a fixed duration and writes output.
func fakeCheck(name string, delay time.Duration, results int) checkFunc {
	return func(ctx context.Context, w io.Writer) {
		printHeader(w, name)
		time.Sleep(delay)
		for i := range results {
			_, _ = fmt.Fprintf(w, "  result-%s-%d\n", name, i)
		}
	}
}

func TestRunChecks_OrderedOutput(t *testing.T) {
	v := &Validator{
		hosts: make(map[string]string),
	}

	// Check C is fastest, A is slowest — output must still be A, B, C order.
	checks := []checkFunc{
		fakeCheck("A", 100*time.Millisecond, 2),
		fakeCheck("B", 50*time.Millisecond, 2),
		fakeCheck("C", 10*time.Millisecond, 2),
	}

	// Capture stdout
	old := captureStdout(t)
	v.runChecks(context.Background(), checks)
	output := old.restore()

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		t.Fatal("no output produced")
	}

	// Verify order: A header must come before B header, B before C.
	aIdx := indexOf(lines, "== A ==")
	bIdx := indexOf(lines, "== B ==")
	cIdx := indexOf(lines, "== C ==")

	if aIdx == -1 || bIdx == -1 || cIdx == -1 {
		t.Fatalf("missing headers in output:\n%s", output)
	}
	if aIdx >= bIdx || bIdx >= cIdx {
		t.Errorf("output not in submission order: A@%d B@%d C@%d\noutput:\n%s", aIdx, bIdx, cIdx, output)
	}

	// Verify A's results come before B's header (grouped, not interleaved).
	aResult := indexOf(lines, "result-A-1")
	if aResult == -1 || aResult >= bIdx {
		t.Errorf("A results should be grouped before B header: result@%d B@%d", aResult, bIdx)
	}
}

func TestRunChecks_Concurrent(t *testing.T) {
	v := &Validator{
		hosts: make(map[string]string),
	}

	const numChecks = 5
	const checkDuration = 100 * time.Millisecond

	checks := make([]checkFunc, numChecks)
	for i := range numChecks {
		name := fmt.Sprintf("check-%d", i)
		checks[i] = fakeCheck(name, checkDuration, 1)
	}

	old := captureStdout(t)
	start := time.Now()
	v.runChecks(context.Background(), checks)
	elapsed := time.Since(start)
	old.restore()

	// If sequential: numChecks * 100ms = 500ms.
	// If concurrent (limit=5, all fit): ~100ms.
	// Allow generous margin but must be faster than sequential.
	maxExpected := checkDuration * time.Duration(numChecks) * 80 / 100
	if elapsed >= maxExpected {
		t.Errorf("checks appear sequential: took %v, expected well under %v", elapsed, maxExpected)
	}
}

func TestRunChecks_AllResultsCollected(t *testing.T) {
	v := &Validator{
		hosts: make(map[string]string),
		report: Report{
			Date: time.Now().UTC().Format(time.RFC3339),
		},
	}

	var count atomic.Int32
	checks := make([]checkFunc, 10)
	for i := range 10 {
		checks[i] = func(ctx context.Context, w io.Writer) {
			v.addResult(w, "PASS", "Test", fmt.Sprintf("check-%d", count.Add(1)), "")
		}
	}

	old := captureStdout(t)
	v.runChecks(context.Background(), checks)
	old.restore()

	report := v.GetReport()
	if report.Passed != 10 {
		t.Errorf("expected 10 passed, got %d", report.Passed)
	}
	if report.Total != 10 {
		t.Errorf("expected 10 total, got %d", report.Total)
	}
}

func TestRunChecks_SemaphoreLimitsConcurrency(t *testing.T) {
	v := &Validator{
		hosts: make(map[string]string),
	}

	var running atomic.Int32
	var maxRunning atomic.Int32

	checks := make([]checkFunc, 10)
	for i := range 10 {
		checks[i] = func(ctx context.Context, w io.Writer) {
			cur := running.Add(1)
			// Track peak concurrency
			for {
				prev := maxRunning.Load()
				if cur <= prev || maxRunning.CompareAndSwap(prev, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			running.Add(-1)
			_, _ = fmt.Fprintf(w, "done\n")
		}
	}

	old := captureStdout(t)
	v.runChecks(context.Background(), checks)
	old.restore()

	peak := maxRunning.Load()
	if peak > int32(maxConcurrentChecks) {
		t.Errorf("peak concurrency %d exceeded limit %d", peak, maxConcurrentChecks)
	}
	if peak < 2 {
		t.Errorf("peak concurrency %d suggests no parallelism", peak)
	}
}

// --- helpers ---

func indexOf(lines []string, substr string) int {
	for i, l := range lines {
		if strings.Contains(l, substr) {
			return i
		}
	}
	return -1
}

// stdoutCapture temporarily redirects os.Stdout to a pipe.
type stdoutCapture struct {
	orig *os.File
	r, w *os.File
	done chan captureResult
	t    *testing.T
}

type captureResult struct {
	output string
	err    error
}

func captureStdout(t *testing.T) *stdoutCapture {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w

	done := make(chan captureResult)
	go func() {
		var buf bytes.Buffer
		_, err := io.Copy(&buf, r)
		done <- captureResult{output: buf.String(), err: err}
	}()

	return &stdoutCapture{orig: orig, r: r, w: w, done: done, t: t}
}

func (c *stdoutCapture) restore() string {
	c.t.Helper()
	_ = c.w.Close()
	os.Stdout = c.orig
	res := <-c.done
	if res.err != nil {
		c.t.Fatalf("capturing stdout: %v", res.err)
	}
	return res.output
}
