package cmd

import (
	"encoding/json"
	"testing"
)

func TestExtensionsToJSON(t *testing.T) {
	// empty → bare array, not null
	b, err := extensionsToJSON(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(b) != "[]" {
		t.Fatalf("empty must render as [], got %q", b)
	}

	exts := []extensionJSON{
		{Name: "elk", Enabled: true, Machines: []string{"elk"}, Compatible: []string{"*"}, Description: "ELK stack"},
		{Name: "exchange", Enabled: false, Machines: []string{"srv01"}, Compatible: []string{"GOAD"}},
	}
	b, err = extensionsToJSON(exts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var decoded []extensionJSON
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("want 2, got %d", len(decoded))
	}
	if decoded[0].Name != "elk" || !decoded[0].Enabled || decoded[0].Machines[0] != "elk" {
		t.Fatalf("elk mapping wrong: %+v", decoded[0])
	}
	if decoded[1].Enabled {
		t.Fatalf("exchange should be disabled: %+v", decoded[1])
	}
}
