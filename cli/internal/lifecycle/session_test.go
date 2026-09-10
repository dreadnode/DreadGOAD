package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
)

const adManifest = `schema_version: 1
kind: active-directory
lifecycle:
  session_init:
    - action: generate_answer_key
`

func writeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunSessionInitGeneratesPrivateAnswerKey(t *testing.T) {
	root := t.TempDir()
	labDir := filepath.Join(root, "ad", "GOAD")
	writeFixture(t, filepath.Join(labDir, manifestName), adManifest)
	writeFixture(t, filepath.Join(labDir, "data", "config.json"), `{
  "lab": {
    "hosts": {"dc01": {"hostname": "dc01", "type": "dc"}},
    "domains": {"example.local": {"users": {}}}
  }
}`)
	cfg := &config.Config{ProjectRoot: root, Env: "dev", Lab: "GOAD"}
	outputDir := filepath.Join(root, "private", "artifacts")

	results, err := RunSessionInit(cfg, outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "completed" {
		t.Fatalf("results = %#v, want one completed action", results)
	}
	answerKey := filepath.Join(outputDir, "answer_key.json")
	if len(results[0].Artifacts) != 1 || results[0].Artifacts[0] != answerKey {
		t.Fatalf("artifacts = %#v, want %s", results[0].Artifacts, answerKey)
	}
	for path, wantMode := range map[string]os.FileMode{
		outputDir: 0o700,
		answerKey: 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != wantMode {
			t.Fatalf("%s mode = %o, want %o", path, got, wantMode)
		}
	}
}

func TestRunSessionInitIsNoopWithoutDeclaredActions(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "ad", "SCOPE-RANGE", manifestName), `schema_version: 1
kind: service-range
lifecycle:
  session_init: []
`)
	cfg := &config.Config{ProjectRoot: root, Env: "dev", Lab: "SCOPE-RANGE"}
	outputDir := filepath.Join(root, "must-not-exist")

	results, err := RunSessionInit(cfg, outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v, want no actions", results)
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("no-op initializer created artifact directory: %v", err)
	}
}

func TestRunSessionInitDefersUnscaffoldedVariant(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "ad", "GOAD", manifestName), adManifest)
	cfg := &config.Config{
		ProjectRoot: root,
		Env:         "new",
		Environments: map[string]config.EnvironmentConfig{
			"new": {
				Variant:       true,
				VariantSource: "ad/GOAD",
				VariantTarget: "ad/GOAD-new",
			},
		},
	}

	results, err := RunSessionInit(cfg, filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "pending" {
		t.Fatalf("results = %#v, want one pending action", results)
	}
	if _, err := os.Stat(filepath.Join(root, "artifacts")); !os.IsNotExist(err) {
		t.Fatalf("pending initializer created artifact directory: %v", err)
	}
}

func TestRunSessionInitRejectsUnknownActionWithoutExecutingIt(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "ad", "CUSTOM", manifestName), `schema_version: 1
kind: custom
lifecycle:
  session_init:
    - action: ../../evil.sh
`)
	cfg := &config.Config{ProjectRoot: root, Env: "dev", Lab: "CUSTOM"}
	outputDir := filepath.Join(root, "must-not-exist")

	results, err := RunSessionInit(cfg, outputDir)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error = %v, want unsupported action", err)
	}
	if results != nil {
		t.Fatalf("results = %#v, want nil before execution", results)
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("rejected action created output directory: %v", err)
	}
}

func TestManifestRejectsUnknownFields(t *testing.T) {
	_, err := decodeManifest([]byte(`schema_version: 1
kind: custom
command: ./run-me
`))
	if err == nil || !strings.Contains(err.Error(), "field command not found") {
		t.Fatalf("error = %v, want strict unknown-field rejection", err)
	}
}

func TestEveryShippedRangeHasAValidManifest(t *testing.T) {
	configs, err := filepath.Glob("../../../ad/*/data/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) == 0 {
		t.Fatal("no shipped range configs found")
	}
	for _, configPath := range configs {
		labDir := filepath.Dir(filepath.Dir(configPath))
		name := filepath.Base(labDir)
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(labDir, manifestName))
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := decodeManifest(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateActions(manifest.Lifecycle.SessionInit); err != nil {
				t.Fatal(err)
			}
		})
	}
}
