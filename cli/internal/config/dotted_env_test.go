package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// loadConfigFile drives the real loader path: viper reads the file, unmarshals,
// and the repair runs. It bypasses Get()'s sync.Once so each case is isolated.
func loadConfigFile(t *testing.T, body string) *Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dreadgoad.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(path)
	setDefaults()
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	c := &Config{}
	if err := viper.Unmarshal(c); err != nil {
		t.Fatal(err)
	}
	repairDottedEnvironmentKeys(c)
	return c
}

// The exact shape the console wrote for the failing range. Before the repair,
// viper stored this as nested keys "3" -> "1" and the lookup missed, so a
// variant environment resolved to the stock lab tree with no error anywhere.
func TestDottedEnvironmentNameResolves(t *testing.T) {
	c := loadConfigFile(t, `
provider: azure
environments:
  '3.1':
    variant: true
    variant_source: ad/GOAD
    variant_target: ad/GOAD-3.1
    variant_name: '3.1'
    vpc_cidr: 10.100.0.0/16
`)
	c.Env = "3.1"
	ec := c.ActiveEnvironment()
	if !ec.Variant {
		t.Fatal("variant is false for '3.1' — the whole variant pipeline would use the stock lab")
	}
	if ec.VariantTarget != "ad/GOAD-3.1" {
		t.Errorf("variant_target = %q, want ad/GOAD-3.1", ec.VariantTarget)
	}
	if ec.VpcCidr != "10.100.0.0/16" {
		t.Errorf("vpc_cidr = %q — non-variant fields must survive too", ec.VpcCidr)
	}
}

// The mechanism that actually broke the range: labConfigDataDir consults
// ActiveEnvironment().Variant, so a mangled key sent it to the stock ad/GOAD
// tree. materializeLabConfig then copied the stock config to the path
// terragrunt reads, and the machines were built with stock passwords the
// inventory did not have.
func TestDottedEnvResolvesToTheVariantLabConfigDir(t *testing.T) {
	c := loadConfigFile(t, `
environments:
  '3.1':
    variant: true
    variant_source: ad/GOAD
    variant_target: ad/GOAD-3.1
`)
	root := t.TempDir()
	c.ProjectRoot = root
	c.Env = "3.1"
	variantData := filepath.Join(root, "ad", "GOAD-3.1", "data")
	if err := os.MkdirAll(variantData, 0o755); err != nil {
		t.Fatal(err)
	}

	got := c.labConfigDataDir()
	if got != variantData {
		t.Fatalf("labConfigDataDir() = %q, want the variant tree %q\n"+
			"this is the exact step that shipped stock passwords to the VMs", got, variantData)
	}
}

// The other real failing env: a dot plus a trailing capital, which also
// exercises the fact that viper lowercases keys.
func TestDottedEnvironmentNameWithSuffixResolves(t *testing.T) {
	c := loadConfigFile(t, `
environments:
  'dg-test-2.A':
    variant: true
    variant_target: ad/GOAD-dg-test-2.A
    region: eastus
`)
	c.Env = "dg-test-2.A"
	ec := c.ActiveEnvironment()
	if !ec.Variant || ec.VariantTarget != "ad/GOAD-dg-test-2.A" {
		t.Fatalf("dg-test-2.A did not resolve: %+v", ec)
	}
	if ec.Region != "eastus" {
		t.Errorf("region = %q, want eastus", ec.Region)
	}
}

// Splitting "3.1" does not merely lose the name, it invents "3" as an
// environment of its own. Left in place it shows up in `config show` as a range
// nobody created, and `--env 3` would resolve it to an all-defaults environment
// — the same silent wrong-config failure the repair exists to stop.
func TestViperKeyFragmentIsRemoved(t *testing.T) {
	c := loadConfigFile(t, `
environments:
  '3.1':
    variant: true
    variant_target: ad/GOAD-3.1
`)
	if _, ok := c.Environments["3"]; ok {
		t.Errorf("the fragment \"3\" survived: %v", envNames(c.Environments))
	}
	if _, ok := c.Environments["3.1"]; !ok {
		t.Error("the real environment was removed along with the fragment")
	}
}

// A fragment that the file actually defines is a real environment and must
// survive, even though it is also the prefix of a dotted sibling.
func TestRealEnvironmentSharingAFragmentNameSurvives(t *testing.T) {
	c := loadConfigFile(t, `
environments:
  '3':
    variant: true
    variant_target: ad/GOAD-3
  '3.1':
    variant: true
    variant_target: ad/GOAD-3.1
`)
	three, ok := c.Environments["3"]
	if !ok {
		t.Fatal("a real environment named \"3\" was deleted as a fragment")
	}
	if three.VariantTarget != "ad/GOAD-3" {
		t.Errorf("environment \"3\" = %+v, want its own variant_target", three)
	}
	if c.Environments["3.1"].VariantTarget != "ad/GOAD-3.1" {
		t.Error("the dotted sibling was clobbered")
	}
}

