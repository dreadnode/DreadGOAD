package rangecommand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/variant"
)

func testConfig(t *testing.T, lab, manifest string) *config.Config {
	t.Helper()
	root := t.TempDir()
	labPath := filepath.Join(root, "ad", lab)
	if err := os.MkdirAll(labPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(labPath, "range.yml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &config.Config{ProjectRoot: root, Lab: lab, Env: "dev"}
}

func TestResolveCompatibilityAndFailClosedDefaults(t *testing.T) {
	legacy := testConfig(t, "legacy", "")
	capability, err := Require(legacy, "score")
	if err != nil {
		t.Fatal(err)
	}
	if capability.HandlerType != HandlerBuiltin || capability.Profile != ProfileActiveDir {
		t.Fatalf("legacy capability = %#v", capability)
	}

	service := testConfig(t, "service", `schema_version: 1
kind: service-range
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: health
`)
	_, err = Require(service, "score")
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Require() error = %v, want UnsupportedError", err)
	}
}

func TestResolveDisabledCommand(t *testing.T) {
	cfg := testConfig(t, "service", `schema_version: 1
kind: service-range
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: health
  scrub:
    enabled: false
    description: No persistent hosts to clean
`)
	capability, err := Resolve(cfg, "scrub")
	if err != nil {
		t.Fatal(err)
	}
	if capability.Supported || capability.Description != "No persistent hosts to clean" {
		t.Fatalf("disabled capability = %#v", capability)
	}
}

func TestExecuteRangeOwnedHandlerReceivesBoundedContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	cfg := testConfig(t, "service", `schema_version: 1
kind: service-range
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: health
  validate:
    protocol: validate/v1
    handler:
      type: executable
      path: commands/validate
`)
	handler := filepath.Join(cfg.LabPath(), "commands", "validate")
	if err := os.MkdirAll(filepath.Dir(handler), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
request=$(cat)
printf '%s\n' "$request" >&2
printf '%s\n' '{"schema":"validate/v1","passed":1,"failed":0,"checks":[]}'
`
	if err := os.WriteFile(handler, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	capability, err := Require(cfg, "validate")
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err = Execute(context.Background(), cfg, capability, Request{
		Options: map[string]any{"quick": true}, Arguments: []string{"fixture"},
	}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	var request Request
	if err := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &request); err != nil {
		t.Fatalf("request JSON: %v\n%s", err, stderr.String())
	}
	if request.Schema != "dreadgoad/range-command-request/v1" || request.Command != "validate" || request.Protocol != "validate/v1" {
		t.Fatalf("request = %#v", request)
	}
	if request.Environment != "dev" || request.LabPath != cfg.LabPath() {
		t.Fatalf("range context = %#v", request)
	}
	if !strings.Contains(stdout.String(), `"schema":"validate/v1"`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestVariantExecutesHandlerFromCompletedTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root := t.TempDir()
	source := filepath.Join(root, "ad", "BASE")
	target := filepath.Join(root, "ad", "BASE-random")
	manifest := `schema_version: 1
kind: active-directory
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: commands/health
`
	for _, dir := range []string{source, target} {
		if err := os.MkdirAll(filepath.Join(dir, "commands"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "range.yml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeHandler := func(path, detail string) {
		t.Helper()
		script := fmt.Sprintf("#!/bin/sh\nrequest=$(cat)\nprintf '%%s\\n' \"$PWD\" \"$request\" >&2\nprintf '%%s\\n' '{\"schema\":\"health/v1\",\"checks\":[{\"detail\":\"%s\"}]}'\n", detail)
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeHandler(filepath.Join(source, "commands", "health"), "source")
	writeHandler(filepath.Join(target, "commands", "health"), "target")
	if err := os.WriteFile(
		filepath.Join(target, variant.CompletionMarkerName), []byte("complete\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		ProjectRoot: root, Env: "random", Lab: "BASE",
		Environments: map[string]config.EnvironmentConfig{
			"random": {
				Lab: "BASE", Variant: true,
				VariantSource: "ad/BASE", VariantTarget: "ad/BASE-random",
			},
		},
	}

	capability, err := Require(cfg, "health")
	if err != nil {
		t.Fatal(err)
	}
	expectedHandler, err := filepath.EvalSymlinks(filepath.Join(target, "commands", "health"))
	if err != nil {
		t.Fatal(err)
	}
	if capability.RangeRoot != target || capability.Path != expectedHandler {
		t.Fatalf("variant capability = %#v", capability)
	}
	var stdout, stderr bytes.Buffer
	if err := Execute(context.Background(), cfg, capability, Request{}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "source") || !strings.Contains(stdout.String(), "target") {
		t.Fatalf("variant handler output = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), target) || !strings.Contains(stderr.String(), `"lab_path":"`+target+`"`) {
		t.Fatalf("variant handler context = %q", stderr.String())
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture")
	}
	cfg := testConfig(t, "service", `schema_version: 1
kind: service-range
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: commands/health
`)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	commandsDir := filepath.Join(cfg.LabPath(), "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(commandsDir, "health")); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(cfg, "health")
	if err == nil || !strings.Contains(err.Error(), "escapes the range directory") {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestExecuteRejectsWrongResultProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	cfg := testConfig(t, "service", `schema_version: 1
kind: service-range
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: health
  score:
    protocol: score/v1
    handler:
      type: executable
      path: score
`)
	handler := filepath.Join(cfg.LabPath(), "score")
	if err := os.WriteFile(handler, []byte("#!/bin/sh\nprintf '%s\\n' '{\"schema\":\"validate/v1\",\"checks\":[]}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	capability, err := Require(cfg, "score")
	if err != nil {
		t.Fatal(err)
	}
	err = Execute(context.Background(), cfg, capability, Request{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "invalid score/v1 result") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestExecuteRejectsOutputAfterFinalResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	cfg := testConfig(t, "service", `schema_version: 1
kind: service-range
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: health
  validate:
    protocol: validate/v1
    handler:
      type: executable
      path: validate
`)
	handler := filepath.Join(cfg.LabPath(), "validate")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"schema\":\"validate/v1\",\"checks\":[]}' 'trailing noise'\n"
	if err := os.WriteFile(handler, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	capability, err := Require(cfg, "validate")
	if err != nil {
		t.Fatal(err)
	}
	err = Execute(context.Background(), cfg, capability, Request{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "final stdout line") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestValidateResultRejectsNonObjectChecks(t *testing.T) {
	err := validateResult("health/v1", []byte(`{"schema":"health/v1","checks":[1]}`))
	if err == nil || !strings.Contains(err.Error(), "checks entries must be objects") {
		t.Fatalf("validateResult() error = %v", err)
	}
}

func TestHandlerEnvironmentKeepsCloudContextButNotModelSecrets(t *testing.T) {
	filtered := handlerEnvironment([]string{
		"PATH=/bin", "AWS_PROFILE=lab", "ARM_SUBSCRIPTION_ID=sub",
		"DREADGOAD_ENV=dev", "DREADGOAD_REGION=eastus2",
		"DREADGOAD_CONSOLE_AUTH_TOKEN=secret", "DREADGOAD_PROXMOX_PASSWORD=secret",
		"OPENROUTER_API_KEY=secret", "UNRELATED_SECRET=secret",
	})
	joined := strings.Join(filtered, "\n")
	for _, expected := range []string{
		"PATH=/bin", "AWS_PROFILE=lab", "ARM_SUBSCRIPTION_ID=sub",
		"DREADGOAD_ENV=dev", "DREADGOAD_REGION=eastus2",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("handler environment dropped %q: %v", expected, filtered)
		}
	}
	for _, forbidden := range []string{
		"DREADGOAD_CONSOLE_AUTH_TOKEN", "DREADGOAD_PROXMOX_PASSWORD",
		"OPENROUTER_API_KEY", "UNRELATED_SECRET",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("handler environment leaked %q: %v", forbidden, filtered)
		}
	}
}

func TestExecuteScoreInitializerUsesPrivateOutputContract(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	cfg := testConfig(t, "service", `schema_version: 1
kind: service-range
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: commands/health
  score:
    protocol: score/v1
    handler:
      type: executable
      path: commands/score
    initializer:
      type: executable
      path: commands/init-score
`)
	commandsDir := filepath.Join(cfg.LabPath(), "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"score", "init-score"} {
		if err := os.WriteFile(filepath.Join(commandsDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	outputDir := filepath.Join(t.TempDir(), "artifacts")
	artifact := filepath.Join(outputDir, "objectives.json")
	script := fmt.Sprintf("#!/bin/sh\ncat >/dev/null\nprintf 'objectives' > %q\nprintf '%%s\\n' %q\n", artifact, `{"schema":"session-init/v1","artifacts":["`+artifact+`"],"message":"ready"}`)
	if err := os.WriteFile(filepath.Join(commandsDir, "init-score"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	capability, err := Require(cfg, "score")
	if err != nil {
		t.Fatal(err)
	}
	if !capability.Initializes {
		t.Fatal("score initializer was not resolved")
	}
	var stdout bytes.Buffer
	if err := ExecuteScoreInitializer(context.Background(), cfg, capability, outputDir, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("initializer artifact: %v", err)
	}
}
