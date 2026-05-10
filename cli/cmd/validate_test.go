package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestParsePollInterval(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr string
	}{
		{name: "empty disables polling", in: "", want: 0},
		{name: "never disables polling", in: "never", want: 0},
		{name: "NEVER case-insensitive", in: "NEVER", want: 0},
		{name: "off disables polling", in: "off", want: 0},
		{name: "zero disables polling", in: "0", want: 0},
		{name: "0s disables polling", in: "0s", want: 0},
		{name: "whitespace trimmed", in: "  never  ", want: 0},

		{name: "exactly 1m allowed", in: "1m", want: time.Minute},
		{name: "5m allowed", in: "5m", want: 5 * time.Minute},
		{name: "60s equals 1m", in: "60s", want: time.Minute},
		{name: "1h allowed", in: "1h", want: time.Hour},

		{name: "30s rejected", in: "30s", wantErr: "at least 1m"},
		{name: "59s rejected", in: "59s", wantErr: "at least 1m"},
		{name: "negative rejected", in: "-1m", wantErr: "at least 1m"},
		{name: "garbage rejected", in: "abc", wantErr: "invalid poll interval"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePollInterval(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parsePollInterval(%q) = %v, want error containing %q", tc.in, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parsePollInterval(%q) error = %q, want substring %q", tc.in, err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePollInterval(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("parsePollInterval(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
