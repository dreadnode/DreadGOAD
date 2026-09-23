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
	if err := os.MkdirAll(filepath.Join(target, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "range.yml"), []byte(
		"schema_version: 1\nkind: active-directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "data", "inventory"), []byte(
		"[domain]\ndc01\n\n[dc]\ndc01\n\n[extensions]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         "kraken",
		Environments: map[string]config.EnvironmentConfig{
			"kraken": {
				Lab: "GOAD", Variant: true, VariantTarget: "ad/GOAD-kraken",
			},
		},
	}
	if err := os.WriteFile(cfg.InventoryPath(), []byte(
		"[default]\ndc01 ansible_host=10.68.1.4 ansible_password=live\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureInventoryTopology(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfg.InventoryPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[domain]", "[dc]", "[extensions]"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("variant inventory is missing %q: %s", want, raw)
		}
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
	canonicalBody := "[all:vars]\ndomain_name=GOAD-kraken\n\n" +
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

	if err := ensureInventoryTopologyFromSource(runtime, canonical); err != nil {
		t.Fatalf("repair topology: %v", err)
	}
	first, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	got := string(first)
	for _, want := range []string{
		"dc01 ansible_host=10.68.1.4 ansible_user=ansible ansible_password=live-secret",
		"srv02 ansible_host=10.68.1.7 ansible_user=ansible ansible_password=other-secret",
		"[domain]\ndc01\nsrv02",
		"[dc]\ndc01",
		"[server]\nsrv02",
		"[extensions]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("repaired inventory is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "dc03") {
		t.Errorf("repair added an undeployed host:\n%s", got)
	}
	if info, err := os.Stat(runtime); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("inventory mode changed: info=%v err=%v", info, err)
	}

	// A second preflight must be byte-for-byte idempotent.
	if err := ensureInventoryTopologyFromSource(runtime, canonical); err != nil {
		t.Fatalf("second repair: %v", err)
	}
	second, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != got {
		t.Errorf("idempotent repair changed inventory:\nfirst:\n%s\nsecond:\n%s", got, second)
	}
}

func TestScaffoldInventoryReconcilesTopologyFromVariant(t *testing.T) {
	root := t.TempDir()
	canonical := filepath.Join(root, "ad", "GOAD-kraken", "data", "inventory")
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte(
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
	for _, want := range []string{"domain_name=GOAD-kraken", "[domain]", "[dc]", "[server]", "[extensions]"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("scaffolded inventory is missing %q:\n%s", want, raw)
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
