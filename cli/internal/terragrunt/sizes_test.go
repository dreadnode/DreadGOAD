package terragrunt

import (
	"os"
	"path/filepath"
	"testing"
)

// write creates dir/name with body, making parents as needed.
func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The shape of a real Azure env: literals in env.hcl for the units that resolve
// theirs from a local, literals in each lab host's unit file.
func TestRequestedSizesReadsTheEnvTree(t *testing.T) {
	root := t.TempDir()
	write(t, root, "env.hcl", `locals {
  controller_instance_size = "Standard_D2s_v3"
  kali_instance_size       = "Standard_D4s_v3"
}`)
	for _, host := range []string{"dc01", "dc02"} {
		write(t, filepath.Join(root, "eastus", "goad", host), "terragrunt.hcl", `inputs = {
  instance_size = "Standard_D2s_v3"
}`)
	}
	// controller and kali pass a local through rather than a literal — they are
	// still machines, and counting only literals would miss them entirely.
	write(t, filepath.Join(root, "eastus", "controller"), "terragrunt.hcl",
		"inputs = {\n  instance_size = local.controller_instance_size\n}")
	write(t, filepath.Join(root, "eastus", "kali"), "terragrunt.hcl",
		"inputs = {\n  instance_size = local.kali_instance_size\n}")

	got, err := RequestedSizes(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Standard_D2s_v3", "Standard_D4s_v3"}
	if len(got.Sizes) != len(want) {
		t.Fatalf("sizes = %v, want %v", got.Sizes, want)
	}
	for i := range want {
		if got.Sizes[i] != want[i] {
			t.Errorf("sizes = %v, want %v (sorted)", got.Sizes, want)
		}
	}
	if got.Units != 4 {
		t.Errorf("units = %d, want 4 (2 hosts + controller + kali)", got.Units)
	}
}

// The cache holds full copies of the upstream modules. Their variables.tf
// defaults name sizes this range never asked for, and an earlier version of
// this scan collected them.
func TestRequestedSizesIgnoresTerragruntCache(t *testing.T) {
	root := t.TempDir()
	write(t, root, "env.hcl", `locals {
  controller_instance_size = "Standard_D2s_v3"
}`)
	cache := filepath.Join(root, "eastus", "goad", "dc01", ".terragrunt-cache", "abc", "module")
	write(t, cache, "terragrunt.hcl", `inputs = {
  instance_size = "Standard_NEVER_ASKED_FOR"
}`)

	got, err := RequestedSizes(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got.Sizes {
		if s == "Standard_NEVER_ASKED_FOR" {
			t.Fatalf("collected a size from .terragrunt-cache: %v", got.Sizes)
		}
	}
	if got.Units != 0 {
		t.Errorf("units = %d, want 0 — a cached module copy is not a machine", got.Units)
	}
}

func TestRequestedSizesOnAnEmptyTree(t *testing.T) {
	// No scaffolding, no sizes — and no error. The caller reports "could not
	// tell" rather than treating this as a capacity problem.
	got, err := RequestedSizes(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sizes) != 0 || got.Units != 0 {
		t.Errorf("got %+v, want empty", got)
	}
}

func TestRequestedSizesSkipsUnrelatedFiles(t *testing.T) {
	root := t.TempDir()
	// A module's own terraform, sitting outside a cache: still not a unit file.
	write(t, root, "variables.tf", `variable "instance_size" { default = "Standard_D99_v9" }`)
	write(t, root, "env.hcl", `locals {
  controller_instance_size = "Standard_D2s_v3"
}`)
	got, err := RequestedSizes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sizes) != 1 || got.Sizes[0] != "Standard_D2s_v3" {
		t.Errorf("sizes = %v, want only the env.hcl literal", got.Sizes)
	}
}

// A commented-out size must not be collected: it would warn about a SKU the
// range never requests, and an operator who sees a false warning stops reading
// the real ones.
func TestRequestedSizesIgnoresComments(t *testing.T) {
	root := t.TempDir()
	write(t, root, "env.hcl", `locals {
  # controller_instance_size = "Standard_COMMENTED_OUT"
  #instance_size = "Standard_ALSO_COMMENTED"
  controller_instance_size = "Standard_D2s_v3"
}`)
	got, err := RequestedSizes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sizes) != 1 || got.Sizes[0] != "Standard_D2s_v3" {
		t.Errorf("sizes = %v, want only the live literal", got.Sizes)
	}
}

// An unreadable unit must not abort the scan — the sizes found elsewhere are
// still worth reporting on.
func TestRequestedSizesSurvivesAnUnreadableFile(t *testing.T) {
	root := t.TempDir()
	write(t, root, "env.hcl", `locals {
  controller_instance_size = "Standard_D2s_v3"
}`)
	unit := filepath.Join(root, "eastus", "goad", "dc01")
	write(t, unit, "terragrunt.hcl", `inputs = { instance_size = "Standard_D4s_v3" }`)
	if err := os.Chmod(filepath.Join(unit, "terragrunt.hcl"), 0o000); err != nil {
		t.Skip("cannot chmod in this environment")
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(unit, "terragrunt.hcl"), 0o600) })

	got, err := RequestedSizes(root)
	if err != nil {
		t.Fatalf("an unreadable unit aborted the whole scan: %v", err)
	}
	if len(got.Sizes) == 0 {
		t.Error("lost the sizes that were readable")
	}
}
