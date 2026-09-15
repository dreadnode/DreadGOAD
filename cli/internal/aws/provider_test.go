package aws

import (
	"testing"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

func TestAWSInstanceUsesShellOnlyForExplicitLinuxTag(t *testing.T) {
	for _, tc := range []struct {
		name string
		tags map[string]string
		want bool
	}{
		{name: "linux", tags: map[string]string{"OS": "Linux"}, want: true},
		{name: "case and whitespace", tags: map[string]string{"OS": " linux "}, want: true},
		{name: "windows", tags: map[string]string{"OS": "Windows"}},
		{name: "missing preserves legacy PowerShell", tags: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := awsInstanceUsesShell(provider.Instance{Tags: tc.tags}); got != tc.want {
				t.Fatalf("awsInstanceUsesShell() = %v, want %v", got, tc.want)
			}
		})
	}
}

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
