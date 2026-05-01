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

func TestRunChecks_GroupedOutput(t *testing.T) {
	v := &Validator{
		hosts: make(map[string]string),
	}

	// Each check's output must be a contiguous block — header followed by
	// its own results — not interleaved with other checks. Order between
	// checks is by completion time, not submission order: runChecks flushes
	// each check's buffered output as soon as the check returns so slow
	// providers (e.g. Azure Run Command) don't hide progress from operators.
	checks := []checkFunc{
		fakeCheck("A", 100*time.Millisecond, 2),
		fakeCheck("B", 50*time.Millisecond, 2),
		fakeCheck("C", 10*time.Millisecond, 2),
	}

	old := captureStdout(t)
	v.runChecks(context.Background(), checks)
	output := old.restore()

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		t.Fatal("no output produced")
	}

	for _, name := range []string{"A", "B", "C"} {
		header := indexOf(lines, "== "+name+" ==")
		r0 := indexOf(lines, fmt.Sprintf("result-%s-0", name))
		r1 := indexOf(lines, fmt.Sprintf("result-%s-1", name))
		if header == -1 || r0 == -1 || r1 == -1 {
			t.Fatalf("missing %s header or results in output:\n%s", name, output)
		}
		if r0 != header+1 || r1 != header+2 {
			t.Errorf("%s output not contiguous: header@%d r0@%d r1@%d\noutput:\n%s",
				name, header, r0, r1, output)
		}
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

// ---- Additional tests for validator.go and checks.go ----

func TestNewValidator_Defaults(t *testing.T) {
	v := NewValidator(nil, "testenv", false, nil, nil)
	if v == nil {
		t.Fatal("NewValidator returned nil")
	}
	if v.env != "testenv" {
		t.Errorf("env = %q, want %q", v.env, "testenv")
	}
	if v.verbose {
		t.Error("verbose should be false")
	}
	if v.log == nil {
		t.Error("log should not be nil")
	}
	if v.hosts == nil {
		t.Error("hosts map should not be nil")
	}
	if v.report.Env != "testenv" {
		t.Errorf("report.Env = %q, want %q", v.report.Env, "testenv")
	}
}

func TestNewValidator_ReportDate(t *testing.T) {
	before := time.Now().UTC().Truncate(time.Second)
	v := NewValidator(nil, "env", false, nil, nil)
	after := time.Now().UTC().Add(time.Second).Truncate(time.Second)

	parsed, err := time.Parse(time.RFC3339, v.report.Date)
	if err != nil {
		t.Fatalf("report.Date %q is not RFC3339: %v", v.report.Date, err)
	}
	if parsed.Before(before) || parsed.After(after) {
		t.Errorf("report.Date %v not in range [%v, %v]", parsed, before, after)
	}
}

func TestGetReport_Totals(t *testing.T) {
	v := &Validator{
		hosts: make(map[string]string),
		report: Report{
			Passed:   3,
			Failed:   2,
			Warnings: 1,
		},
	}
	r := v.GetReport()
	if r.Total != 6 {
		t.Errorf("Total = %d, want 6", r.Total)
	}
}

func TestSaveReport_Valid(t *testing.T) {
	v := &Validator{
		hosts: make(map[string]string),
		report: Report{
			Date:   time.Now().UTC().Format(time.RFC3339),
			Env:    "testenv",
			Passed: 1,
		},
	}
	dir := t.TempDir()
	path := dir + "/report.json"

	if err := v.SaveReport(path); err != nil {
		t.Fatalf("SaveReport: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "testenv") {
		t.Errorf("report JSON missing env: %s", data)
	}
}

func TestSaveReport_InvalidPath(t *testing.T) {
	v := &Validator{
		hosts:  make(map[string]string),
		report: Report{},
	}
	err := v.SaveReport("/nonexistent/dir/report.json")
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

func TestAddResult_StatusCounts(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		wantPass int
		wantFail int
		wantWarn int
	}{
		{"pass", "PASS", 1, 0, 0},
		{"fail", "FAIL", 0, 1, 0},
		{"warn", "WARN", 0, 0, 1},
		{"skip", "SKIP", 0, 0, 0},
		{"info", "INFO", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &Validator{
				hosts:  make(map[string]string),
				report: Report{},
			}
			var buf bytes.Buffer
			v.addResult(&buf, tt.status, "Cat", "Name", "Detail")

			if v.report.Passed != tt.wantPass {
				t.Errorf("Passed = %d, want %d", v.report.Passed, tt.wantPass)
			}
			if v.report.Failed != tt.wantFail {
				t.Errorf("Failed = %d, want %d", v.report.Failed, tt.wantFail)
			}
			if v.report.Warnings != tt.wantWarn {
				t.Errorf("Warnings = %d, want %d", v.report.Warnings, tt.wantWarn)
			}
			if len(v.report.Results) != 1 {
				t.Errorf("Results len = %d, want 1", len(v.report.Results))
			}
			r := v.report.Results[0]
			if r.Status != tt.status {
				t.Errorf("Status = %q, want %q", r.Status, tt.status)
			}
		})
	}
}

func TestAddResult_OutputContainsName(t *testing.T) {
	statuses := []string{"PASS", "FAIL", "WARN", "SKIP", "INFO"}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			v := &Validator{
				hosts:  make(map[string]string),
				report: Report{},
			}
			var buf bytes.Buffer
			v.addResult(&buf, status, "Cat", "MyCheckName", "")
			if !strings.Contains(buf.String(), "MyCheckName") {
				t.Errorf("output missing name for status %s: %q",
					status, buf.String())
			}
		})
	}
}

func TestHasHost(t *testing.T) {
	v := &Validator{
		hosts: map[string]string{
			"DC01": "i-12345",
		},
	}
	tests := []struct {
		host string
		want bool
	}{
		{"DC01", true},
		{"DC02", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := v.hasHost(tt.host); got != tt.want {
				t.Errorf("hasHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestParseOutputLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "single line",
			input: "user1",
			want:  []string{"user1"},
		},
		{
			name:  "multiple lines",
			input: "user1\nuser2\nuser3",
			want:  []string{"user1", "user2", "user3"},
		},
		{
			name:  "blank lines stripped",
			input: "user1\n\nuser2\n\n",
			want:  []string{"user1", "user2"},
		},
		{
			name:  "whitespace trimmed",
			input: "  user1  \n  user2  ",
			want:  []string{"user1", "user2"},
		},
		{
			name:  "only whitespace",
			input: "   \n   \n",
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseOutputLines(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d]=%q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPrintHeader(t *testing.T) {
	var buf bytes.Buffer
	printHeader(&buf, "Test Header")
	got := buf.String()
	if !strings.Contains(got, "Test Header") {
		t.Errorf("printHeader output missing header text: %q", got)
	}
	if !strings.Contains(got, "==") {
		t.Errorf("printHeader output missing == delimiters: %q", got)
	}
}
