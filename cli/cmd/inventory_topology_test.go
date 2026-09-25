package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/rangeconfig"
)

func TestEnsureInventoryTopologyUsesConfiguredVariantTarget(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "ad", "GOAD")
	target := filepath.Join(root, "ad", "GOAD-kraken")
	writeInventoryTestFile(t, filepath.Join(base, "range.yml"),
		"schema_version: 1\nkind: active-directory\n", 0o644)
	writeInventoryTestFile(t, filepath.Join(target, "data", "inventory"),
		"[domain]\ndc01\n\n[dc]\ndc01\n\n[extensions]\n", 0o644)
	// The generated target is deliberately stale. Provider-owned identities
	// must come from the authored source and not from this target copy.
	writeInventoryTestFile(t, filepath.Join(target, "providers", "azure", "inventory"),
		"[all:vars]\nadmin_user=goadmin\n", 0o644)
	writeInventoryTestFile(t, filepath.Join(base, "providers", "azure", "inventory"),
		"[all:vars]\nadmin_user=administrator\n", 0o644)
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         "kraken",
		Environments: map[string]config.EnvironmentConfig{
			"kraken": {
				Lab: "GOAD", Provider: "azure", Variant: true, VariantTarget: "ad/GOAD-kraken",
			},
		},
	}
	writeInventoryTestFile(t, cfg.InventoryPath(),
		"[default]\ndc01 ansible_host=10.68.1.4 ansible_password=live\n\n"+
			"[all:vars]\nadmin_user=goadmin\n", 0o640)
	if err := ensureInventoryTopology(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfg.InventoryPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ansible_password=live", "admin_user=administrator", "[domain]", "[dc]", "[extensions]",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("variant inventory is missing %q: %s", want, raw)
		}
	}
	if strings.Contains(string(raw), "admin_user=goadmin") {
		t.Errorf("variant inventory retained stale provider identity: %s", raw)
	}
	if info, err := os.Stat(cfg.InventoryPath()); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("inventory mode changed: info=%v err=%v", info, err)
	}
}

func writeInventoryTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureProviderInventoryVariablesUsesAWSIdentity(t *testing.T) {
	root := t.TempDir()
	providerInventory := filepath.Join(root, "ad", "GOAD", "providers", "aws", "inventory")
	if err := os.MkdirAll(filepath.Dir(providerInventory), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(providerInventory, []byte(
		"[all:vars]\nadmin_user=goadmin\nforce_dns_server=no\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{ProjectRoot: root, Env: "range", Provider: "aws"}
	if err := os.WriteFile(cfg.InventoryPath(), []byte(
		"[all:vars]\nadmin_user=administrator\nforce_dns_server=yes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureProviderInventoryVariables(cfg); err != nil {
		t.Fatal(err)
	}
	got := readInventoryForTest(t, cfg.InventoryPath())
	if !strings.Contains(got, "admin_user=goadmin") {
		t.Errorf("AWS identity was not reconciled: %s", got)
	}
	if !strings.Contains(got, "force_dns_server=yes") {
		t.Errorf("non-provider-owned setting was replaced: %s", got)
	}
}

func TestEnsureProviderInventoryVariablesFallsBackToLabDefaults(t *testing.T) {
	root := t.TempDir()
	writeInventoryTestFile(t, filepath.Join(root, "ad", "GOAD-Light", "providers", "ludus", "inventory"),
		"[all:vars]\nansible_user=localuser\n", 0o644)
	writeInventoryTestFile(t, filepath.Join(root, "ad", "GOAD-Light", "data", "inventory"),
		"[all:vars]\nadmin_user=administrator\n", 0o644)
	cfg := &config.Config{ProjectRoot: root, Env: "range", Lab: "GOAD-Light", Provider: "ludus"}
	writeInventoryTestFile(t, cfg.InventoryPath(),
		"[all:vars]\nadmin_user=goadmin\nansible_user=live-user\n", 0o644)

	if err := ensureProviderInventoryVariables(cfg); err != nil {
		t.Fatal(err)
	}
	got := readInventoryForTest(t, cfg.InventoryPath())
	if !strings.Contains(got, "admin_user=administrator") {
		t.Errorf("lab-default identity was not reconciled: %s", got)
	}
	if !strings.Contains(got, "ansible_user=live-user") {
		t.Errorf("live connection identity was replaced: %s", got)
	}
}

func TestReplaceInventoryVariableEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		want        string
		wantChanged bool
	}{
		{
			name:        "missing all vars section",
			content:     "[default]\ndc01 ansible_host=10.0.0.1\n",
			want:        "[all:vars]\nadmin_user=administrator\n",
			wantChanged: true,
		},
		{
			name: "duplicate stale assignments",
			content: "[all:vars]\nadmin_user=goadmin\n" +
				"admin_user = stale\nforce_dns_server=yes\n",
			want:        "admin_user=administrator\nadmin_user=administrator\nforce_dns_server=yes",
			wantChanged: true,
		},
		{
			name:        "already correct is byte idempotent",
			content:     "[all:vars]\nadmin_user=administrator\nforce_dns_server=yes\n",
			want:        "[all:vars]\nadmin_user=administrator\nforce_dns_server=yes\n",
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := replaceInventoryVariable(tt.content, "admin_user", "admin_user=administrator")
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if tt.name == "already correct is byte idempotent" {
				if got != tt.want {
					t.Errorf("idempotent replacement changed bytes:\ngot:  %q\nwant: %q", got, tt.want)
				}
			} else if !strings.Contains(got, tt.want) {
				t.Errorf("replacement missing %q:\n%s", tt.want, got)
			}
		})
	}
}

func TestEnsureInventoryTopologySkipsServiceRangeWithoutCanonicalInventory(t *testing.T) {
	root := t.TempDir()
	lab := filepath.Join(root, "ad", "SERVICE")
	if err := os.MkdirAll(lab, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lab, "range.yml"), []byte(
		"schema_version: 1\n"+
			"kind: service-range\n"+
			"commands:\n"+
			"  health:\n"+
			"    protocol: health/v1\n"+
			"    handler:\n"+
			"      type: executable\n"+
			"      path: commands/health\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         "service",
		Environments: map[string]config.EnvironmentConfig{
			"service": {Lab: "SERVICE"},
		},
	}
	body := "[all]\nweb01 ansible_host=10.0.0.4\n"
	if err := os.WriteFile(cfg.InventoryPath(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureInventoryTopology(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfg.InventoryPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != body {
		t.Errorf("service inventory changed: %q", raw)
	}
}

func TestEnsureInventoryTopologyRepairsStaleReferenceInventory(t *testing.T) {
	runtime, canonical := staleInventoryFixture(t)

	if err := ensureInventoryTopologyFromSource(runtime, canonical); err != nil {
		t.Fatalf("repair topology: %v", err)
	}
	got := readInventoryForTest(t, runtime)
	assertRepairedStaleInventory(t, runtime, got)

	// A second preflight must be byte-for-byte idempotent.
	if err := ensureInventoryTopologyFromSource(runtime, canonical); err != nil {
		t.Fatalf("second repair: %v", err)
	}
	if second := readInventoryForTest(t, runtime); second != got {
		t.Errorf("idempotent repair changed inventory:\nfirst:\n%s\nsecond:\n%s", got, second)
	}
}

func staleInventoryFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	runtime := filepath.Join(root, "kraken-inventory")
	canonical := filepath.Join(root, "ad", "GOAD-kraken", "data", "inventory")
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		t.Fatal(err)
	}

	// This is the shape copied from the stale local test-inventory: connection
	// data is valid, but every topology section is absent.
	runtimeBody := "[default]\n" +
		"dc01 ansible_host=10.68.1.4 ansible_user=ansible ansible_password=live-secret\n" +
		"srv02 ansible_host=10.68.1.7 ansible_user=ansible ansible_password=other-secret\n\n" +
		"[all:vars]\ndomain_name=GOAD-kraken\nadmin_user=goadmin\n"
	canonicalBody := "[all:vars]\ndomain_name=canonical-must-not-win\n" +
		"force_dns_server=no\ndns_server=1.1.1.1\ndns_server_forwarder=1.1.1.1\n" +
		"ansible_user=vagrant\nansible_password=vagrant\n\n" +
		"[domain]\ndc01\nsrv02\ndc03\n\n" +
		"[dc]\ndc01\ndc03\n\n" +
		"[server]\nsrv02\n\n" +
		"[extensions]\n"
	if err := os.WriteFile(runtime, []byte(runtimeBody), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte(canonicalBody), 0o644); err != nil {
		t.Fatal(err)
	}
	return runtime, canonical
}

func readInventoryForTest(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func assertRepairedStaleInventory(t *testing.T, runtime, got string) {
	t.Helper()
	for _, want := range []string{
		"dc01 ansible_host=10.68.1.4 ansible_user=ansible ansible_password=live-secret",
		"srv02 ansible_host=10.68.1.7 ansible_user=ansible ansible_password=other-secret",
		"[domain]\ndc01\nsrv02",
		"[dc]\ndc01",
		"[server]\nsrv02",
		"[extensions]",
		"domain_name=GOAD-kraken",
		"force_dns_server=no",
		"dns_server=1.1.1.1",
		"dns_server_forwarder=1.1.1.1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("repaired inventory is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "dc03") {
		t.Errorf("repair added an undeployed host:\n%s", got)
	}
	if strings.Contains(got, "domain_name=canonical-must-not-win") {
		t.Errorf("repair replaced an existing runtime variable:\n%s", got)
	}
	if strings.Contains(got, "ansible_user=vagrant") || strings.Contains(got, "ansible_password=vagrant") {
		t.Errorf("repair copied canonical connection credentials:\n%s", got)
	}
	if info, err := os.Stat(runtime); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("inventory mode changed: info=%v err=%v", info, err)
	}
}

func TestScaffoldInventoryReconcilesTopologyFromVariant(t *testing.T) {
	root := t.TempDir()
	canonical := filepath.Join(root, "ad", "GOAD-kraken", "data", "inventory")
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte(
		"[all:vars]\nforce_dns_server=no\ndns_server=1.1.1.1\n"+
			"dns_server_forwarder=1.1.1.1\nansible_user=vagrant\n\n"+
			"[domain]\ndc01\nsrv02\n\n[dc]\ndc01\n\n[server]\nsrv02\n\n[extensions]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Azure's default reference is a local, ignored runtime artifact. This
	// deliberately recreates the stale test-inventory that caused Kraken's
	// network_setup.yml failure.
	if err := os.WriteFile(filepath.Join(root, "test-inventory"), []byte(
		"[default]\n"+
			"dc01 ansible_host=192.168.10.10 ansible_password=stock\n"+
			"srv02 ansible_host=192.168.10.22 ansible_password=stock\n\n"+
			"[all:vars]\nadmin_user=goadmin\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := scaffoldContext{
		scaffoldRequest: scaffoldRequest{
			envName: "kraken", reference: "test", useVariant: true,
		},
		projectRoot:   root,
		plan:          scaffoldPlan{Profile: rangeconfig.ProfileActiveDir, LabPath: filepath.Join(root, "ad", "GOAD")},
		provider:      "azure",
		inventoryPath: filepath.Join(root, "kraken-inventory"),
		hostFilter:    map[string]bool{"dc01": true, "srv02": true},
	}
	if err := scaffoldInventoryForPlan(ctx); err != nil {
		t.Fatalf("scaffold inventory: %v", err)
	}
	raw, err := os.ReadFile(ctx.inventoryPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"domain_name=GOAD-kraken", "force_dns_server=no", "dns_server=1.1.1.1",
		"dns_server_forwarder=1.1.1.1", "[domain]", "[dc]", "[server]", "[extensions]",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("scaffolded inventory is missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), "ansible_user=vagrant") {
		t.Errorf("scaffold copied canonical connection settings:\n%s", raw)
	}
}

func TestEnsureInventoryTopologyCreatesMissingAllVarsSection(t *testing.T) {
	root := t.TempDir()
	runtime := filepath.Join(root, "range-inventory")
	canonical := filepath.Join(root, "inventory")
	if err := os.WriteFile(runtime, []byte(
		"[default]\ndc01 ansible_host=10.0.0.1 ansible_password=live\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte(
		"[all:vars]\nforce_dns_server=no\n\n[domain]\ndc01\n\n[dc]\ndc01\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureInventoryTopologyFromSource(runtime, canonical); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ansible_password=live", "[all:vars]\nforce_dns_server=no"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("repaired inventory is missing %q:\n%s", want, raw)
		}
	}
}

func TestEnsureInventoryTopologyCompletesExistingGroups(t *testing.T) {
	root := t.TempDir()
	runtime := filepath.Join(root, "range-inventory")
	canonical := filepath.Join(root, "inventory")
	if err := os.WriteFile(runtime, []byte(
		"[default]\ndc01 ansible_host=10.0.0.1\nsrv02 ansible_host=10.0.0.2\n\n"+
			"[domain]\nsrv02\n\n[dc]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte(
		"[domain]\ndc01\nsrv02\n\n[dc]\ndc01\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureInventoryTopologyFromSource(runtime, canonical); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	_, sections := topologySections(string(raw))
	for group, want := range map[string][]string{
		"domain": {"srv02", "dc01"},
		"dc":     {"dc01"},
	} {
		got := sections[group].members
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("[%s] members = %v, want %v; inventory:\n%s", group, got, want, raw)
		}
	}
}

func TestEnsureInventoryTopologyRejectsRangeWithoutDeployedDC(t *testing.T) {
	root := t.TempDir()
	runtime := filepath.Join(root, "range-inventory")
	canonical := filepath.Join(root, "inventory")
	if err := os.WriteFile(runtime, []byte("[default]\nsrv02 ansible_host=10.0.0.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte("[domain]\ndc01\nsrv02\n[dc]\ndc01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ensureInventoryTopologyFromSource(runtime, canonical)
	if err == nil || !strings.Contains(err.Error(), "no deployed host") {
		t.Fatalf("inventory without a DC was not rejected: %v", err)
	}
}

func TestEnsureInventoryTopologyLeavesNonADInventoryAlone(t *testing.T) {
	root := t.TempDir()
	runtime := filepath.Join(root, "service-inventory")
	canonical := filepath.Join(root, "inventory")
	body := "[all]\nweb01 ansible_host=10.0.0.4\n"
	if err := os.WriteFile(runtime, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte("[web]\nweb01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureInventoryTopologyFromSource(runtime, canonical); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != body {
		t.Errorf("non-AD inventory changed: %q", raw)
	}
}

func TestEnsureInventoryTopologyRejectsMissingCanonicalInventory(t *testing.T) {
	root := t.TempDir()
	runtime := filepath.Join(root, "range-inventory")
	if err := os.WriteFile(runtime, []byte("[default]\ndc01 ansible_host=10.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ensureInventoryTopologyFromSource(runtime, filepath.Join(root, "missing-inventory"))
	if err == nil || !strings.Contains(err.Error(), "canonical AD inventory topology is missing") {
		t.Fatalf("missing AD topology source was not rejected: %v", err)
	}
}
