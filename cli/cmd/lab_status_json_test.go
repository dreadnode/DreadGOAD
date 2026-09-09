package cmd

import (
	"encoding/json"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

func TestInstancesToStatusJSON(t *testing.T) {
	tests := []struct {
		name      string
		instances []provider.Instance
		wantLen   int
	}{
		{
			name:      "empty yields JSON array not null",
			instances: nil,
			wantLen:   0,
		},
		{
			name: "running and stopped instances",
			instances: []provider.Instance{
				{ID: "i-0abc", Name: "goad-dreadgoad-kingslanding", State: "running", PrivateIP: "10.0.4.124"},
				{ID: "i-0def", Name: "goad-dreadgoad-winterfell", State: "stopped", PrivateIP: "10.0.4.76"},
			},
			wantLen: 2,
		},
	}

	tests = append(tests, struct {
		name      string
		instances []provider.Instance
		wantLen   int
	}{
		name: "account and group carried through",
		instances: []provider.Instance{
			{ID: "i-0abc", Name: "aws-box", State: "running", Account: "123456789012"},
		},
		wantLen: 1,
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := instancesToStatusJSON(tt.instances)
			if err != nil {
				t.Fatalf("instancesToStatusJSON returned error: %v", err)
			}

			// Must always be a JSON array (never the literal "null"), so an
			// empty range decodes to "[]" for the ingestion hook.
			var decoded []statusJSONInstance
			if err := json.Unmarshal(b, &decoded); err != nil {
				t.Fatalf("output is not valid JSON array: %v (raw: %s)", err, b)
			}
			if string(b) == "null" {
				t.Fatalf("empty input must render as [] not null")
			}
			if len(decoded) != tt.wantLen {
				t.Fatalf("want %d instances, got %d", tt.wantLen, len(decoded))
			}
		})
	}
}

func TestInstancesToStatusJSONEmptyIsBareArray(t *testing.T) {
	b, err := instancesToStatusJSON(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(b) != "[]" {
		t.Fatalf("empty range must render exactly as [], got %q", b)
	}
}

func TestInstancesToStatusJSONStoppedNoIP(t *testing.T) {
	// A stopped instance has no private IP; it must still round-trip cleanly.
	in := []provider.Instance{
		{ID: "i-0def", Name: "goad-dreadgoad-winterfell", State: "stopped", PrivateIP: ""},
	}
	b, err := instancesToStatusJSON(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var decoded []statusJSONInstance
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded[0].State != "stopped" || decoded[0].PrivateIP != "" {
		t.Fatalf("stopped/empty-IP passthrough wrong: %+v", decoded[0])
	}
}

func TestInstancesToStatusJSONFieldMapping(t *testing.T) {
	in := []provider.Instance{
		{ID: "i-0abc", Name: "goad-dreadgoad-kingslanding", State: "running", PrivateIP: "10.0.4.124"},
	}
	b, err := instancesToStatusJSON(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var decoded []statusJSONInstance
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	got := decoded[0]
	if got.ID != "i-0abc" || got.Name != "goad-dreadgoad-kingslanding" ||
		got.State != "running" || got.PrivateIP != "10.0.4.124" {
		t.Fatalf("field mapping wrong: %+v", got)
	}
}
