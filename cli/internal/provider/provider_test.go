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

func TestFilterInstancesByLab(t *testing.T) {
	instances := []Instance{
		{ID: "goad", Tags: map[string]string{"Lab": "dreadgoad"}},
		{ID: "goat", Tags: map[string]string{"Lab": "SCOPE-RANGE"}},
	}

	if got := FilterInstancesByLab(instances, "GOAD-Light", false); len(got) != 2 {
		t.Fatalf("disabled filter returned %#v, want all instances", got)
	}
	got := FilterInstancesByLab(instances, "scope-range", true)
	if len(got) != 1 || got[0].ID != "goat" {
		t.Fatalf("enabled filter returned %#v, want only GOAT", got)
	}
}