func envNames(m map[string]EnvironmentConfig) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Undotted names went through viper correctly before; they must keep working.
func TestUndottedEnvironmentNameStillResolves(t *testing.T) {
	c := loadConfigFile(t, `
environments:
  dreadindex:
    variant: true
    variant_target: ad/GOAD-dreadindex
    region: eastus
`)
	c.Env = "dreadindex"
	ec := c.ActiveEnvironment()
	if !ec.Variant || ec.VariantTarget != "ad/GOAD-dreadindex" || ec.Region != "eastus" {
		t.Fatalf("dreadindex regressed: %+v", ec)
	}
}

// The repair replaces the environments map wholesale, so the built-in
// dev/staging/prod defaults must survive a file that names none of them.
func TestBuiltInEnvironmentDefaultsSurviveTheRepair(t *testing.T) {
	c := loadConfigFile(t, `
environments:
  '3.1':
    variant: true
    variant_target: ad/GOAD-3.1
`)
	for _, name := range []string{"dev", "staging", "prod"} {
		if _, ok := c.Environments[name]; !ok {
			t.Errorf("built-in environment %q was dropped by the repair", name)
		}
	}
	if _, ok := c.Environments["3.1"]; !ok {
		t.Error("the file's own environment is missing")
	}
}

// A file entry must win over a same-named default rather than merging into it.
func TestFileEnvironmentOverridesDefault(t *testing.T) {
	c := loadConfigFile(t, `
environments:
  staging:
    variant: true
    variant_target: ad/GOAD-custom
`)
	c.Env = "staging"
	ec := c.ActiveEnvironment()
	if !ec.Variant || ec.VariantTarget != "ad/GOAD-custom" {
		t.Fatalf("file did not override the built-in staging default: %+v", ec)
	}
}

// The delimiter change rejected in favour of this repair would have flattened
// extensions.* into unreachable keys. Guard that they still load.
func TestExtensionDefaultsAreUnaffected(t *testing.T) {
	c := loadConfigFile(t, `
environments:
  '3.1':
    variant: true
`)
	if len(c.Extensions) == 0 {
		t.Fatal("extension defaults vanished — the nested extensions.* keys broke")
	}
	elk, ok := c.Extensions["elk"]
	if !ok || elk.Playbook == "" {
		t.Errorf("elk extension did not load: %+v", c.Extensions)
	}
}

// A config with no environments key must leave viper's result alone.
func TestRepairIsANoOpWithoutEnvironments(t *testing.T) {
	c := loadConfigFile(t, "provider: azure\n")
	if _, ok := c.Environments["staging"]; !ok {
		t.Error("the repair discarded the defaults when the file had no environments")
	}
}

// Unparsable YAML must not wipe the map. Losing every environment silently is
// worse than keeping whatever viper managed to read.
func TestRepairKeepsViperResultOnUnparsableFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	dir := t.TempDir()
	path := filepath.Join(dir, "dreadgoad.yaml")
	if err := os.WriteFile(path, []byte("environments:\n  dev:\n    variant: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	c := &Config{Environments: map[string]EnvironmentConfig{"kept": {Variant: true}}}

	// Corrupt the file after viper read it, then repair.
	if err := os.WriteFile(path, []byte("environments: [oh no\n  - :"), 0o644); err != nil {
		t.Fatal(err)
	}
	repairDottedEnvironmentKeys(c)

	if _, ok := c.Environments["kept"]; !ok {
		t.Error("a corrupt file wiped environments that were already resolved")
	}
}

// A deleted config file must not panic or clear the map.
func TestRepairSurvivesAMissingFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(filepath.Join(t.TempDir(), "gone.yaml"))
	c := &Config{Environments: map[string]EnvironmentConfig{"kept": {Variant: true}}}
	repairDottedEnvironmentKeys(c)
	if _, ok := c.Environments["kept"]; !ok {
		t.Error("a missing file cleared the environments map")
	}
}

// With no config file at all, ConfigFileUsed() is empty and the repair must
// return before touching anything.
func TestRepairIsANoOpWithNoConfigFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	c := &Config{Environments: map[string]EnvironmentConfig{"kept": {Variant: true}}}
	repairDottedEnvironmentKeys(c)
	if len(c.Environments) != 1 {
		t.Errorf("environments changed with no config file: %+v", c.Environments)
	}
}
