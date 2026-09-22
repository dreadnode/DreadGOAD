package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/variant"
	"github.com/spf13/viper"
)

func TestConfigFileUsedReturnsAbsolutePathForRelativeConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	absolute := filepath.Join(t.TempDir(), "custom.yaml")
	if err := os.WriteFile(absolute, []byte("environments: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(workingDir, absolute)
	if err != nil {
		t.Fatal(err)
	}
	viper.SetConfigFile(relative)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}

	if got := ConfigFileUsed(); got != absolute {
		t.Fatalf("ConfigFileUsed() = %q, want %q", got, absolute)
	}
}

func TestEffectiveRangeRootTracksVariantLifecycle(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "ad", "BASE")
	target := filepath.Join(root, "ad", "BASE-random")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		ProjectRoot: root,
		Env:         "random",
		Environments: map[string]EnvironmentConfig{
			"random": {
				Variant: true, VariantSource: "ad/BASE", VariantTarget: "ad/BASE-random",
			},
		},
	}

	resolved, err := cfg.EffectiveRangeRoot()
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != source || resolved.Source != RangeRootVariantSource {
		t.Fatalf("pending variant root = %#v", resolved)
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.EffectiveRangeRoot(); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("partial variant error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(target, variant.CompletionMarkerName), []byte("complete\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	resolved, err = cfg.EffectiveRangeRoot()
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != target || resolved.Source != RangeRootVariantTarget {
		t.Fatalf("completed variant root = %#v", resolved)
	}
}

func TestEffectiveRangeRootKeepsOrdinaryLabPath(t *testing.T) {
	cfg := &Config{ProjectRoot: t.TempDir(), Lab: "SERVICE", Env: "dev"}
	resolved, err := cfg.EffectiveRangeRoot()
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != cfg.LabPath() || resolved.Source != RangeRootBase {
		t.Fatalf("ordinary range root = %#v", resolved)
	}
}

func TestEffectiveRangeRootRejectsMissingVariantSource(t *testing.T) {
	root := t.TempDir()
	cfg := &Config{
		ProjectRoot: root, Env: "random",
		Environments: map[string]EnvironmentConfig{
			"random": {
				Variant: true, VariantSource: "ad/MISSING", VariantTarget: "ad/MISSING-random",
			},
		},
	}
	if _, err := cfg.EffectiveRangeRoot(); err == nil || !strings.Contains(err.Error(), "variant source") {
		t.Fatalf("missing variant source error = %v", err)
	}
}
