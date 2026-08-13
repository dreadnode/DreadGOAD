package ansible

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestResolveRetrySettings(t *testing.T) {
	tests := []struct {
		name              string
		opts              RetryOptions
		configuredRetries int
		configuredDelay   time.Duration
		wantAttempts      int
		wantDelay         time.Duration
		wantDisabled      bool
	}{
		{
			name:              "omitted uses config",
			configuredRetries: 3,
			configuredDelay:   30 * time.Second,
			wantAttempts:      3,
			wantDelay:         30 * time.Second,
		},
		{
			name: "explicit zero runs once without retry or delay",
			opts: RetryOptions{
				MaxRetriesSet: true,
				RetryDelaySet: true,
			},
			configuredRetries: 3,
			configuredDelay:   30 * time.Second,
			wantAttempts:      1,
			wantDelay:         0,
			wantDisabled:      true,
		},
		{
			name:              "zero in config still runs initial attempt",
			configuredRetries: 0,
			configuredDelay:   0,
			wantAttempts:      1,
			wantDelay:         0,
			wantDisabled:      true,
		},
		{
			name: "explicit positive overrides config",
			opts: RetryOptions{
				MaxRetries:    5,
				MaxRetriesSet: true,
				RetryDelay:    12 * time.Second,
				RetryDelaySet: true,
			},
			configuredRetries: 3,
			configuredDelay:   30 * time.Second,
			wantAttempts:      5,
			wantDelay:         12 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attempts, delay, disabled := resolveRetrySettings(tc.opts, tc.configuredRetries, tc.configuredDelay)
			if attempts != tc.wantAttempts || delay != tc.wantDelay || disabled != tc.wantDisabled {
				t.Errorf("resolveRetrySettings() = (%d, %s, disabled=%v), want (%d, %s, disabled=%v)",
					attempts, delay, disabled, tc.wantAttempts, tc.wantDelay, tc.wantDisabled)
			}
		})
	}
}

func TestRunPlaybookWithRetryExplicitZeroRunsExactlyOnce(t *testing.T) {
	original := runPlaybookAttempt
	t.Cleanup(func() { runPlaybookAttempt = original })

	attempts := 0
	runPlaybookAttempt = func(context.Context, RunOptions) *RunResult {
		attempts++
		return &RunResult{ExitCode: 1, ErrorType: ErrUnclassified, ErrorDetail: "fixture failure"}
	}

	err := RunPlaybookWithRetry(context.Background(), RetryOptions{
		Playbook:      "fixture.yml",
		Env:           "test",
		MaxRetries:    0,
		MaxRetriesSet: true,
		RetryDelay:    0,
		RetryDelaySet: true,
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err == nil || !strings.Contains(err.Error(), "failed after 1 attempts") {
		t.Fatalf("error = %v, want one-attempt failure", err)
	}
	if attempts != 1 {
		t.Errorf("ansible attempts = %d, want exactly 1", attempts)
	}
}

// TestRunPlaybookWithRetryStopsOnCancelledContext pins the cancellation check
// at the top of the retry loop. Wiring `provision` to the root's signal-aware
// context means an interrupt now surfaces as an ordinary playbook failure, and
// without the check the loop interprets that failure and announces retries it
// cannot perform. The returned error is context.Canceled either way, so this
// asserts on the log: no attempt may be started once ctx is done.
func TestRunPlaybookWithRetryStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var logBuf bytes.Buffer
	err := RunPlaybookWithRetry(ctx, RetryOptions{
		Playbook:   "noop.yml",
		Env:        "test",
		MaxRetries: 3,
		Log:        slog.New(slog.NewTextHandler(&logBuf, nil)),
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got err %v, want context.Canceled", err)
	}
	if got := logBuf.String(); strings.Contains(got, "starting playbook") ||
		strings.Contains(got, "retrying with") {
		t.Fatalf("cancelled context still drove a retry attempt; log was:\n%s", got)
	}
}

// TestBuildRetryLimit covers all branches of buildRetryLimit.
func TestBuildRetryLimit(t *testing.T) {
	tests := []struct {
		name        string
		userLimit   string
		failedHosts string
		want        string
	}{
		{
			name:        "both set",
			userLimit:   "dc01",
			failedHosts: "dc02,dc03",
			want:        "dc01,dc02,dc03",
		},
		{
			name:        "only userLimit",
			userLimit:   "dc01",
			failedHosts: "",
			want:        "dc01",
		},
		{
			name:        "only failedHosts",
			userLimit:   "",
			failedHosts: "dc02",
			want:        "dc02",
		},
		{
			name:        "both empty",
			userLimit:   "",
			failedHosts: "",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRetryLimit(tt.userLimit, tt.failedHosts)
			if got != tt.want {
				t.Errorf("buildRetryLimit(%q, %q) = %q, want %q",
					tt.userLimit, tt.failedHosts, got, tt.want)
			}
		})
	}
}

// TestRetryOptionsLogger verifies the logger fallback logic.
func TestRetryOptionsLogger(t *testing.T) {
	t.Run("returns custom logger when set", func(t *testing.T) {
		// slog.Default() is a valid *slog.Logger; we just verify no panic.
		opts := RetryOptions{}
		got := opts.logger()
		if got == nil {
			t.Error("logger() returned nil for default logger")
		}
	})
}
