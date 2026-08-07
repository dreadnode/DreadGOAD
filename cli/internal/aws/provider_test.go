package aws

import "testing"

func TestToProviderInstancePreservesTags(t *testing.T) {
	tags := map[string]string{"Role": "AttackBox", "Environment": "test"}
	got := toProviderInstance(Instance{
		InstanceID: "i-kali",
		Name:       "test-goad-dreadgoad-kali",
		PrivateIP:  "10.8.4.10",
		State:      "running",
		Tags:       tags,
	})

	if got.Tags["Role"] != "AttackBox" || got.Tags["Environment"] != "test" {
		t.Fatalf("toProviderInstance() tags = %#v, want Role and Environment", got.Tags)
	}
}
