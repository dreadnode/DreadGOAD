package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
)

func TestLoadLabUsesActiveLabDirectory(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "ad", "SCOPE-RANGE", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := `{"lab":{"hosts":{"kali01":{"hostname":"kali01","domain":"range.test","type":"server","os":"linux"}}}}`
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{ProjectRoot: root, Env: "scope-dev", Lab: "SCOPE-RANGE"}
	lab, err := loadLab(cfg)
	if err != nil {
		t.Fatalf("loadLab() error: %v", err)
	}
	if got := lab.Hostname("kali01"); got != "kali01" {
		t.Fatalf("Hostname(kali01) = %q, want kali01", got)
	}
}
