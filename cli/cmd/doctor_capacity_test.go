package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
)

// Every path below returns before a client is built, so none of them touch the
// network. That is the property under test: a pre-flight check that cannot
// answer must degrade to a warning, never block `up` and never panic.

func TestCapacityChecksSkipNonAzureProviders(t *testing.T) {
	for _, provider := range []string{"aws", "proxmox", "ludus", ""} {
		cfg := &config.Config{Provider: provider, Region: "eastus"}
		if got := azureCapacityChecks(cfg); got != nil {
			t.Errorf("provider %q produced %d Azure check(s); it must produce none",
				provider, len(got))
		}
	}
}

func TestCapacityChecksWarnWithoutARegion(t *testing.T) {
	cfg := &config.Config{Provider: "azure", Region: ""}
	got := azureCapacityChecks(cfg)
	if len(got) != 1 || got[0].Status != "warn" {
		t.Fatalf("got %+v, want a single warn", got)
	}
	if !strings.Contains(got[0].Message, "region") {
		t.Errorf("message does not say why: %q", got[0].Message)
	}
}

func TestCapacityChecksWarnWhenTheEnvIsNotScaffolded(t *testing.T) {
	// The normal state for a brand-new environment: `up` runs before the
	// terragrunt tree exists. Nothing to read, so nothing to assert about.
	cfg := &config.Config{
		Provider:    "azure",
		Region:      "eastus",
		Env:         "never-created",
		ProjectRoot: t.TempDir(),
	}
	cfg.Infra.Deployment = "goad-deployment"

	got := azureCapacityChecks(cfg)
	if len(got) != 1 || got[0].Status != "warn" {
		t.Fatalf("got %+v, want a single warn", got)
	}
	if !strings.Contains(got[0].Message, "no scaffolding") {
		t.Errorf("message does not name the cause: %q", got[0].Message)
	}
}

func TestCapacityChecksWarnWhenNoSizesAreDeclared(t *testing.T) {
	// A tree that exists but declares no literal size — e.g. every size is
	// interpolated. Reporting a pass here would claim the region was checked.
	root := t.TempDir()
	envDir := filepath.Join(root, "infra", "azure", "goad-deployment", "e1")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "env.hcl"), []byte("locals {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Provider: "azure", Region: "eastus", Env: "e1", ProjectRoot: root}
	cfg.Infra.Deployment = "goad-deployment"

	got := azureCapacityChecks(cfg)
	if len(got) != 1 || got[0].Status != "warn" {
		t.Fatalf("got %+v, want a single warn", got)
	}
	if !strings.Contains(got[0].Message, "could not read the VM sizes") {
		t.Errorf("message does not name the cause: %q", got[0].Message)
	}
}

// Nothing this check emits may be "fail": doctor.PrintResults turns a fail into
// an aborted `up`, and none of these signals is certain enough to justify that.
func TestCapacityChecksNeverEmitFail(t *testing.T) {
	cases := []*config.Config{
		{Provider: "azure", Region: ""},
		{Provider: "azure", Region: "eastus", Env: "nope", ProjectRoot: t.TempDir()},
	}
	for _, cfg := range cases {
		for _, r := range azureCapacityChecks(cfg) {
			if r.Status == "fail" {
				t.Errorf("check %q returned fail, which would abort up: %q", r.Name, r.Message)
			}
		}
	}
}
