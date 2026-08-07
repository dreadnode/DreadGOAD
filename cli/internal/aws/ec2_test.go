package aws

import (
	"reflect"
	"testing"
)

func TestDiscoveryFiltersUseProjectAndEnvironmentTags(t *testing.T) {
	filters := discoveryFilters("staging", []string{"running", "stopped"})
	got := make(map[string][]string, len(filters))
	for _, filter := range filters {
		got[deref(filter.Name)] = filter.Values
	}

	want := map[string][]string{
		"tag:Project":         {"DreadGOAD"},
		"tag:Environment":     {"staging"},
		"instance-state-name": {"running", "stopped"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoveryFilters() = %#v, want %#v", got, want)
	}
	if _, exists := got["tag:Name"]; exists {
		t.Fatal("discoveryFilters() must not require a Name tag")
	}
}

func TestDiscoveryFiltersOmitStateWhenEmpty(t *testing.T) {
	filters := discoveryFilters("test", nil)
	for _, filter := range filters {
		if deref(filter.Name) == "instance-state-name" {
			t.Fatal("discoveryFilters() included an empty instance-state-name filter")
		}
	}
}
