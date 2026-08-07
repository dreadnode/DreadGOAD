package provider

import "testing"

func TestFindInstanceByRole(t *testing.T) {
	instances := []Instance{
		{ID: "i-domain", Tags: map[string]string{"Role": "DomainController"}},
		{ID: "i-kali", Tags: map[string]string{"Role": "AttackBox"}},
	}

	got := FindInstanceByRole(instances, "attackbox")
	if got == nil || got.ID != "i-kali" {
		t.Fatalf("FindInstanceByRole() = %#v, want i-kali", got)
	}
	if got := FindInstanceByRole(instances, "missing"); got != nil {
		t.Fatalf("FindInstanceByRole() = %#v, want nil", got)
	}
}
