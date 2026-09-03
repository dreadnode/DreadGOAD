package ansible

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
)

func TestBuildArgsEncodesExtraVarsAsSingleJSONObject(t *testing.T) {
	proxyArgs := "-o ProxyCommand='nc -X 5 -x 127.0.0.1:62103 %h %p' -o StrictHostKeyChecking=no"
	opts := RunOptions{
		Playbook: "scope-base.yml",
		Env:      "scope-dev",
		ExtraVars: map[string]string{
			"ansible_connection":      "ssh",
			"ansible_ssh_common_args": proxyArgs,
			"adversarial_value":       "quotes: 'single' \"double\"; equals=a=b; unicode=☃\nnext-line",
		},
	}

	args := buildArgs(opts, &config.Config{ProjectRoot: t.TempDir()})
	if len(args) < 2 || args[len(args)-2] != "-e" {
		t.Fatalf("extra vars missing from args: %v", args)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(args[len(args)-1]), &got); err != nil {
		t.Fatalf("extra vars are not a JSON object: %v", err)
	}
	if got["ansible_ssh_common_args"] != proxyArgs {
		t.Fatalf("proxy args = %q, want %q", got["ansible_ssh_common_args"], proxyArgs)
	}
	if got["ansible_connection"] != "ssh" {
		t.Fatalf("connection = %q, want ssh", got["ansible_connection"])
	}
	if got["adversarial_value"] != "quotes: 'single' \"double\"; equals=a=b; unicode=☃\nnext-line" {
		t.Fatalf("adversarial value was altered: %q", got["adversarial_value"])
	}
}

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
