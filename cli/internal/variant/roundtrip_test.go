package variant

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// collectKeys walks a decoded JSON object and records every key path it reaches,
// skipping paths whose value is an empty string, slice, or map. Empty values are
// excluded because `omitempty` drops them on re-marshal, and absent is
// semantically identical to empty for every field in the lab schema.
func collectKeys(m map[string]any, prefix string, out map[string]bool) {
	for k, v := range m {
		path := prefix + "." + k
		switch val := v.(type) {
		case map[string]any:
			if len(val) == 0 {
				continue
			}
			out[path] = true
			collectKeys(val, path, out)
		case []any:
			if len(val) == 0 {
				continue
			}
			out[path] = true
		case string:
			if val == "" {
				continue
			}
			out[path] = true
		default:
			out[path] = true
		}
	}
}

// TestConfigRoundTripPreservesKeys guards against the generator silently
// dropping lab config keys. `variant generate` decodes config.json into
// LabConfig, mutates it, and re-encodes it, so any key the structs don't model
// is lost. That produced a real defect: GOAD-variant-1 was generated without
// the `vulns_adcs_templates` scoreboard annotation, and a generated SCCM variant
// would have lost its entire `sccm` block. Every lab config is checked so that
// adding a key to any config without adding a struct field fails here.
func TestConfigRoundTripPreservesKeys(t *testing.T) {
	configs, err := filepath.Glob("../../../ad/*/data/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) == 0 {
		t.Fatal("no lab configs found; check the glob path")
	}

	for _, path := range configs {
		lab := filepath.Base(filepath.Dir(filepath.Dir(path)))
		t.Run(lab, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			var before map[string]any
			if err := json.Unmarshal(raw, &before); err != nil {
				t.Fatalf("decode as generic map: %v", err)
			}

			var cfg LabConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				t.Fatalf("decode as LabConfig: %v", err)
			}
			encoded, err := json.Marshal(cfg)
			if err != nil {
				t.Fatalf("re-encode LabConfig: %v", err)
			}
			var after map[string]any
			if err := json.Unmarshal(encoded, &after); err != nil {
				t.Fatalf("decode round-tripped output: %v", err)
			}

			want := map[string]bool{}
			collectKeys(before, "", want)
			got := map[string]bool{}
			collectKeys(after, "", got)

			var lost []string
			for k := range want {
				if !got[k] {
					lost = append(lost, k)
				}
			}
			sort.Strings(lost)
			for _, k := range lost {
				t.Errorf("key dropped by LabConfig round-trip: %s", k)
			}
		})
	}
}
