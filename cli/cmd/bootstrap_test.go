package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapInventory(t *testing.T) {
	t.Run("copies example when inventory missing", func(t *testing.T) {
		dir := t.TempDir()
		invPath := filepath.Join(dir, "dev-inventory")
		examplePath := invPath + ".example"

		exampleContent := []byte("[all:vars]\nenv=dev\nregion=us-west-2\n")
		if err := os.WriteFile(examplePath, exampleContent, 0o644); err != nil {
			t.Fatalf("write example: %v", err)
		}

		if err := bootstrapInventory(invPath); err != nil {
			t.Fatalf("bootstrapInventory() error: %v", err)
		}

		got, err := os.ReadFile(invPath)
		if err != nil {
			t.Fatalf("read bootstrapped inventory: %v", err)
		}
		if string(got) != string(exampleContent) {
			t.Errorf("content mismatch:\ngot:  %q\nwant: %q", got, exampleContent)
		}
	})

	t.Run("no-op when inventory exists", func(t *testing.T) {
		dir := t.TempDir()
		invPath := filepath.Join(dir, "dev-inventory")

		existing := []byte("[all:vars]\nenv=dev\ninstance=i-abc123\n")
		if err := os.WriteFile(invPath, existing, 0o644); err != nil {
			t.Fatalf("write existing: %v", err)
		}

		if err := bootstrapInventory(invPath); err != nil {
			t.Fatalf("bootstrapInventory() error: %v", err)
		}

		got, err := os.ReadFile(invPath)
		if err != nil {
			t.Fatalf("read inventory: %v", err)
		}
		if string(got) != string(existing) {
			t.Errorf("existing inventory was overwritten")
		}
	})

	t.Run("errors when neither file exists", func(t *testing.T) {
		dir := t.TempDir()
		invPath := filepath.Join(dir, "dev-inventory")

		err := bootstrapInventory(invPath)
		if err == nil {
			t.Fatal("expected error when no inventory or example exists")
		}
	})
}
