package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/variant"
)

func markVariantComplete(t *testing.T, target string) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(target, variant.CompletionMarkerName),
		[]byte("complete\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func TestMaterializeLabConfigAllowsMissingOptionalConfig(t *testing.T) {
	cfg := &config.Config{ProjectRoot: t.TempDir(), Env: "dev"}

	if err := materializeLabConfig(cfg); err != nil {
		t.Fatalf("materializeLabConfig() error = %v, want nil", err)
	}
}

func TestMaterializeLabConfigRejectsMissingVariantTarget(t *testing.T) {
	root := t.TempDir()
	baseData := filepath.Join(root, "ad", "GOAD", "data")
	if err := os.MkdirAll(baseData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseData, "config.json"), []byte(`{"base":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         "kraken",
		Environments: map[string]config.EnvironmentConfig{
			"kraken": {Variant: true, VariantTarget: "ad/GOAD-kraken"},
		},
	}

	err := materializeLabConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "variant target does not exist") {
		t.Fatalf("materializeLabConfig() error = %v, want missing variant target error", err)
	}
	if _, statErr := os.Stat(filepath.Join(baseData, "kraken-config.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("base config was materialized despite missing variant target: %v", statErr)
	}
}

func TestMaterializeLabConfigRejectsVariantTargetWithoutConfig(t *testing.T) {
	root := t.TempDir()
	variantData := filepath.Join(root, "ad", "GOAD-kraken", "data")
	if err := os.MkdirAll(variantData, 0o755); err != nil {
		t.Fatal(err)
	}
	markVariantComplete(t, filepath.Dir(variantData))
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         "kraken",
		Environments: map[string]config.EnvironmentConfig{
			"kraken": {Variant: true, VariantTarget: "ad/GOAD-kraken"},
		},
	}

	err := materializeLabConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "resolve variant lab config") ||
		!errors.Is(err, config.ErrLabConfigNotFound) {
		t.Fatalf("materializeLabConfig() error = %v, want missing variant config error", err)
	}
}

func TestMaterializeLabConfigRejectsIncompleteVariantTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "ad", "GOAD-kraken")
	variantData := filepath.Join(target, "data")
	if err := os.MkdirAll(variantData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(variantData, "config.json"), []byte(`{"partial":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         "kraken",
		Environments: map[string]config.EnvironmentConfig{
			"kraken": {Variant: true, VariantTarget: "ad/GOAD-kraken"},
		},
	}

	err := materializeLabConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "variant directory is incomplete") ||
		!strings.Contains(err.Error(), variant.CompletionMarkerName) {
		t.Fatalf("materializeLabConfig() error = %v, want incomplete variant error", err)
	}
	if _, statErr := os.Stat(filepath.Join(variantData, "kraken-config.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("partial variant config was materialized: %v", statErr)
	}
}

func TestMaterializeLabConfigUsesActiveLab(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "ad", "SERVICE", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"service":true}`)
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), want, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{ProjectRoot: root, Env: "service-dev", Lab: "SERVICE"}
	if err := materializeLabConfig(cfg); err != nil {
		t.Fatalf("materializeLabConfig() error: %v", err)
	}
	destination := filepath.Join(dataDir, "service-dev-config.json")
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("materialized config = %s, want %s", got, want)
	}
}

func TestMaterializeLabConfigSurfacesResolutionFailure(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "ad", "GOAD", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), []byte(`{"base":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "dev-overlay.json"), []byte(`{"broken":`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := materializeLabConfig(&config.Config{ProjectRoot: root, Env: "dev"})
	if err == nil || !strings.Contains(err.Error(), "resolve lab config: merge config") {
		t.Fatalf("materializeLabConfig() error = %v, want merge resolution error", err)
	}
	if errors.Is(err, config.ErrLabConfigNotFound) {
		t.Fatalf("malformed config was misclassified as missing: %v", err)
	}
}

func TestMaterializeLabConfigRejectsOverlayWithoutBase(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "ad", "GOAD", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "dev-overlay.json"), []byte(`{"present":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := materializeLabConfig(&config.Config{ProjectRoot: root, Env: "dev"})
	if err == nil || !strings.Contains(err.Error(), "overlay") || !strings.Contains(err.Error(), "requires base config") {
		t.Fatalf("materializeLabConfig() error = %v, want missing base config error", err)
	}
	if errors.Is(err, config.ErrLabConfigNotFound) {
		t.Fatalf("orphaned overlay was misclassified as missing: %v", err)
	}
}

func TestMaterializeLabConfigWritesVariantTarget(t *testing.T) {
	root := t.TempDir()
	variantData := filepath.Join(root, "ad", "custom-variant", "data")
	if err := os.MkdirAll(variantData, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"variant":true}`)
	if err := os.WriteFile(filepath.Join(variantData, "config.json"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	markVariantComplete(t, filepath.Dir(variantData))
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         "dev",
		Environments: map[string]config.EnvironmentConfig{
			"dev": {Variant: true, VariantTarget: "ad/custom-variant"},
		},
	}

	if err := materializeLabConfig(cfg); err != nil {
		t.Fatalf("materializeLabConfig() error: %v", err)
	}
	destination := filepath.Join(variantData, "dev-config.json")
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read materialized config: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("materialized config = %s, want %s", got, want)
	}
	wrongDestination := filepath.Join(root, "ad", "GOAD", "data", "dev-config.json")
	if _, err := os.Stat(wrongDestination); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("base-lab config unexpectedly materialized at %s: %v", wrongDestination, err)
	}
}

