package cmd

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Terraform state for this project is LOCAL and gitignored (see
// infra/*/goad-deployment/root.hcl and .gitignore). That has a consequence
// worth naming precisely, because the obvious error message hides it: a range
// can only be torn down from the working copy that deployed it. Everywhere
// else, the state describing those resources simply does not exist.
//
// The failure this produces is quiet and expensive. `infra destroy` reported
// only "infra working directory not found", which reads as "recreate the
// directory" — but a recreated directory starts from EMPTY state, so
// Terragrunt would plan to CREATE the range a second time rather than destroy
// the running one. An operator (or an agent) following that hint goes looking
// for a directory that would not have helped, while the real resources keep
// billing.

// hasTerraformState reports whether a working directory contains an actual
// Terraform state document. Terragrunt keeps local state per module, including
// beneath .terragrunt-cache, so the search is recursive. The .terraform tree is
// skipped because terraform init writes backend metadata named terraform.tfstate
// there before any resource state exists.
func hasTerraformState(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return nil //nolint:nilerr // unreadable subtree just means "no evidence here"
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".terraform" {
				return filepath.SkipDir
			}
			return nil
		}
		if (strings.HasSuffix(name, ".tfstate") || strings.HasSuffix(name, ".tfstate.backup")) && isTerraformStateDocument(path) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func isTerraformStateDocument(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var state struct {
		Version int `json:"version"`
	}
	return json.Unmarshal(data, &state) == nil && state.Version > 0
}

// infraStateError explains a missing or empty working directory in terms of
// what the operator can actually do next, rather than in terms of the path the
// code happened to look for.
//
// “action“ matters: an absent directory before `apply` means the environment
// was never scaffolded, which is ordinary and fixable. The same absence before
// `destroy` means the state is gone, which is neither.
func infraStateError(workDir, env, region, action string, dirExists bool) error {
	if action != "destroy" {
		// Only reached when the directory is absent: checkInfraWorkDir returns
		// early for a non-destroy action whose directory exists. An extra
		// `if dirExists { return nil }` here looked defensive but was
		// unreachable, and it swallowed the case where that gate is wrong —
		// mutating the gate produced no test failure until this was removed.
		return fmt.Errorf(
			"infra working directory not found: %s\n"+
				"Environment %q has not been scaffolded for region %q.\n"+
				"Run 'dreadgoad infra validate' to check your setup.",
			workDir, env, region)
	}

	// Destroy: the directory is beside the point — state is what's missing.
	detail := "it has never been applied (no Terraform state found)"
	if !dirExists {
		detail = "it does not exist in this checkout"
	}
	return fmt.Errorf(
		"cannot destroy %s/%s: %s\n"+
			"  expected working directory: %s\n\n"+
			"Terraform state for this project is local and gitignored, so a range can "+
			"only be destroyed from the working copy that deployed it. Recreating this "+
			"directory would start from empty state and plan to CREATE the range, not "+
			"tear it down.\n\n"+
			"If the range is still running, either run 'dreadgoad infra destroy' on the "+
			"machine that deployed it, or delete its cloud resources directly — on Azure "+
			"that is the range's resource group, which 'dreadgoad lab status --json' "+
			"reports as the \"group\" field.",
		env, region, detail, workDir)
}

// checkInfraWorkDir validates that a provider's rendered/scaffolded working
// directory exists. Remote backends such as AWS S3 are deliberately not gated
// on local state; Terragrunt initializes and reads that state from the backend.
func checkInfraWorkDir(workDir, env, region, action string) error {
	if _, err := os.Stat(workDir); err == nil {
		return nil
	}
	return fmt.Errorf(
		"infra working directory not found: %s\n"+
			"Environment %q has not been scaffolded for region %q.\n"+
			"Run 'dreadgoad infra validate' to check your setup.",
		workDir, env, region)
}

// checkLocalInfraState adds the destroy safety gate required by Azure's local
// backend. It must not be used for providers whose state is remote.
func checkLocalInfraState(workDir, env, region, action string) error {
	_, statErr := os.Stat(workDir)
	dirExists := statErr == nil
	if dirExists && (action != "destroy" || hasTerraformState(workDir)) {
		return nil
	}
	return infraStateError(workDir, env, region, action, dirExists)
}
