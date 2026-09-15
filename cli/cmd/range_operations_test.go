package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
)

func TestOperationsForSelectedProfile(t *testing.T) {
	root := t.TempDir()
	const profile = "test-service"
	rangeOperationProfiles[profile] = rangeOperations{autoBootstrapAWSBackend: true}
	t.Cleanup(func() { delete(rangeOperationProfiles, profile) })
	writeOperationsManifest(t, root, "service", "schema_version: 1\nkind: service-range\noperations:\n  profile: test-service\n")

	operations, err := operationsFor(&config.Config{ProjectRoot: root, Lab: "service"})
	if err != nil {
		t.Fatal(err)
	}
	if !operations.autoBootstrapAWSBackend {
		t.Fatalf("registered capabilities were not selected: %#v", operations)
	}
}

func TestOperationsForActiveDirectoryDefaults(t *testing.T) {
	root := t.TempDir()
	writeOperationsManifest(t, root, "ad", "schema_version: 1\nkind: active-directory\n")

	operations, err := operationsFor(&config.Config{ProjectRoot: root, Lab: "ad"})
	if err != nil {
		t.Fatal(err)
	}
	if operations != (rangeOperations{}) {
		t.Fatalf("active-directory capabilities changed: %#v", operations)
	}
}

func TestOperationsForRejectsMissingAndUnsupportedProfiles(t *testing.T) {
	root := t.TempDir()
	writeOperationsManifest(t, root, "missing", "schema_version: 1\nkind: service-range\n")
	writeOperationsManifest(t, root, "unknown", "schema_version: 1\nkind: service-range\noperations:\n  profile: unknown\n")

	for _, lab := range []string{"missing", "unknown"} {
		if _, err := operationsFor(&config.Config{ProjectRoot: root, Lab: lab}); err == nil {
			t.Fatalf("operationsFor(%q) unexpectedly succeeded", lab)
		}
	}
}

func writeOperationsManifest(t *testing.T, root, lab, body string) {
	t.Helper()
	dir := filepath.Join(root, "ad", lab)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "range.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
