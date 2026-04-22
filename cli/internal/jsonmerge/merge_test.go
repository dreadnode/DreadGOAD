package jsonmerge

import (
	"encoding/json"
	"testing"
)

func TestMergePatch(t *testing.T) {
	tests := []struct {
		name  string
		base  string
		patch string
		want  string
	}{
		{
			name:  "scalar replacement",
			base:  `{"a": 1}`,
			patch: `{"a": 2}`,
			want:  `{"a": 2}`,
		},
		{
			name:  "add key",
			base:  `{"a": 1}`,
			patch: `{"b": 2}`,
			want:  `{"a": 1, "b": 2}`,
		},
		{
			name:  "delete key with null",
			base:  `{"a": 1, "b": 2}`,
			patch: `{"b": null}`,
			want:  `{"a": 1}`,
		},
		{
			name:  "array replacement",
			base:  `{"a": [1, 2, 3]}`,
			patch: `{"a": [4, 5]}`,
			want:  `{"a": [4, 5]}`,
		},
		{
			name:  "nested merge",
			base:  `{"a": {"b": 1, "c": 2}}`,
			patch: `{"a": {"b": 3}}`,
			want:  `{"a": {"b": 3, "c": 2}}`,
		},
		{
			name:  "nested delete",
			base:  `{"a": {"b": 1, "c": 2}}`,
			patch: `{"a": {"c": null}}`,
			want:  `{"a": {"b": 1}}`,
		},
		{
			name:  "replace object with scalar",
			base:  `{"a": {"b": 1}}`,
			patch: `{"a": "hello"}`,
			want:  `{"a": "hello"}`,
		},
		{
			name:  "replace scalar with object",
			base:  `{"a": "hello"}`,
			patch: `{"a": {"b": 1}}`,
			want:  `{"a": {"b": 1}}`,
		},
		{
			name:  "empty patch is noop",
			base:  `{"a": 1}`,
			patch: `{}`,
			want:  `{"a": 1}`,
		},
		{
			name:  "patch non-object base",
			base:  `"hello"`,
			patch: `{"a": 1}`,
			want:  `{"a": 1}`,
		},
		{
			name:  "deep nested merge",
			base:  `{"a": {"b": {"c": 1, "d": 2}, "e": 3}}`,
			patch: `{"a": {"b": {"c": 99}}}`,
			want:  `{"a": {"b": {"c": 99, "d": 2}, "e": 3}}`,
		},
		{
			name:  "replace nested object with empty object",
			base:  `{"a": {"b": {"c": 1}}}`,
			patch: `{"a": {"b": {}}}`,
			want:  `{"a": {"b": {"c": 1}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var base, patch, want any
			if err := json.Unmarshal([]byte(tt.base), &base); err != nil {
				t.Fatalf("bad base: %v", err)
			}
			if err := json.Unmarshal([]byte(tt.patch), &patch); err != nil {
				t.Fatalf("bad patch: %v", err)
			}
			if err := json.Unmarshal([]byte(tt.want), &want); err != nil {
				t.Fatalf("bad want: %v", err)
			}

			got := MergePatch(base, patch)

			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("got %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestMergePatchBytes(t *testing.T) {
	base := []byte(`{"a": {"b": 1, "c": 2}, "d": 3}`)
	patch := []byte(`{"a": {"b": 99, "c": null}, "e": 4}`)

	got, err := MergePatchBytes(base, patch)
	if err != nil {
		t.Fatalf("MergePatchBytes: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	a := result["a"].(map[string]any)
	if a["b"] != float64(99) {
		t.Errorf("a.b = %v, want 99", a["b"])
	}
	if _, exists := a["c"]; exists {
		t.Error("a.c should have been deleted")
	}
	if result["d"] != float64(3) {
		t.Errorf("d = %v, want 3", result["d"])
	}
	if result["e"] != float64(4) {
		t.Errorf("e = %v, want 4", result["e"])
	}
}

func TestMergePatchDoesNotMutateBase(t *testing.T) {
	var base, patch any
	if err := json.Unmarshal([]byte(`{"a": {"b": 1}}`), &base); err != nil {
		t.Fatalf("unmarshal base: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"a": {"b": 2}}`), &patch); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}

	MergePatch(base, patch)

	// Original base should be unchanged.
	baseMap := base.(map[string]any)
	inner := baseMap["a"].(map[string]any)
	if inner["b"] != float64(1) {
		t.Error("MergePatch mutated the base map")
	}
}
