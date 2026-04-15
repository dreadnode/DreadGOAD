package terragrunt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildValidEnvDir creates a minimal valid environment directory structure.
func buildValidEnvDir(t *testing.T, env, region string) string {
	t.Helper()
	base := t.TempDir()

	dirs := []string{
		filepath.Join(base, env, region, "network"),
		filepath.Join(base, env, region, "goad", "dc01"),
		filepath.Join(base, env, region, "goad", "dc02"),
		filepath.Join(base, env, region, "goad", "dc03"),
		filepath.Join(base, env, region, "goad", "srv02"),
		filepath.Join(base, env, region, "goad", "srv03"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
	}

	// Required files
	files := map[string]string{
		filepath.Join(base, "host.hcl"):           `host = "test"`,
		filepath.Join(base, "host-registry.yaml"): `hosts: []`,
		filepath.Join(base, env, "env.hcl"): `
deployment_name = "mydeployment"
aws_account_id  = "123456789012"
env             = "` + env + `"
`,
		filepath.Join(base, env, region, "region.hcl"): `
aws_region = "` + region + `"
`,
		filepath.Join(base, env, region, "network", "terragrunt.hcl"): `# network`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
	}
	return base
}

func TestValidationResult_OK(t *testing.T) {
	tests := []struct {
		name   string
		result ValidationResult
		want   bool
	}{
		{"no errors", ValidationResult{}, true},
		{"with errors", ValidationResult{Errors: []string{"oops"}}, false},
		{"only warnings", ValidationResult{Warnings: []string{"warn"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.OK(); got != tt.want {
				t.Errorf("OK() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateEnvironment_Valid(t *testing.T) {
	env := "myenv"
	region := "us-east-1"
	base := buildValidEnvDir(t, env, region)

	result := ValidateEnvironment(base, env, region)
	if !result.OK() {
		t.Errorf("expected valid environment, got errors: %v", result.Errors)
	}
}

func TestValidateEnvironment_MissingHostHCL(t *testing.T) {
	env := "myenv"
	region := "us-east-1"
	base := buildValidEnvDir(t, env, region)

	// Remove host.hcl
	require.NoError(t, os.Remove(filepath.Join(base, "host.hcl")))

	result := ValidateEnvironment(base, env, region)
	if result.OK() {
		t.Error("expected errors for missing host.hcl, got none")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "host.hcl") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error mentioning host.hcl, got: %v", result.Errors)
	}
}

func TestValidateEnvironment_MissingNetworkHCL(t *testing.T) {
	env := "myenv"
	region := "us-east-1"
	base := buildValidEnvDir(t, env, region)

	require.NoError(t, os.Remove(filepath.Join(base, env, region, "network", "terragrunt.hcl")))

	result := ValidateEnvironment(base, env, region)
	if result.OK() {
		t.Error("expected errors for missing network/terragrunt.hcl")
	}
}

func TestValidateEnvironment_ChangeMePlaceholder(t *testing.T) {
	env := "myenv"
	region := "us-east-1"
	base := buildValidEnvDir(t, env, region)

	// Overwrite env.hcl with CHANGE_ME
	envHCL := filepath.Join(base, env, "env.hcl")
	content := `
deployment_name = "CHANGE_ME"
aws_account_id  = "123456789012"
env             = "myenv"
`
	if err := os.WriteFile(envHCL, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result := ValidateEnvironment(base, env, region)
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "CHANGE_ME") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CHANGE_ME error, got: %v", result.Errors)
	}
}

func TestValidateEnvironment_EnvMismatch(t *testing.T) {
	env := "myenv"
	region := "us-east-1"
	base := buildValidEnvDir(t, env, region)

	// Overwrite env.hcl with wrong env value
	envHCL := filepath.Join(base, env, "env.hcl")
	content := `
deployment_name = "mydeployment"
aws_account_id  = "123456789012"
env             = "wrongenv"
`
	if err := os.WriteFile(envHCL, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result := ValidateEnvironment(base, env, region)
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "wrongenv") || strings.Contains(e, "does not match") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected env mismatch error, got: %v", result.Errors)
	}
}

func TestValidateEnvironment_RegionMismatch(t *testing.T) {
	env := "myenv"
	region := "us-east-1"
	base := buildValidEnvDir(t, env, region)

	// Overwrite region.hcl with wrong region
	regionHCL := filepath.Join(base, env, region, "region.hcl")
	content := `aws_region = "eu-west-1"`
	if err := os.WriteFile(regionHCL, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result := ValidateEnvironment(base, env, region)
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "eu-west-1") || strings.Contains(e, "does not match") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected region mismatch error, got: %v", result.Errors)
	}
}

func TestValidateEnvironment_MissingGOADHosts(t *testing.T) {
	env := "myenv"
	region := "us-east-1"
	base := buildValidEnvDir(t, env, region)

	// Remove all GOAD host dirs
	goadDir := filepath.Join(base, env, region, "goad")
	require.NoError(t, os.RemoveAll(goadDir))

	result := ValidateEnvironment(base, env, region)
	// Missing GOAD hosts produce warnings, not errors
	if len(result.Warnings) == 0 {
		t.Error("expected warnings for missing GOAD host configs, got none")
	}
}

func TestValidateHCLField_Present(t *testing.T) {
	result := &ValidationResult{}
	validateHCLField(`deployment_name = "myname"`, "deployment_name", result)
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got: %v", result.Errors)
	}
}

func TestValidateHCLField_Missing(t *testing.T) {
	result := &ValidationResult{}
	validateHCLField(`other_field = "value"`, "deployment_name", result)
	if len(result.Errors) == 0 {
		t.Error("expected error for missing field, got none")
	}
	if !strings.Contains(result.Errors[0], "deployment_name") {
		t.Errorf("error should mention field name, got: %v", result.Errors[0])
	}
}

func TestPrintValidationResult_NoErrors(t *testing.T) {
	// Just ensure it doesn't panic.
	result := &ValidationResult{}
	PrintValidationResult(result, "myenv", "us-east-1")
}

func TestPrintValidationResult_WithErrors(t *testing.T) {
	result := &ValidationResult{
		Errors:   []string{"missing file"},
		Warnings: []string{"host missing"},
	}
	PrintValidationResult(result, "myenv", "us-east-1")
}
