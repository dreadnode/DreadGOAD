package ansible

import (
	"slices"
	"testing"
)

func TestSanitizeAWSEnv(t *testing.T) {
	tests := []struct {
		name        string
		in          []string
		wantHas     []string
		wantMissing []string
	}{
		{
			name: "profile and access key both set drops profile",
			in: []string{
				"PATH=/usr/bin",
				"AWS_PROFILE=personal",
				"AWS_ACCESS_KEY_ID=AKIAEXAMPLE",
				"AWS_SESSION_TOKEN=tok",
			},
			wantHas:     []string{"PATH=/usr/bin", "AWS_ACCESS_KEY_ID=AKIAEXAMPLE", "AWS_SESSION_TOKEN=tok"},
			wantMissing: []string{"AWS_PROFILE=personal"},
		},
		{
			name: "profile alone is kept",
			in: []string{
				"AWS_PROFILE=personal",
				"HOME=/tmp",
			},
			wantHas:     []string{"AWS_PROFILE=personal", "HOME=/tmp"},
			wantMissing: nil,
		},
		{
			name: "access keys alone are kept",
			in: []string{
				"AWS_ACCESS_KEY_ID=AKIAEXAMPLE",
				"AWS_SESSION_TOKEN=tok",
			},
			wantHas:     []string{"AWS_ACCESS_KEY_ID=AKIAEXAMPLE", "AWS_SESSION_TOKEN=tok"},
			wantMissing: nil,
		},
		{
			name: "empty profile value with keys is a no-op",
			in: []string{
				"AWS_PROFILE=",
				"AWS_ACCESS_KEY_ID=AKIAEXAMPLE",
			},
			wantHas:     []string{"AWS_PROFILE=", "AWS_ACCESS_KEY_ID=AKIAEXAMPLE"},
			wantMissing: nil,
		},
		{
			name: "profile plus empty keys is a no-op",
			in: []string{
				"AWS_PROFILE=personal",
				"AWS_ACCESS_KEY_ID=",
				"AWS_SESSION_TOKEN=",
			},
			wantHas:     []string{"AWS_PROFILE=personal", "AWS_ACCESS_KEY_ID=", "AWS_SESSION_TOKEN="},
			wantMissing: nil,
		},
		{
			name: "profile plus session token only still drops profile",
			in: []string{
				"AWS_PROFILE=personal",
				"AWS_SESSION_TOKEN=tok",
			},
			wantHas:     []string{"AWS_SESSION_TOKEN=tok"},
			wantMissing: []string{"AWS_PROFILE=personal"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeAWSEnv(slices.Clone(tt.in))
			for _, want := range tt.wantHas {
				if !slices.Contains(got, want) {
					t.Errorf("expected env to contain %q, got %v", want, got)
				}
			}
			for _, missing := range tt.wantMissing {
				if slices.Contains(got, missing) {
					t.Errorf("expected env NOT to contain %q, got %v", missing, got)
				}
			}
		})
	}
}
