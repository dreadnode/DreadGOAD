package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/rangeconfig"
)

// TestRepointInventoryDomainPointsAtTheVariant covers the failure where a
// variant environment provisions from the stock lab's assets: the inventory is
// built from a reference or the stock provider template, so it arrives naming
// the base lab, and playbooks resolve ad/{{ domain_name }}/scripts from it.
func TestRepointInventoryDomainPointsAtTheVariant(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			"existing value is replaced",
			"[all:vars]\ndomain_name=GOAD\nadmin_user=administrator\n",
			"domain_name=GOAD-redteam",
		},
		{
			// AWS references carry SSM settings the variant template lacks, so
			// only this one key may change.
			"ssm settings survive",
			"[all:vars]\ndomain_name=GOAD\nansible_aws_ssm_region=us-west-2\n",
			"ansible_aws_ssm_region=us-west-2",
		},
		{
			"missing value is inserted",
			"[all:vars]\nadmin_user=administrator\n",
			"domain_name=GOAD-redteam",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			inv := filepath.Join(root, "redteam-inventory")
			if err := os.WriteFile(inv, []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := repointInventoryDomain(root, "redteam", "ad/GOAD"); err != nil {
				t.Fatalf("repointInventoryDomain: %v", err)
			}
			got, err := os.ReadFile(inv)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), tt.want) {
				t.Errorf("inventory =\n%s\nwant it to contain %q", got, tt.want)
			}
			if strings.Contains(string(got), "domain_name=GOAD\n") {
				t.Errorf("base lab domain_name survived:\n%s", got)
			}
		})
	}
}

// The non-variant path must not touch the inventory at all: without a variant
// there is no ad/GOAD-<env>/ tree for domain_name to point at.
func TestScaffoldInventoryLeavesDomainAloneWithoutVariant(t *testing.T) {
	root := t.TempDir()
	body := "[all:vars]\ndomain_name=GOAD\n"
	inv := filepath.Join(root, "redteam-inventory")
	if err := os.WriteFile(inv, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Simulates scaffoldInventory's useVariant=false branch, which skips the
	// repoint entirely.
	got, err := os.ReadFile(inv)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("inventory changed without --variant:\n%s", got)
	}
}

