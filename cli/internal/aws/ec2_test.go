package aws

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestDiscoveryFilterSetsSupportTagsAndLegacyNames(t *testing.T) {
	filterSets := discoveryFilterSets("staging", []string{"running", "stopped"})
	if len(filterSets) != 2 {
		t.Fatalf("discoveryFilterSets() returned %d sets, want 2", len(filterSets))
	}

	got := make([]map[string][]string, 0, len(filterSets))
	for _, filters := range filterSets {
		set := make(map[string][]string, len(filters))
		for _, filter := range filters {
			set[deref(filter.Name)] = filter.Values
		}
		got = append(got, set)
	}

	want := []map[string][]string{
		{
			"tag:Project":         {"DreadGOAD"},
			"tag:Environment":     {"staging"},
			"instance-state-name": {"running", "stopped"},
		},
		{
			"tag:Name":            {"*staging*dreadgoad*"},
			"instance-state-name": {"running", "stopped"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoveryFilterSets() = %#v, want %#v", got, want)
	}
}

func TestDiscoveryFilterSetsOmitStateWhenEmpty(t *testing.T) {
	for _, filters := range discoveryFilterSets("test", nil) {
		for _, filter := range filters {
			if deref(filter.Name) == "instance-state-name" {
				t.Fatal("discoveryFilterSets() included an empty instance-state-name filter")
			}
		}
	}
}

func TestAppendDiscoveredInstancesDeduplicatesAndPreservesTags(t *testing.T) {
	reservation := types.Reservation{Instances: []types.Instance{
		{
			InstanceId: Ptr("i-kali"),
			State:      &types.InstanceState{Name: types.InstanceStateNameRunning},
			Tags: []types.Tag{
				{Key: Ptr("Name"), Value: Ptr("test-goad-dreadgoad-kali")},
				{Key: Ptr("Role"), Value: Ptr("AttackBox")},
			},
		},
	}}
	seen := make(map[string]struct{})

	instances := appendDiscoveredInstances(nil, seen, []types.Reservation{reservation})
	instances = appendDiscoveredInstances(instances, seen, []types.Reservation{reservation})

	if len(instances) != 1 {
		t.Fatalf("appendDiscoveredInstances() returned %d instances, want 1", len(instances))
	}
	if instances[0].Name != "test-goad-dreadgoad-kali" || instances[0].Tags["Role"] != "AttackBox" {
		t.Fatalf("appendDiscoveredInstances() = %#v, want Name and Role tags preserved", instances[0])
	}
}
