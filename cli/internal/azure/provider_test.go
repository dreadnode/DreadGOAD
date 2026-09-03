package azure

import (
	"testing"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

func TestFilterProviderInstancesByLab(t *testing.T) {
	instances := []provider.Instance{
		{ID: "goad", Tags: map[string]string{"Lab": "goad-goad"}},
		{ID: "scope", Tags: map[string]string{"Lab": "SCOPE-RANGE"}},
		{ID: "other", Tags: map[string]string{"Lab": "OTHER"}},
	}

	legacy := filterProviderInstancesByLab(instances, "GOAD")
	if len(legacy) != len(instances) {
		t.Fatalf("GOAD compatibility filter returned %d instances, want %d", len(legacy), len(instances))
	}

	scope := filterProviderInstancesByLab(instances, "scope-range")
	if len(scope) != 1 || scope[0].ID != "scope" {
		t.Fatalf("scope filter = %#v, want only scope", scope)
	}

	if got := filterProviderInstancesByLab(instances, "missing"); len(got) != 0 {
		t.Fatalf("missing lab filter returned %#v, want empty", got)
	}
}
