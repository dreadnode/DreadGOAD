package terragrunt

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Requested is the compute an environment's terragrunt tree asks for.
type Requested struct {
	// Distinct VM sizes named anywhere in the tree, sorted.
	Sizes []string
	// Number of terragrunt units that deploy a VM. Each unit directory is one
	// machine: the five lab hosts plus the controller, and kali when enabled.
	Units int
}

// instanceSizeRE matches `instance_size = "..."` and its prefixed forms
// (`controller_instance_size`, `kali_instance_size`) as written in env.hcl and
// unit files. Assignments that reference a local (`= local.controller_instance_size`)
// deliberately do not match: the literal they resolve to is declared in env.hcl,
// which this scan also reads, so the size is still collected exactly once.
var instanceSizeRE = regexp.MustCompile(`(?m)^\s*[a-z_]*instance_size\s*=\s*"([^"]+)"`)

// unitSizeRE matches the bare `instance_size` input a unit passes to its
// module, whatever the right-hand side. Counting on the literal alone
// undercounts: the controller and kali units resolve theirs from env.hcl
// (`instance_size = local.controller_instance_size`) and would not be seen as
// machines at all.
var unitSizeRE = regexp.MustCompile(`(?m)^\s*instance_size\s*=`)

// RequestedSizes reports the VM sizes an environment's terragrunt tree asks
// for, and how many VMs it deploys.
//
// Reads only `env.hcl` and `terragrunt.hcl`, and never descends into
// `.terragrunt-cache`: those caches hold full copies of the upstream modules,
// whose `variables.tf` defaults would otherwise be collected as sizes this
// range never requests.
//
// Text-scanned rather than HCL-evaluated on purpose. Evaluating would mean
// resolving locals, includes and dependency outputs — terragrunt's whole
// pipeline — to learn something the literals state plainly. The cost is that a
// size assembled by interpolation is missed, which is why callers should treat
// an empty result as "could not tell" rather than "nothing to check".
func RequestedSizes(envDir string) (Requested, error) {
	seen := map[string]bool{}
	var out Requested

	err := filepath.WalkDir(envDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip module caches and any VCS metadata that happens to sit here.
			if d.Name() == ".terragrunt-cache" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if name != "terragrunt.hcl" && name != "env.hcl" {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			// An unreadable unit should not fail the whole scan; the caller
			// reports on what was found and a missing size is not fatal.
			return nil //nolint:nilerr // best-effort scan
		}
		matches := instanceSizeRE.FindAllStringSubmatch(string(body), -1)
		for _, m := range matches {
			size := strings.TrimSpace(m[1])
			if size != "" && !seen[size] {
				seen[size] = true
				out.Sizes = append(out.Sizes, size)
			}
		}
		// One unit file that deploys a VM is one VM. env.hcl is shared
		// configuration, not a unit, so it names sizes without adding a machine.
		//
		// Counts kali, which only deploys under --with-kali. Overstating by one
		// is the safe direction for a capacity warning; understating would let a
		// range through that does not fit.
		if name == "terragrunt.hcl" && unitSizeRE.Match(body) {
			out.Units++
		}
		return nil
	})
	if err != nil {
		return Requested{}, err
	}
	sort.Strings(out.Sizes)
	return out, nil
}
