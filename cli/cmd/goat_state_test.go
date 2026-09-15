package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testTerraformState = `{"version":4,"resources":[]}`

func writeTestState(t *testing.T, root string, mode os.FileMode) string {
	t.Helper()
	module := filepath.Join(root, "goat-dev", "centralus", "network")
	if err := os.MkdirAll(module, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(module, "terraform.tfstate")
	if err := os.WriteFile(path, []byte(testTerraformState), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPrepareGOATStateMigratesAndSecuresLegacyState(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()
	legacyRoot := checkoutGOATStateRoot(projectRoot)
	legacyState := writeTestState(t, legacyRoot, 0o644)
	if err := os.Chmod(filepath.Dir(legacyState), 0o755); err != nil {
		t.Fatal(err)
	}

	stableRoot, err := prepareGOATState(projectRoot, homeDir)
	if err != nil {
		t.Fatalf("prepareGOATState() error: %v", err)
	}
	if stableRoot != goatStateRoot(homeDir) {
		t.Fatalf("stable root = %q, want %q", stableRoot, goatStateRoot(homeDir))
	}
	if _, err := os.Stat(legacyRoot); !os.IsNotExist(err) {
		t.Fatalf("legacy state was not moved: %v", err)
	}
	stableState := filepath.Join(stableRoot, "goat-dev", "centralus", "network", "terraform.tfstate")
	for path, want := range map[string]os.FileMode{
		stableRoot:                0o700,
		filepath.Dir(stableState): 0o700,
		stableState:               0o600,
	} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat %s: %v", path, statErr)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("mode %s = %o, want %o", path, got, want)
		}
	}
}

func TestPrepareGOATStateRefusesTwoPopulatedRoots(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()
	legacyRoot := checkoutGOATStateRoot(projectRoot)
	stableRoot := goatStateRoot(homeDir)
	writeTestState(t, legacyRoot, 0o600)
	writeTestState(t, stableRoot, 0o600)

	_, err := prepareGOATState(projectRoot, homeDir)
	if err == nil || !strings.Contains(err.Error(), "exists in both") {
		t.Fatalf("expected split-state refusal, got %v", err)
	}
	if !hasTerraformState(legacyRoot) || !hasTerraformState(stableRoot) {
		t.Fatal("split-state refusal modified one of the roots")
	}
}

func TestPrepareGOATStateMigratesIntoEmptyDestination(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()
	legacyRoot := checkoutGOATStateRoot(projectRoot)
	writeTestState(t, legacyRoot, 0o644)
	if err := os.MkdirAll(goatStateRoot(homeDir), 0o755); err != nil {
		t.Fatal(err)
	}

	stableRoot, err := prepareGOATState(projectRoot, homeDir)
	if err != nil {
		t.Fatalf("prepareGOATState() error: %v", err)
	}
	if !hasTerraformState(stableRoot) {
		t.Fatal("legacy state was not migrated into empty destination")
	}
}

func TestPrepareGOATStateRejectsSymlinkedLegacyPath(t *testing.T) {
	projectRoot := t.TempDir()
	homeDir := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(projectRoot, ".dreadgoad")); err != nil {
		t.Fatal(err)
	}

	_, err := prepareGOATState(projectRoot, homeDir)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected legacy symlink refusal, got %v", err)
	}
}

func TestSecureGOATStateRejectsNestedSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "redirect")); err != nil {
		t.Fatal(err)
	}
	if err := secureGOATState(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected nested symlink refusal, got %v", err)
	}
}
