package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderRangeTagFollowsManifest(t *testing.T) {
	root := t.TempDir()
	for lab, body := range map[string]string{
		"service": "schema_version: 1\nkind: service-range\ndiscovery:\n  range_tag: SERVICE\n",
		"ad":      "schema_version: 1\nkind: active-directory\ndiscovery:\n  range_tag: GOAD\n",
	} {
		dir := filepath.Join(root, "ad", lab)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "range.yml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		lab  string
		want string
	}{
		{lab: "service", want: "SERVICE"},
		{lab: "ad", want: "GOAD"},
	}
	for _, test := range tests {
		t.Run(test.lab, func(t *testing.T) {
			got, err := (&Config{ProjectRoot: root, Lab: test.lab}).ProviderRangeTag()
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("providerRangeTag() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestProviderRangeTagRejectsMissingManifest(t *testing.T) {
	root := t.TempDir()
	if _, err := (&Config{ProjectRoot: root, Lab: "missing"}).ProviderRangeTag(); err == nil {
		t.Fatal("range without a manifest must fail closed")
	}
}

func TestProviderRangeTagRequiresManifestIdentity(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ad", "service")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "range.yml"), []byte("schema_version: 1\nkind: service-range\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Config{ProjectRoot: root, Lab: "service"}).ProviderRangeTag(); err == nil {
		t.Fatal("range manifest without discovery.range_tag must fail")
	}
}
