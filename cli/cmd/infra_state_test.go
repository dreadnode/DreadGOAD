package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestHasTerraformStateDetectsAppliedModules(t *testing.T) {
	// Terragrunt keeps state per module, so evidence sits in subdirectories
	// rather than at the working-directory root.
	dir := t.TempDir()
	mod := filepath.Join(dir, "goad", "dc01")
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	if hasTerraformState(dir) {
		t.Fatal("scaffold with no state must not read as applied")
	}
	if err := os.WriteFile(filepath.Join(mod, "terraform.tfstate"), []byte(`{"version":4,"resources":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !hasTerraformState(dir) {
		t.Fatal("a nested .tfstate must count as applied")
	}
}

func TestHasTerraformStateRejectsInitArtifacts(t *testing.T) {
	dir := t.TempDir()
	terraformDir := filepath.Join(dir, "network", ".terraform")
	if err := os.MkdirAll(terraformDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(terraformDir, "terraform.tfstate"), []byte(`{"version":3,"backend":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "network", ".terraform.lock.hcl"), []byte("# providers"), 0o600); err != nil {
		t.Fatal(err)
	}
	if hasTerraformState(dir) {
		t.Fatal("terraform init artifacts must not count as applied state")
	}
}

// The exact situation that produced the misleading error: the range is running
// in Azure, but this checkout has no state for it.
func TestDestroyWithoutStateExplainsWhyRecreatingWontHelp(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "dreadindex", "centralus")
	err := checkLocalInfraState(missing, "dreadindex", "centralus", "destroy")
	if err == nil {
		t.Fatal("destroy without state must fail")
	}
	msg := err.Error()
	for _, want := range []string{
		"cannot destroy dreadindex/centralus",
		"does not exist in this checkout",
		"only be destroyed from the working copy that deployed it",
		"plan to CREATE",
		"resource group",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	// The old message sent people after the directory; that hint must be gone.
	if strings.Contains(msg, "infra working directory not found") {
		t.Fatalf("still leads with the misleading directory framing:\n%s", msg)
	}
}

// A scaffolded-but-never-applied directory is the other half of the same bug:
// destroy would have run and quietly done nothing.
func TestDestroyOnScaffoldWithoutStateIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "goad", "dc01"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := checkLocalInfraState(dir, "test", "centralus", "destroy")
	if err == nil {
		t.Fatal("destroy on a never-applied scaffold must fail")
	}
	if !strings.Contains(err.Error(), "never been applied") {
		t.Fatalf("should say the directory exists but was never applied:\n%s", err)
	}
}

// Regression guard: a first apply has no state by definition and must proceed.
func TestApplyOnFreshScaffoldIsAllowed(t *testing.T) {
	dir := t.TempDir()
	if err := checkLocalInfraState(dir, "dev", "us-west-2", "apply"); err != nil {
		t.Fatalf("first apply must not be blocked: %v", err)
	}
	if err := checkInfraWorkDir(dir, "dev", "us-west-2", "plan"); err != nil {
		t.Fatalf("plan must not be blocked: %v", err)
	}
}

// A missing directory before apply is ordinary and keeps the old guidance.
func TestApplyOnMissingDirKeepsScaffoldGuidance(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	err := checkInfraWorkDir(missing, "dev", "us-west-2", "apply")
	if err == nil {
		t.Fatal("apply on a missing directory must fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "infra validate") || !strings.Contains(msg, "not been scaffolded") {
		t.Fatalf("apply should point at scaffolding, not at state:\n%s", msg)
	}
	if strings.Contains(msg, "plan to CREATE") {
		t.Fatalf("destroy-specific wording leaked into apply:\n%s", msg)
	}
}

// Destroy proceeds normally when state is present.
func TestDestroyWithStateProceeds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "terraform.tfstate"), []byte(`{"version":4,"resources":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkLocalInfraState(dir, "dev", "us-west-2", "destroy"); err != nil {
		t.Fatalf("destroy with state must proceed: %v", err)
	}
}

func TestRemoteBackendDestroyDoesNotRequireLocalState(t *testing.T) {
	dir := t.TempDir()
	if err := checkInfraWorkDir(dir, "dev", "us-west-2", "destroy"); err != nil {
		t.Fatalf("remote backend destroy must initialize and query its backend: %v", err)
	}
}

func TestInfraActionContextPreservesCommandCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cmd := &cobra.Command{}
	cmd.SetContext(parent)
	cmd.Flags().Duration("timeout", time.Minute, "")

	ctx, cancel := infraActionContext(cmd)
	defer cancel()
	cancelParent()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("infra context ignored command cancellation")
	}
}
