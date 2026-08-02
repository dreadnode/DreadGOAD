package scoreboard

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGenerateAnswerKeyMissingLab verifies that GenerateAnswerKey returns a
// clear error when the config has no top-level "lab" object.
func TestGenerateAnswerKeyMissingLab(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"not_lab": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := GenerateAnswerKey(cfg)
	if err == nil {
		t.Fatal("expected error for missing 'lab' key, got nil")
	}
}

// TestGenerateAnswerKeyEmptyLab verifies that an empty lab config produces
// a valid answer key with zero objectives rather than crashing.
func TestGenerateAnswerKeyEmptyLab(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "data", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`{"lab": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ak, err := GenerateAnswerKey(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ak.Objectives) != 0 {
		t.Errorf("expected 0 objectives for empty lab, got %d", len(ak.Objectives))
	}
	if ak.TotalObjectives != 0 {
		t.Errorf("expected TotalObjectives=0, got %d", ak.TotalObjectives)
	}
}