func TestMaterializeLabConfigWritesMergedVariantConfigToTarget(t *testing.T) {
	root := t.TempDir()
	variantData := filepath.Join(root, "ad", "GOAD-kraken", "data")
	if err := os.MkdirAll(variantData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(variantData, "config.json"),
		[]byte(`{"lab":{"name":"base","keep":true}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(variantData, "kraken-overlay.json"),
		[]byte(`{"lab":{"name":"kraken"}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	markVariantComplete(t, filepath.Dir(variantData))
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         "kraken",
		Environments: map[string]config.EnvironmentConfig{
			"kraken": {Variant: true, VariantTarget: "ad/GOAD-kraken"},
		},
	}

	if err := materializeLabConfig(cfg); err != nil {
		t.Fatalf("materializeLabConfig() error: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(variantData, "kraken-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Lab struct {
			Name string `json:"name"`
			Keep bool   `json:"keep"`
		} `json:"lab"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parse materialized config: %v", err)
	}
	if got.Lab.Name != "kraken" || !got.Lab.Keep {
		t.Errorf("materialized config did not merge base + overlay: %s", raw)
	}
}

func TestMaterializeLabConfigLeavesLegacyDestinationUntouched(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "ad", "GOAD", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dataDir, "dev-config.json")
	want := []byte(`{"legacy":true}`)
	if err := os.WriteFile(destination, want, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := materializeLabConfig(&config.Config{ProjectRoot: root, Env: "dev"}); err != nil {
		t.Fatalf("materializeLabConfig() error: %v", err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("legacy config changed: got %s, want %s", got, want)
	}
}

func TestMaterializeLabConfigReportsWriteFailure(t *testing.T) {
	root := t.TempDir()
	variantData := filepath.Join(root, "ad", "custom-variant", "data")
	if err := os.MkdirAll(variantData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(variantData, "config.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	markVariantComplete(t, filepath.Dir(variantData))
	destination := filepath.Join(variantData, "dev-config.json")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         "dev",
		Environments: map[string]config.EnvironmentConfig{
			"dev": {Variant: true, VariantTarget: "ad/custom-variant"},
		},
	}

	err := materializeLabConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "write lab config") {
		t.Fatalf("materializeLabConfig() error = %v, want write error", err)
	}
}

func TestShouldEnableAWSKali(t *testing.T) {
	kaliDir := filepath.Join(t.TempDir(), "kali")
	if err := os.Mkdir(kaliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		requested bool
		action    string
		path      string
		want      bool
	}{
		{name: "explicit apply", requested: true, action: "apply", path: filepath.Join(t.TempDir(), "missing"), want: true},
		{name: "ordinary apply", action: "apply", path: kaliDir, want: false},
		{name: "existing module on destroy", action: "destroy", path: kaliDir, want: true},
		{name: "missing module on destroy", action: "destroy", path: filepath.Join(t.TempDir(), "missing"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldEnableAWSKali(tt.requested, tt.action, tt.path); got != tt.want {
				t.Fatalf("shouldEnableAWSKali(%v, %q, %q) = %v, want %v", tt.requested, tt.action, tt.path, got, tt.want)
			}
		})
	}
}

func TestShouldBootstrapAWSBackendForOptedInProfile(t *testing.T) {
	automatic := rangeOperations{autoBootstrapAWSBackend: true}
	manual := rangeOperations{}
	for _, action := range []string{"init", "plan", "apply"} {
		if !shouldBootstrapAWSBackend(automatic, action, false) {
			t.Fatalf("opted-in profile should bootstrap its backend for %s", action)
		}
	}
	if shouldBootstrapAWSBackend(automatic, "destroy", false) {
		t.Fatal("destroy should not bootstrap a missing backend")
	}
	if shouldBootstrapAWSBackend(manual, "apply", false) {
		t.Fatal("manual profile changed its opt-in bootstrap behavior")
	}
	if !shouldBootstrapAWSBackend(manual, "apply", true) {
		t.Fatal("explicit backend bootstrap was ignored")
	}
}
