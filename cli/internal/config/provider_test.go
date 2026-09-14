package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilterProviderInstancesByLabFollowsRangeKind(t *testing.T) {
	root := t.TempDir()
	for lab, kind := range map[string]string{
		"service": "service-range",
		"ad":      "active-directory",
	} {
		dir := filepath.Join(root, "ad", lab)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := []byte("schema_version: 1\nkind: " + kind + "\n")
		if err := os.WriteFile(filepath.Join(dir, "range.yml"), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		lab  string
		want bool
	}{
		{lab: "service", want: true},
		{lab: "ad", want: false},
		{lab: "legacy-without-manifest", want: false},
	}
	for _, test := range tests {
		t.Run(test.lab, func(t *testing.T) {
			got, err := (&Config{ProjectRoot: root, Lab: test.lab}).filterProviderInstancesByLab()
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("filterProviderInstancesByLab() = %v, want %v", got, test.want)
			}
		})
	}
}
