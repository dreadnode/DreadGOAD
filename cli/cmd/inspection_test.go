package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
)

func TestInspectorForSelectedProfile(t *testing.T) {
	root := t.TempDir()
	const profile = "test-service"
	inspectionProfiles[profile] = goadInspector{}
	t.Cleanup(func() { delete(inspectionProfiles, profile) })
	writeInspectionManifest(t, root, "service", "schema_version: 1\nkind: service-range\ninspection:\n  profile: test-service\n")
	writeInspectionManifest(t, root, "ad", "schema_version: 1\nkind: active-directory\n")

	service, err := inspectorFor(&config.Config{ProjectRoot: root, Lab: "service"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := service.(goadInspector); !ok {
		t.Fatal("registered profile must select its inspector")
	}

	ad, err := inspectorFor(&config.Config{ProjectRoot: root, Lab: "ad"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ad.(goadInspector); !ok {
		t.Fatal("active-directory kind must retain the AD inspector")
	}
}

func TestInspectorForRejectsUnconfiguredServiceRange(t *testing.T) {
	root := t.TempDir()
	writeInspectionManifest(t, root, "service", "schema_version: 1\nkind: service-range\n")

	if _, err := inspectorFor(&config.Config{ProjectRoot: root, Lab: "service"}); err == nil {
		t.Fatal("service range without inspection.profile must fail")
	}
}

func TestInspectorForRejectsUnsupportedProfile(t *testing.T) {
	root := t.TempDir()
	writeInspectionManifest(t, root, "service", "schema_version: 1\nkind: service-range\ninspection:\n  profile: unknown\n")

	if _, err := inspectorFor(&config.Config{ProjectRoot: root, Lab: "service"}); err == nil {
		t.Fatal("unsupported inspection.profile must fail")
	}
}

func writeInspectionManifest(t *testing.T, root, lab, body string) {
	t.Helper()
	dir := filepath.Join(root, "ad", lab)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "range.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
