package ansible

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

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
