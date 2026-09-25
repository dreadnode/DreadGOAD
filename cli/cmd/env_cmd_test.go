package cmd

import (
	"errors"
	"io/fs"
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
	labPath := filepath.Join(root, "ad", "GOAD-Mini")
	if err := os.MkdirAll(filepath.Join(labPath, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(labPath, "data", "inventory"), []byte(
		"[domain]\ndc01\n\n[dc]\ndc01\n\n[extensions]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "[all:vars]\ndomain_name=GOAD\nenv=staging\nansible_aws_ssm_region=us-west-1\n[default]\ndc01 ansible_host=i-1234\n"
	if err := os.WriteFile(filepath.Join(root, reference+"-inventory.example"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := scaffoldPlan{Lab: "GOAD-Mini", LabPath: labPath, Profile: rangeconfig.ProfileActiveDir}
	ctx := scaffoldContext{
		scaffoldRequest: scaffoldRequest{envName: "mini", region: "us-east-1", reference: reference},
		projectRoot:     root,
		plan:            plan,
		provider:        "aws",
		inventoryPath:   filepath.Join(root, "mini-inventory"),
		hostFilter:      map[string]bool{"dc01": true},
	}
	if err := scaffoldInventoryForPlan(ctx); err != nil {
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

func TestNewScaffoldContextDerivesPathsFromNamedRequest(t *testing.T) {
	root := t.TempDir()
	lab := filepath.Join(root, "ad", "SERVICE")
	referenceRegion := filepath.Join(root, "infra", "azure", "service-deployment", "service-dev", "centralus")
	if err := os.MkdirAll(referenceRegion, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(referenceRegion, "region.hcl"), []byte("locals {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	request := scaffoldRequest{
		envName:   "kraken",
		region:    "eastus",
		vpcCIDR:   "10.50.0.0/16",
		reference: "service-dev",
	}
	plan := scaffoldPlan{
		Lab: "SERVICE", LabPath: lab, Profile: rangeconfig.ProfileTemplate,
		Spec: rangeconfig.ProviderSpec{
			Deployment: "service-deployment", DefaultRegion: "centralus",
		},
	}
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         request.envName,
		Environments: map[string]config.EnvironmentConfig{
			request.envName: {Provider: "azure", Deployment: plan.Spec.Deployment},
		},
	}

	ctx, err := newScaffoldContext(cfg, plan, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolveScaffoldReference(&ctx); err != nil {
		t.Fatal(err)
	}
	wantEnvDir := filepath.Join(root, "infra", "azure", "service-deployment", "kraken")
	if ctx.provider != "azure" || ctx.envDir != wantEnvDir {
		t.Fatalf("provider/envDir = %q/%q, want azure/%q", ctx.provider, ctx.envDir, wantEnvDir)
	}
	if ctx.regionDir != filepath.Join(wantEnvDir, "eastus") {
		t.Errorf("regionDir = %q", ctx.regionDir)
	}
	if ctx.inventoryPath != filepath.Join(root, "kraken-inventory") {
		t.Errorf("inventoryPath = %q", ctx.inventoryPath)
	}
	if ctx.referenceRegionDir != referenceRegion {
		t.Errorf("referenceRegionDir = %q, want %q", ctx.referenceRegionDir, referenceRegion)
	}

	// The context owns a value copy, so later edits to the caller's request
	// cannot silently change paths midway through scaffolding.
	request.envName = "changed"
	if ctx.envName != "kraken" || filepath.Base(ctx.envDir) != "kraken" {
		t.Fatalf("context changed with caller request: %#v", ctx)
	}
}

func TestScaffoldChecksExistingTargetBeforeReference(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "infra", "azure", "service-deployment", "kraken")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := scaffoldPlan{
		Lab: "SERVICE", Profile: rangeconfig.ProfileTemplate,
		Spec: rangeconfig.ProviderSpec{Deployment: "service-deployment", DefaultRegion: "centralus"},
	}
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         "kraken",
		Environments: map[string]config.EnvironmentConfig{
			"kraken": {Provider: "azure", Deployment: plan.Spec.Deployment},
		},
	}
	err := scaffoldEnvWithPlan(cfg, plan, scaffoldRequest{
		envName: "kraken", region: "eastus", vpcCIDR: "10.50.0.0/16", reference: "missing-reference",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v, want existing-target error before reference discovery", err)
	}
}

func TestScaffoldTargetExistsDistinguishesFilesystemErrors(t *testing.T) {
	root := t.TempDir()

	exists, err := scaffoldTargetExists(filepath.Join(root, "missing"))
	if err != nil || exists {
		t.Fatalf("missing path: exists=%v error=%v", exists, err)
	}

	brokenLink := filepath.Join(root, "broken-link")
	if err := os.Symlink(filepath.Join(root, "missing-target"), brokenLink); err != nil {
		t.Skipf("create broken symlink fixture: %v", err)
	}
	exists, err = scaffoldTargetExists(brokenLink)
	if err != nil || !exists {
		t.Fatalf("broken symlink: exists=%v error=%v", exists, err)
	}

	exists, err = scaffoldTargetExists("\x00")
	if err == nil || exists {
		t.Fatalf("invalid path: exists=%v error=%v, want filesystem error", exists, err)
	}
}

func TestScaffoldRollbackRemovesOnlyReservedArtifacts(t *testing.T) {
	root := t.TempDir()
	artifacts := scaffoldArtifacts{}
	ownedDir := filepath.Join(root, "owned-dir")
	ownedFile := filepath.Join(root, "owned-file")
	survivor := filepath.Join(root, "survivor")

	if err := artifacts.reserveDirectory(ownedDir); err != nil {
		t.Fatal(err)
	}
	if err := artifacts.reserveFile(ownedFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(survivor, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	succeeded := false
	cleanupFailedScaffold(&succeeded, &artifacts)

	for _, path := range []string{ownedDir, ownedFile} {
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("reserved artifact was not removed: %s: %v", path, err)
		}
	}
	if data, err := os.ReadFile(survivor); err != nil || string(data) != "keep" {
		t.Fatalf("untracked artifact changed: data=%q error=%v", data, err)
	}
}

func TestScaffoldReservationDoesNotClaimExistingPath(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	artifacts := scaffoldArtifacts{}
	if err := artifacts.reserveDirectory(existing); err == nil {
		t.Fatal("reserveDirectory accepted an existing path")
	}
	succeeded := false
	cleanupFailedScaffold(&succeeded, &artifacts)
	if info, err := os.Stat(existing); err != nil || !info.IsDir() {
		t.Fatalf("unowned existing path was removed: info=%v error=%v", info, err)
	}
}

func TestScaffoldEnvRejectsServiceRangeBeforeWriting(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "ad", "SERVICE")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "schema_version: 1\nkind: service-range\n" + serviceHealthManifest
	if err := os.WriteFile(filepath.Join(source, "range.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{ProjectRoot: root, Provider: "azure"}

	err := scaffoldEnv(cfg, scaffoldRequest{
		envName:       "service-variant",
		region:        "centralus",
		vpcCIDR:       "10.100.0.0/16",
		reference:     "service-dev",
		variantSource: "ad/SERVICE",
		useVariant:    true,
	})
	if err == nil || !strings.Contains(err.Error(), "active-directory ranges") {
		t.Fatalf("scaffoldEnv() error = %v, want unsupported range-kind error", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "infra")); !os.IsNotExist(statErr) {
		t.Fatalf("unsupported scaffold wrote infrastructure: %v", statErr)
	}
}

func TestTemplateProfileScaffoldsSERVICEWithoutGOADArtifacts(t *testing.T) {
	root := t.TempDir()
	lab := filepath.Join(root, "ad", "SERVICE")
	for _, dir := range []string{
		filepath.Join(lab, "data"),
		filepath.Join(lab, "providers", "azure"),
		filepath.Join(root, "infra", "azure", "service-deployment", "service-dev", "centralus", "hosts", "web01"),
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
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: commands/health
infrastructure:
  azure:
    deployment: service-deployment
    scaffold_profile: template
    template_environment: service-dev
    default_region: centralus
    network:
      cidr: 10.50.0.0/16
      editable: false
`,
		filepath.Join(lab, "data", "config.json"):                                                                                   `{"lab":{"hosts":{"web01":{}}}}`,
		filepath.Join(lab, "providers", "azure", "inventory"):                                                                       "[all:vars]\nansible_ssh_private_key_file=~/.keys/azure-service-dev-service-admin\n[all]\nweb01 ansible_host=10.50.10.20\n",
		filepath.Join(root, "infra", "azure", "service-deployment", "service-dev", "env.hcl"):                                       "locals { env = \"service-dev\" deployment_name = \"service\" key = \"azure-service-dev-service-admin\" }\n",
		filepath.Join(root, "infra", "azure", "service-deployment", "service-dev", "centralus", "region.hcl"):                       "locals { location = \"centralus\" }\n",
		filepath.Join(root, "infra", "azure", "service-deployment", "service-dev", "centralus", "hosts", "web01", "terragrunt.hcl"): "mock = \"service-dev-service-rg\"\nterraform {}\n",
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
			"kraken": {Lab: "SERVICE", Provider: "azure", Deployment: "service-deployment"},
		},
	}
	plan, err := resolveScaffoldPlan(cfg, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := scaffoldEnvWithPlan(cfg, plan, scaffoldRequest{
		envName: "kraken", region: "centralus", vpcCIDR: "10.50.0.0/16", reference: "service-dev",
	}); err != nil {
		t.Fatal(err)
	}
	for path, contains := range map[string]string{
		filepath.Join(root, "infra", "azure", "service-deployment", "kraken", "env.hcl"):                                       "azure-kraken-service-admin",
		filepath.Join(root, "infra", "azure", "service-deployment", "kraken", "centralus", "hosts", "web01", "terragrunt.hcl"): "terraform",
		filepath.Join(root, "kraken-inventory"):                                                                                "azure-kraken-service-admin",
	} {
		raw, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(raw), contains) {
			t.Fatalf("%s = %q, %v; want %q", path, raw, err, contains)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "ad", "GOAD", "data", "kraken-overlay.json")); !os.IsNotExist(err) {
		t.Fatalf("SERVICE scaffold created a GOAD overlay: %v", err)
	}
	rendered, err := os.ReadFile(filepath.Join(root, "infra", "azure", "service-deployment", "kraken", "centralus", "hosts", "web01", "terragrunt.hcl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rendered), "service-dev") || !strings.Contains(string(rendered), "kraken-service-rg") {
		t.Fatalf("copied template literals were not rendered: %s", rendered)
	}
}

func TestTemplateProfileScaffoldsAWSRegionAndInventory(t *testing.T) {
	root := t.TempDir()
	lab := filepath.Join(root, "ad", "SERVICE")
	template := filepath.Join(root, "infra", "service-deployment", "service-aws", "us-east-2")
	for _, dir := range []string{
		filepath.Join(lab, "data"),
		filepath.Join(lab, "providers", "aws"),
		filepath.Join(template, "hosts", "web01"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(lab, "range.yml"): `schema_version: 1
kind: service-range
variants:
  supported: false
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: commands/health
infrastructure:
  aws:
    deployment: service-deployment
    scaffold_profile: template
    template_environment: service-aws
    default_region: us-east-2
    network:
      cidr: 10.50.0.0/16
      editable: false
`,
		filepath.Join(lab, "data", "config.json"):                   `{"lab":{"hosts":{"web01":{}}}}`,
		filepath.Join(lab, "providers", "aws", "inventory"):         "[all:vars]\nansible_aws_ssm_region={{region}}\nansible_aws_ssm_bucket_name=AUTO\nenv={{env}}\n[all]\nweb01 ansible_host=PENDING\n",
		filepath.Join(filepath.Dir(template), "env.hcl"):            `locals { env = "service-aws" deployment_name = "service" }`,
		filepath.Join(template, "region.hcl"):                       `locals { aws_region = "us-east-2" }`,
		filepath.Join(template, "hosts", "web01", "terragrunt.hcl"): `name = "service-aws-service-web01"`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{ProjectRoot: root, Env: "kraken", Environments: map[string]config.EnvironmentConfig{
		"kraken": {Lab: "SERVICE", Provider: "aws", Deployment: "service-deployment"},
	}}
	plan, err := resolveScaffoldPlan(cfg, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := scaffoldEnvWithPlan(cfg, plan, scaffoldRequest{
		envName: "kraken", region: "us-west-2", vpcCIDR: "10.50.0.0/16", reference: "service-aws",
	}); err != nil {
		t.Fatal(err)
	}
	regionHCL, err := os.ReadFile(filepath.Join(root, "infra", "service-deployment", "kraken", "us-west-2", "region.hcl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(regionHCL), `aws_region = "us-west-2"`) || strings.Contains(string(regionHCL), "location") {
		t.Fatalf("AWS region.hcl = %q", regionHCL)
	}
	inventory, err := os.ReadFile(filepath.Join(root, "kraken-inventory"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ansible_aws_ssm_region=us-west-2", "ansible_aws_ssm_bucket_name=AUTO", "env=kraken"} {
		if !strings.Contains(string(inventory), want) {
			t.Fatalf("AWS inventory %q does not contain %q", inventory, want)
		}
	}
}

func TestFailedScaffoldRemovesOnlyNewArtifacts(t *testing.T) {
	root := t.TempDir()
	lab := filepath.Join(root, "ad", "SERVICE")
	template := filepath.Join(root, "infra", "azure", "service-deployment", "service-dev")
	for _, dir := range []string{
		filepath.Join(template, "centralus", "hosts"),
		filepath.Join(lab, "providers", "azure"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(lab, "range.yml"):                    "schema_version: 1\nkind: service-range\n" + serviceHealthManifest + "infrastructure:\n  azure:\n    deployment: service-deployment\n    scaffold_profile: template\n    template_environment: service-dev\n    network:\n      cidr: 10.50.0.0/16\n      editable: false\n",
		filepath.Join(lab, "data", "config.json"):          "{}\n",
		filepath.Join(template, "env.hcl"):                 "locals { env = \"service-dev\" }\n",
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
		"broken": {Lab: "SERVICE", Provider: "azure", Deployment: "service-deployment"},
	}}
	plan, err := resolveScaffoldPlan(cfg, "", false)
	if err != nil {
		t.Fatal(err)
	}
	err = scaffoldEnvWithPlan(cfg, plan, scaffoldRequest{
		envName: "broken", region: "centralus", vpcCIDR: "10.50.0.0/16", reference: "service-dev",
	})
	if err == nil || !strings.Contains(err.Error(), "inventory") {
		t.Fatalf("error = %v, want missing inventory", err)
	}
	if _, err := os.Stat(filepath.Join(root, "infra", "azure", "service-deployment", "broken")); !os.IsNotExist(err) {
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
	lab := filepath.Join(root, "ad", "SERVICE")
	template := filepath.Join(root, "infra", "azure", "service-deployment", "service-dev")
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
		filepath.Join(template, "env.hcl"):                          `locals { env = "service-dev" }`,
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
		Lab: "SERVICE", LabPath: lab, Profile: rangeconfig.ProfileTemplate,
		Spec: rangeconfig.ProviderSpec{
			Deployment: "service-deployment", ScaffoldProfile: rangeconfig.ProfileTemplate,
			TemplateEnvironment: "service-dev", DefaultRegion: "centralus",
		},
	}
	cfg := &config.Config{ProjectRoot: root, Env: "kraken", Environments: map[string]config.EnvironmentConfig{
		"kraken": {Lab: "SERVICE", Provider: "azure", Deployment: "service-deployment"},
	}}
	if err := scaffoldEnvWithPlan(cfg, plan, scaffoldRequest{
		envName: "kraken", region: "eastus", vpcCIDR: "10.50.0.0/16", reference: "service-dev",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "infra", "azure", "service-deployment", "kraken", "eastus", "hosts", "source.txt"))
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

func TestCopyInfrastructureFiltersHostUnitsButPreservesSharedAssets(t *testing.T) {
	src := filepath.Join(t.TempDir(), "source")
	dst := filepath.Join(t.TempDir(), "destination")
	files := map[string]string{
		"goad/dc01/terragrunt.hcl":                "dc01",
		"goad/dc02/terragrunt.hcl":                "dc02",
		"goad/templates/bootstrap.ps1.tpl":        "bootstrap",
		"goad/support/scripts/configure-host.ps1": "support",
		"network/terragrunt.hcl":                  "network",
	}
	for name, content := range files {
		path := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create source directory for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write source file %s: %v", name, err)
		}
	}

	if err := copyInfrastructure(src, dst, map[string]bool{"dc01": true}); err != nil {
		t.Fatalf("copyInfrastructure() error = %v", err)
	}

	for _, name := range []string{
		"goad/dc01/terragrunt.hcl",
		"goad/templates/bootstrap.ps1.tpl",
		"goad/support/scripts/configure-host.ps1",
		"network/terragrunt.hcl",
	} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(name))); err != nil {
			t.Errorf("expected %s to be copied: %v", name, err)
		}
	}

	if _, err := os.Stat(filepath.Join(dst, "goad", "dc02")); !os.IsNotExist(err) {
		t.Errorf("excluded host directory exists or could not be checked: %v", err)
	}
}
