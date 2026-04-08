package lab

import (
	"os"
	"path/filepath"
	"testing"
)

// buildFakeProject creates a minimal project structure for testing.
func buildFakeProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	adDir := filepath.Join(root, "ad")

	// GOAD lab with two providers and a config.json
	goadDir := filepath.Join(adDir, "GOAD")
	for _, sub := range []string{
		filepath.Join("providers", "aws"),
		filepath.Join("providers", "azure"),
		"data",
	} {
		if err := os.MkdirAll(filepath.Join(goadDir, sub), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	configJSON := `{"lab":{"hosts":{"DC01":{},"DC02":{}}}}`
	if err := os.WriteFile(
		filepath.Join(goadDir, "data", "config.json"),
		[]byte(configJSON), 0o644,
	); err != nil {
		t.Fatalf("WriteFile config.json: %v", err)
	}

	// MINI lab with no providers and no config.json
	miniDir := filepath.Join(adDir, "MINI")
	if err := os.MkdirAll(miniDir, 0o755); err != nil {
		t.Fatalf("MkdirAll MINI: %v", err)
	}

	// TEMPLATE should be excluded
	tmplDir := filepath.Join(adDir, "TEMPLATE")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatalf("MkdirAll TEMPLATE: %v", err)
	}

	// variant directory should be excluded
	varDir := filepath.Join(adDir, "GOAD-variant-small")
	if err := os.MkdirAll(varDir, 0o755); err != nil {
		t.Fatalf("MkdirAll variant: %v", err)
	}

	return root
}

func TestDiscoverLabs_Basic(t *testing.T) {
	root := buildFakeProject(t)
	labs, err := DiscoverLabs(root)
	if err != nil {
		t.Fatalf("DiscoverLabs: %v", err)
	}

	// Should find GOAD and MINI, not TEMPLATE or variant.
	if len(labs) != 2 {
		t.Fatalf("expected 2 labs, got %d: %v", len(labs), labs)
	}

	names := make(map[string]bool)
	for _, l := range labs {
		names[l.Name] = true
	}
	if !names["GOAD"] {
		t.Error("expected GOAD in discovered labs")
	}
	if !names["MINI"] {
		t.Error("expected MINI in discovered labs")
	}
	if names["TEMPLATE"] {
		t.Error("TEMPLATE should be excluded")
	}
	if names["GOAD-variant-small"] {
		t.Error("variant directory should be excluded")
	}
}

func TestDiscoverLabs_Providers(t *testing.T) {
	root := buildFakeProject(t)
	labs, err := DiscoverLabs(root)
	if err != nil {
		t.Fatalf("DiscoverLabs: %v", err)
	}

	var goad *Lab
	for i := range labs {
		if labs[i].Name == "GOAD" {
			goad = &labs[i]
		}
	}
	if goad == nil {
		t.Fatal("GOAD lab not found")
	}
	if len(goad.Providers) != 2 {
		t.Errorf("expected 2 providers, got %d: %v", len(goad.Providers), goad.Providers)
	}
}

func TestDiscoverLabs_Hosts(t *testing.T) {
	root := buildFakeProject(t)
	labs, err := DiscoverLabs(root)
	if err != nil {
		t.Fatalf("DiscoverLabs: %v", err)
	}

	var goad *Lab
	for i := range labs {
		if labs[i].Name == "GOAD" {
			goad = &labs[i]
		}
	}
	if goad == nil {
		t.Fatal("GOAD lab not found")
	}
	if len(goad.Hosts) != 2 {
		t.Errorf("expected 2 hosts, got %d: %v", len(goad.Hosts), goad.Hosts)
	}
}

func TestDiscoverLabs_MissingAdDir(t *testing.T) {
	root := t.TempDir()
	_, err := DiscoverLabs(root)
	if err == nil {
		t.Fatal("expected error for missing ad/ directory, got nil")
	}
}

func TestDiscoverLabs_Sorted(t *testing.T) {
	root := buildFakeProject(t)
	labs, err := DiscoverLabs(root)
	if err != nil {
		t.Fatalf("DiscoverLabs: %v", err)
	}
	for i := 1; i < len(labs); i++ {
		if labs[i].Name < labs[i-1].Name {
			t.Errorf("labs not sorted: %q before %q", labs[i-1].Name, labs[i].Name)
		}
	}
}

func TestLoadPlaybookConfig_Valid(t *testing.T) {
	root := t.TempDir()
	yml := "default:\n  - site.yml\n  - dc.yml\nGOAD:\n  - goad.yml\n"
	if err := os.WriteFile(filepath.Join(root, "playbooks.yml"), []byte(yml), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadPlaybookConfig(root)
	if err != nil {
		t.Fatalf("LoadPlaybookConfig: %v", err)
	}
	if len(cfg["default"]) != 2 {
		t.Errorf("expected 2 default playbooks, got %d", len(cfg["default"]))
	}
	if len(cfg["GOAD"]) != 1 {
		t.Errorf("expected 1 GOAD playbook, got %d", len(cfg["GOAD"]))
	}
}

func TestLoadPlaybookConfig_Missing(t *testing.T) {
	root := t.TempDir()
	_, err := LoadPlaybookConfig(root)
	if err == nil {
		t.Fatal("expected error for missing playbooks.yml, got nil")
	}
}

func TestPlaybooksForLab(t *testing.T) {
	root := t.TempDir()
	yml := "default:\n  - site.yml\nGOAD:\n  - goad.yml\n"
	if err := os.WriteFile(filepath.Join(root, "playbooks.yml"), []byte(yml), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tests := []struct {
		name    string
		labName string
		want    []string
	}{
		{
			name:    "lab-specific entry",
			labName: "GOAD",
			want:    []string{"goad.yml"},
		},
		{
			name:    "falls back to default",
			labName: "UNKNOWN",
			want:    []string{"site.yml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlaybooksForLab(root, tt.labName, nil)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d]=%q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPlaybooksForLab_MissingFile(t *testing.T) {
	root := t.TempDir()
	fallback := []string{"fallback.yml"}
	got := PlaybooksForLab(root, "GOAD", fallback)
	if len(got) != 1 || got[0] != "fallback.yml" {
		t.Errorf("expected fallback, got %v", got)
	}
}
