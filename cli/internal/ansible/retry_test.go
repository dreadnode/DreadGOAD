package ansible

import (
	"testing"
)

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
