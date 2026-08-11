package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
)

func TestMaterializeLabConfigAllowsMissingOptionalConfig(t *testing.T) {
	cfg := &config.Config{ProjectRoot: t.TempDir(), Env: "dev"}

	if err := materializeLabConfig(cfg); err != nil {
		t.Fatalf("materializeLabConfig() error = %v, want nil", err)
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

func TestMaterializeLabConfigCreatesDestinationDirectory(t *testing.T) {
	root := t.TempDir()
	variantData := filepath.Join(root, "ad", "custom-variant", "data")
	if err := os.MkdirAll(variantData, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"variant":true}`)
	if err := os.WriteFile(filepath.Join(variantData, "config.json"), want, 0o644); err != nil {
		t.Fatal(err)
	}
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
	destination := filepath.Join(root, "ad", "GOAD", "data", "dev-config.json")
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read materialized config: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("materialized config = %s, want %s", got, want)
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

func TestMaterializeLabConfigReportsDirectoryCreationFailure(t *testing.T) {
	root := t.TempDir()
	variantData := filepath.Join(root, "ad", "custom-variant", "data")
	if err := os.MkdirAll(variantData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(variantData, "config.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ad", "GOAD"), []byte("not a directory"), 0o644); err != nil {
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
	if err == nil || !strings.Contains(err.Error(), "create lab config directory") {
		t.Fatalf("materializeLabConfig() error = %v, want directory creation error", err)
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
	destination := filepath.Join(root, "ad", "GOAD", "data", "dev-config.json")
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