func TestScaffoldInventoryRepointsNonGOADBaseLab(t *testing.T) {
	root := t.TempDir()
	reference := "staging"
	body := "[all:vars]\ndomain_name=GOAD\nenv=staging\nansible_aws_ssm_region=us-west-1\n[default]\ndc01 ansible_host=i-1234\n"
	if err := os.WriteFile(filepath.Join(root, reference+"-inventory.example"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := scaffoldPlan{Lab: "GOAD-Mini", Profile: rangeconfig.ProfileActiveDir}
	if err := scaffoldInventoryForPlan(
		"aws", root, plan, "mini", "us-east-1", reference, "", false,
		map[string]bool{"dc01": true},
	); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "mini-inventory"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{"domain_name=GOAD-Mini", "env=mini", "ansible_aws_ssm_region=us-east-1", "ansible_host=PENDING"} {
		if !strings.Contains(got, want) {
			t.Errorf("inventory does not contain %q:\n%s", want, got)
		}
	}
}

func TestVariantTargetForFollowsTheSource(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		// Default source keeps the historical ad/GOAD-<env> layout exactly.
		{"", "GOAD-redteam"},
		{"ad/GOAD", "GOAD-redteam"},
		// A non-default base lab must not land in a GOAD-named directory.
		{"ad/SCCM", "SCCM-redteam"},
		{"ad/GOAD-Light", "GOAD-Light-redteam"},
		{"/abs/path/to/NHA", "NHA-redteam"},
	}
	for _, tt := range tests {
		got := variantTargetFor("/repo", "redteam", tt.source)
		want := filepath.Join("/repo", "ad", tt.want)
		if got != want {
			t.Errorf("variantTargetFor(%q) = %q, want %q", tt.source, got, want)
		}
	}
}

func TestScaffoldEnvRejectsServiceRangeBeforeWriting(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "ad", "SCOPE-RANGE")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "schema_version: 1\nkind: service-range\n"
	if err := os.WriteFile(filepath.Join(source, "range.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{ProjectRoot: root, Provider: "azure"}

	err := scaffoldEnv(
		cfg,
		"scope-variant",
		"centralus",
		"10.100.0.0/16",
		"scope-dev",
		"ad/SCOPE-RANGE",
		true,
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "active-directory ranges") {
		t.Fatalf("scaffoldEnv() error = %v, want unsupported range-kind error", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "infra")); !os.IsNotExist(statErr) {
		t.Fatalf("unsupported scaffold wrote infrastructure: %v", statErr)
	}
}

func TestTemplateProfileScaffoldsScopeWithoutGOADArtifacts(t *testing.T) {
	root := t.TempDir()
	lab := filepath.Join(root, "ad", "SCOPE-RANGE")
	for _, dir := range []string{
		filepath.Join(lab, "data"),
		filepath.Join(lab, "providers", "azure"),
		filepath.Join(root, "infra", "azure", "scope-range-deployment", "scope-dev", "centralus", "hosts", "web01"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fixtures := map[string]string{
		filepath.Join(lab, "range.yml"): `schema_version: 1
kind: service-range
variants:
  supported: false
infrastructure:
  azure:
    deployment: scope-range-deployment
    scaffold_profile: template
    template_environment: scope-dev
    default_region: centralus
    network:
      cidr: 10.50.0.0/16
      editable: false
`,
		filepath.Join(lab, "data", "config.json"):                                                                                     `{"lab":{"hosts":{"web01":{}}}}`,
		filepath.Join(lab, "providers", "azure", "inventory"):                                                                         "[all:vars]\nansible_ssh_private_key_file=~/.keys/azure-scope-dev-key\n[all]\nweb01 ansible_host=10.50.10.20\n",
		filepath.Join(root, "infra", "azure", "scope-range-deployment", "scope-dev", "env.hcl"):                                       "locals { env = \"scope-dev\" key = \"azure-scope-dev-key\" }\n",
		filepath.Join(root, "infra", "azure", "scope-range-deployment", "scope-dev", "centralus", "region.hcl"):                       "locals { location = \"centralus\" }\n",
		filepath.Join(root, "infra", "azure", "scope-range-deployment", "scope-dev", "centralus", "hosts", "web01", "terragrunt.hcl"): "mock = \"scope-dev-centralus\"\nterraform {}\n",
	}
	for path, body := range fixtures {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         "kraken",
		Environments: map[string]config.EnvironmentConfig{
			"kraken": {Lab: "SCOPE-RANGE", Provider: "azure", Deployment: "scope-range-deployment"},
		},
	}
	plan, err := resolveScaffoldPlan(cfg, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := scaffoldEnvWithPlan(cfg, plan, "kraken", "centralus", "10.50.0.0/16", "scope-dev", "", false, false); err != nil {
		t.Fatal(err)
	}
	for path, contains := range map[string]string{
		filepath.Join(root, "infra", "azure", "scope-range-deployment", "kraken", "env.hcl"):                                       "azure-kraken-key",
		filepath.Join(root, "infra", "azure", "scope-range-deployment", "kraken", "centralus", "hosts", "web01", "terragrunt.hcl"): "terraform",
		filepath.Join(root, "kraken-inventory"): "azure-kraken-key",
	} {
		raw, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(raw), contains) {
			t.Fatalf("%s = %q, %v; want %q", path, raw, err, contains)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "ad", "GOAD", "data", "kraken-overlay.json")); !os.IsNotExist(err) {
		t.Fatalf("SCOPE scaffold created a GOAD overlay: %v", err)
	}
	rendered, err := os.ReadFile(filepath.Join(root, "infra", "azure", "scope-range-deployment", "kraken", "centralus", "hosts", "web01", "terragrunt.hcl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rendered), "scope-dev") || !strings.Contains(string(rendered), "kraken-centralus") {
		t.Fatalf("copied template literals were not rendered: %s", rendered)
	}
}

func TestFailedScaffoldRemovesOnlyNewArtifacts(t *testing.T) {
	root := t.TempDir()
	lab := filepath.Join(root, "ad", "SCOPE-RANGE")
	template := filepath.Join(root, "infra", "azure", "scope-range-deployment", "scope-dev")
	for _, dir := range []string{
		filepath.Join(template, "centralus", "hosts"),
		filepath.Join(lab, "providers", "azure"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(lab, "range.yml"):                    "schema_version: 1\nkind: service-range\ninfrastructure:\n  azure:\n    deployment: scope-range-deployment\n    scaffold_profile: template\n    template_environment: scope-dev\n    network:\n      cidr: 10.50.0.0/16\n      editable: false\n",
		filepath.Join(lab, "data", "config.json"):          "{}\n",
		filepath.Join(template, "env.hcl"):                 "locals { env = \"scope-dev\" }\n",
		filepath.Join(template, "centralus", "region.hcl"): "locals {}\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{ProjectRoot: root, Env: "broken", Environments: map[string]config.EnvironmentConfig{
		"broken": {Lab: "SCOPE-RANGE", Provider: "azure", Deployment: "scope-range-deployment"},
	}}
	plan, err := resolveScaffoldPlan(cfg, "", false)
	if err != nil {
		t.Fatal(err)
	}
	err = scaffoldEnvWithPlan(cfg, plan, "broken", "centralus", "10.50.0.0/16", "scope-dev", "", false, false)
	if err == nil || !strings.Contains(err.Error(), "inventory") {
		t.Fatalf("error = %v, want missing inventory", err)
	}
	if _, err := os.Stat(filepath.Join(root, "infra", "azure", "scope-range-deployment", "broken")); !os.IsNotExist(err) {
		t.Fatalf("failed scaffold left infra behind: %v", err)
	}
}

func TestRenderTemplateContentDoesNotReplaceReferenceSubstrings(t *testing.T) {
	got := renderTemplateContent(
		`env = "test" key = "azure-test-key" note = "latest" region = "centralus" explicit = "{{env}}/{{region}}"`,
		"test", "kraken", "centralus", "eastus",
	)
	for _, want := range []string{`env = "kraken"`, `azure-kraken-key`, `note = "latest"`, `region = "eastus"`, `explicit = "kraken/eastus"`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered content %q does not contain %q", got, want)
		}
	}
}

func TestScaffoldUsesManifestDefaultTemplateRegion(t *testing.T) {
	root := t.TempDir()
	lab := filepath.Join(root, "ad", "SCOPE-RANGE")
	template := filepath.Join(root, "infra", "azure", "scope-range-deployment", "scope-dev")
	for _, dir := range []string{
		filepath.Join(lab, "data"),
		filepath.Join(lab, "providers", "azure"),
		filepath.Join(template, "aaa-wrong", "hosts"),
		filepath.Join(template, "centralus", "hosts"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(lab, "data", "config.json"):                   `{"lab":{"hosts":{}}}`,
		filepath.Join(lab, "providers", "azure", "inventory"):       "[all:vars]\nenv={{env}}\n",
		filepath.Join(template, "env.hcl"):                          `locals { env = "scope-dev" }`,
		filepath.Join(template, "aaa-wrong", "region.hcl"):          `locals { source = "wrong" }`,
		filepath.Join(template, "aaa-wrong", "hosts", "source.txt"): "wrong",
		filepath.Join(template, "centralus", "region.hcl"):          `locals { source = "centralus" }`,
		filepath.Join(template, "centralus", "hosts", "source.txt"): "correct",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plan := scaffoldPlan{
		Lab: "SCOPE-RANGE", LabPath: lab, Profile: rangeconfig.ProfileTemplate,
		Spec: rangeconfig.ProviderSpec{
			Deployment: "scope-range-deployment", ScaffoldProfile: rangeconfig.ProfileTemplate,
			TemplateEnvironment: "scope-dev", DefaultRegion: "centralus",
		},
	}
	cfg := &config.Config{ProjectRoot: root, Env: "kraken", Environments: map[string]config.EnvironmentConfig{
		"kraken": {Lab: "SCOPE-RANGE", Provider: "azure", Deployment: "scope-range-deployment"},
	}}
	if err := scaffoldEnvWithPlan(cfg, plan, "kraken", "eastus", "10.50.0.0/16", "scope-dev", "", false, false); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "infra", "azure", "scope-range-deployment", "kraken", "eastus", "hosts", "source.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "correct" {
		t.Fatalf("scaffold copied %q; want declared centralus template", raw)
	}
}

func TestDeriveAzureSubnets(t *testing.T) {
	tests := []struct {
		name     string
		vnetCIDR string
		wantBast string
		wantCtrl string
		wantKali string
		wantErr  bool
	}{
		{"standard", "10.8.0.0/16", "10.8.2.0/26", "10.8.3.0/28", "10.8.4.0/28", false},
		{"different octet", "10.1.0.0/16", "10.1.2.0/26", "10.1.3.0/28", "10.1.4.0/28", false},
		{"high octet", "10.200.0.0/16", "10.200.2.0/26", "10.200.3.0/28", "10.200.4.0/28", false},
		{"not /16", "10.8.0.0/24", "", "", "", true},
		{"invalid CIDR", "not-a-cidr", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subnets, err := deriveAzureSubnets(tt.vnetCIDR)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if subnets.Bastion != tt.wantBast {
				t.Errorf("bastion = %q, want %q", subnets.Bastion, tt.wantBast)
			}
			if subnets.Controller != tt.wantCtrl {
				t.Errorf("controller = %q, want %q", subnets.Controller, tt.wantCtrl)
			}
			if subnets.Kali != tt.wantKali {
				t.Errorf("kali = %q, want %q", subnets.Kali, tt.wantKali)
			}
		})
	}
}

func TestCreateAzureEnvHCLSetsGOADInstanceSizes(t *testing.T) {
	envDir := t.TempDir()
	if err := createAzureEnvHCL(envDir, "memory-test", "10.8.0.0/16", nil); err != nil {
		t.Fatalf("createAzureEnvHCL() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(envDir, "env.hcl"))
	if err != nil {
		t.Fatalf("read env.hcl: %v", err)
	}

	for _, want := range []string{
		`goad_instance_sizes = {`,
		`dc01  = "Standard_D2s_v3"`,
		`dc02  = "Standard_D4s_v3"`,
		`dc03  = "Standard_D2s_v3"`,
		`srv02 = "Standard_D2s_v3"`,
		`srv03 = "Standard_D2s_v3"`,
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf("env.hcl missing %q", want)
		}
	}

	if got := strings.Count(string(content), `"Standard_D4s_v3"`); got != 1 {
		t.Errorf("D4s_v3 count = %d, want 1", got)
	}
}
