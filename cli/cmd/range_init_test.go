package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/variant"
)

func TestLoadRangeCapabilitiesIncludesAgentPrompt(t *testing.T) {
	root := t.TempDir()
	labDir := filepath.Join(root, "ad", "EXAMPLE")
	if err := os.MkdirAll(filepath.Join(labDir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "schema_version: 1\nkind: active-directory\nagent:\n  prompt: prompts/agent.md\n"
	if err := os.WriteFile(filepath.Join(labDir, "range.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(labDir, "prompts", "agent.md"),
		[]byte("Use example-range terminology."),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	payload, err := loadRangeCapabilities(&config.Config{
		ProjectRoot: root,
		Lab:         "EXAMPLE",
		Env:         "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.Range != "EXAMPLE" || payload.AgentPrompt != "Use example-range terminology." {
		t.Fatalf("payload = %#v", payload)
	}
	if len(payload.Commands) != 5 {
		t.Fatalf("commands = %d, want all five semantic capabilities", len(payload.Commands))
	}
}

func TestLoadRangeCapabilitiesUsesCompletedVariantPrompt(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "ad", "EXAMPLE")
	target := filepath.Join(root, "ad", "EXAMPLE-random")
	manifest := "schema_version: 1\nkind: active-directory\nagent:\n  prompt: prompts/agent.md\n"
	for _, fixture := range []struct {
		dir, prompt string
	}{{source, "Source guidance."}, {target, "Target guidance."}} {
		if err := os.MkdirAll(filepath.Join(fixture.dir, "prompts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fixture.dir, "range.yml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fixture.dir, "prompts", "agent.md"), []byte(fixture.prompt), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(target, variant.CompletionMarkerName), []byte("complete\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}

	payload, err := loadRangeCapabilities(&config.Config{
		ProjectRoot: root, Lab: "EXAMPLE", Env: "random",
		Environments: map[string]config.EnvironmentConfig{
			"random": {
				Lab: "EXAMPLE", Variant: true,
				VariantSource: "ad/EXAMPLE", VariantTarget: "ad/EXAMPLE-random",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.AgentPrompt != "Target guidance." {
		t.Fatalf("agent prompt = %q", payload.AgentPrompt)
	}
}
