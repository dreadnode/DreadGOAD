package config

import (
	"os"
	"testing"
)

func TestResolveSource(t *testing.T) {
	fileKeys := map[string]bool{"region": true, "env": true}
	cfgFile := "/tmp/dreadgoad.yaml"

	tests := []struct {
		name         string
		key          string
		changedFlags map[string]bool
		envVar       string
		envVal       string
		wantContains string
	}{
		{
			name:         "cli flag takes precedence",
			key:          "env",
			changedFlags: map[string]bool{"env": true},
			wantContains: "cli flag",
		},
		{
			name:         "env var takes precedence over config file",
			key:          "region",
			changedFlags: map[string]bool{},
			envVar:       "DREADGOAD_REGION",
			envVal:       "us-west-2",
			wantContains: "env var",
		},
		{
			name:         "config file when no flag or env",
			key:          "region",
			changedFlags: map[string]bool{},
			wantContains: "config file",
		},
		{
			name:         "default when not in any source",
			key:          "max_retries",
			changedFlags: map[string]bool{},
			wantContains: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVar != "" {
				t.Setenv(tt.envVar, tt.envVal)
			} else {
				// Ensure the env var is not set from a prior test
				if err := os.Unsetenv("DREADGOAD_REGION"); err != nil {
					t.Fatalf("failed to unset env var: %v", err)
				}
			}
			got := resolveSource(tt.key, tt.changedFlags, fileKeys, cfgFile)
			if got == "" {
				t.Fatal("resolveSource returned empty string")
			}
			found := false
			if len(tt.wantContains) > 0 {
				for i := 0; i <= len(got)-len(tt.wantContains); i++ {
					if got[i:i+len(tt.wantContains)] == tt.wantContains {
						found = true
						break
					}
				}
			}
			if !found {
				t.Errorf("resolveSource(%q) = %q, want to contain %q", tt.key, got, tt.wantContains)
			}
		})
	}
}

func TestTraceConfig(t *testing.T) {
	cfg := &Config{
		Env:         "staging",
		Region:      "us-east-1",
		Debug:       false,
		MaxRetries:  3,
		RetryDelay:  30,
		IdleTimeout: 1200,
		LogDir:      "/tmp/logs",
		ProjectRoot: "/opt/goad",
		Infra: InfraConfig{
			Deployment:       "goad-deployment",
			TerragruntBinary: "terragrunt",
			TerraformBinary:  "tofu",
		},
	}

	entries := TraceConfig(cfg, map[string]bool{})
	if len(entries) == 0 {
		t.Fatal("TraceConfig returned no entries")
	}

	// Verify all expected keys are present.
	gotKeys := make(map[string]bool)
	for _, e := range entries {
		gotKeys[e.Key] = true
		if e.Value == "" {
			t.Errorf("entry %q has empty value", e.Key)
		}
		if e.Source == "" {
			t.Errorf("entry %q has empty source", e.Key)
		}
	}

	for _, want := range []string{"env", "region", "debug", "max_retries", "project_root"} {
		if !gotKeys[want] {
			t.Errorf("TraceConfig missing key %q", want)
		}
	}
}

func TestViperKeyToFlag(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"env", "env"},
		{"max_retries", "max-retries"},
		{"infra.deployment", "infra.deployment"},
	}
	for _, tt := range tests {
		got := viperKeyToFlag(tt.key)
		if got != tt.want {
			t.Errorf("viperKeyToFlag(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}
