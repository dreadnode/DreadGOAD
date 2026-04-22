package jsonmerge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDiffRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		target string
	}{
		{
			name:   "scalar change",
			base:   `{"a": 1, "b": 2}`,
			target: `{"a": 1, "b": 99}`,
		},
		{
			name:   "key removal",
			base:   `{"a": 1, "b": 2}`,
			target: `{"a": 1}`,
		},
		{
			name:   "key addition",
			base:   `{"a": 1}`,
			target: `{"a": 1, "b": 2}`,
		},
		{
			name:   "nested change",
			base:   `{"a": {"b": 1, "c": 2}}`,
			target: `{"a": {"b": 99, "c": 2}}`,
		},
		{
			name:   "array replacement",
			base:   `{"a": [1, 2]}`,
			target: `{"a": [3, 4, 5]}`,
		},
		{
			name:   "identical",
			base:   `{"a": 1}`,
			target: `{"a": 1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var base, target any
			if err := json.Unmarshal([]byte(tt.base), &base); err != nil {
				t.Fatalf("unmarshal base: %v", err)
			}
			if err := json.Unmarshal([]byte(tt.target), &target); err != nil {
				t.Fatalf("unmarshal target: %v", err)
			}

			patch := Diff(base, target)
			result := MergePatch(base, patch)

			gotJSON, _ := json.Marshal(result)
			wantJSON, _ := json.Marshal(target)
			if string(gotJSON) != string(wantJSON) {
				patchJSON, _ := json.Marshal(patch)
				t.Errorf("round-trip failed:\n  base:   %s\n  target: %s\n  patch:  %s\n  got:    %s", tt.base, tt.target, patchJSON, gotJSON)
			}
		})
	}
}

func TestDiffBytesRealConfigs(t *testing.T) {
	// This test validates that Diff + MergePatch round-trips against
	// the actual GOAD config files when they exist.
	projectRoot := findProjectRoot(t)
	if projectRoot == "" {
		t.Skip("project root not found")
	}

	goadData := filepath.Join(projectRoot, "ad", "GOAD", "data")
	baseConfig := filepath.Join(goadData, "config.json")
	if _, err := os.Stat(baseConfig); err != nil {
		t.Skipf("base config not found: %v", err)
	}

	base, err := os.ReadFile(baseConfig)
	if err != nil {
		t.Fatalf("read base: %v", err)
	}

	for _, env := range []string{"dev", "staging", "test"} {
		envConfig := filepath.Join(goadData, env+"-config.json")
		if _, err := os.Stat(envConfig); err != nil {
			continue
		}

		t.Run(env, func(t *testing.T) {
			target, err := os.ReadFile(envConfig)
			if err != nil {
				t.Fatalf("read %s-config.json: %v", env, err)
			}

			// Compute the overlay.
			overlay, err := DiffBytes(base, target)
			if err != nil {
				t.Fatalf("DiffBytes: %v", err)
			}

			// Apply overlay to base and compare with target.
			merged, err := MergePatchBytes(base, overlay)
			if err != nil {
				t.Fatalf("MergePatchBytes: %v", err)
			}

			// Compare as parsed JSON to ignore formatting differences.
			var mergedVal, targetVal any
			if err := json.Unmarshal(merged, &mergedVal); err != nil {
				t.Fatalf("unmarshal merged: %v", err)
			}
			if err := json.Unmarshal(target, &targetVal); err != nil {
				t.Fatalf("unmarshal target: %v", err)
			}

			mergedJSON, _ := json.Marshal(mergedVal)
			targetJSON, _ := json.Marshal(targetVal)
			if string(mergedJSON) != string(targetJSON) {
				t.Errorf("round-trip mismatch for %s-config.json\n  overlay size: %d bytes", env, len(overlay))
			} else {
				t.Logf("%s overlay: %d bytes (vs %d byte full config)", env, len(overlay), len(target))
			}
		})
	}
}

func findProjectRoot(t *testing.T) string {
	t.Helper()
	// Walk up from the test file to find the project root (contains go.mod).
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir) // go.mod is in cli/, project root is parent
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
