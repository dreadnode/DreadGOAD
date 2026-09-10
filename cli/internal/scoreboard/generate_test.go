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

// TestGenerateAnswerKeyRejectsNonADLab verifies that service ranges cannot be
// mislabeled as GOAD answer keys merely because they also define hosts.
func TestGenerateAnswerKeyRejectsNonADLab(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "data", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`{"lab":{"hosts":{"web01":{"hostname":"web01"}},"domains":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateAnswerKey(cfg); err == nil {
		t.Fatal("expected non-AD lab to be rejected")
	}
}

func TestWriteAnswerKeyIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answer_key.json")
	if err := WriteAnswerKey(&AnswerKey{Version: "2.0"}, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("answer key mode = %o, want 600", got)
	}
}
