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

func TestFilterInstancesByRange(t *testing.T) {
	instances := []Instance{
		{ID: "goad", Tags: map[string]string{"Range": "GOAD"}},
		{ID: "goat", Tags: map[string]string{"Range": "GOAT"}},
		{ID: "lowercase-goat", Tags: map[string]string{"Range": "goat"}},
		{ID: "untagged"},
	}

	if got := FilterInstancesByRange(instances, ""); len(got) != 4 {
		t.Fatalf("empty range filter returned %#v, want all instances", got)
	}
	got := FilterInstancesByRange(instances, "GOAT")
	if len(got) != 1 || got[0].ID != "goat" {
		t.Fatalf("GOAT filter returned %#v, want only exact GOAT tag", got)
	}
	got = FilterInstancesByRange(instances, "GOAD")
	if len(got) != 1 || got[0].ID != "goad" {
		t.Fatalf("GOAD filter returned %#v, want only exact GOAD tag", got)
	}
}
