package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The names below are verbatim from `dreadgoad lab status` against a live
// Azure range. Before the fix these produced "dc01-vm" and "dreadgoad-dc01",
// neither of which matches an inventory host, so every sync was a no-op.
func TestExtractHostRoleAzure(t *testing.T) {
	tests := []struct {
		name   string
		vmName string
		want   string
	}{
		{"azure goad host", "3.1-goad-dreadgoad-DC01-vm", "dc01"},
		{"azure member server", "3.1-goad-dreadgoad-SRV02-vm", "srv02"},
		{"azure dotted env", "dg-test-2.A-goad-dreadgoad-DC03-vm", "dc03"},
		{"azure doubled prefix", "dreadindex2-dreadgoad-dreadgoad-DC01-vm", "dc01"},
		{"azure controller", "3.1-goad-controller-vm", "controller"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractHostRole(tt.vmName); got != tt.want {
				t.Errorf("extractHostRole(%q) = %q, want %q", tt.vmName, got, tt.want)
			}
		})
	}
}

// A machine whose whole name is the prefix plus the suffix leaves nothing to
// use as a role. Returning "dreadgoad" there would build a regex that matches
// no host but still looks like a successful extraction.
func TestExtractHostRoleDegenerateAzureName(t *testing.T) {
	if got := extractHostRole("dreadgoad-vm"); got != "" {
		t.Errorf("extractHostRole(\"dreadgoad-vm\") = %q, want empty", got)
	}
}

func TestApplyInstanceUpdatesAzure(t *testing.T) {
	invPath := filepath.Join(t.TempDir(), "3.1-inventory")
	content := "[default]\n" +
		"dc01 ansible_host=PENDING dns_domain=dc01 dict_key=dc01\n" +
		"dc02 ansible_host=PENDING dns_domain=dc01 dict_key=dc02\n" +
		"srv02 ansible_host=PENDING dns_domain=dc02 dict_key=srv02\n"
	if err := os.WriteFile(invPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	instances := []instanceInfo{
		{InstanceID: "/subscriptions/x/dc01", Name: "3.1-goad-dreadgoad-DC01-vm", PrivateIP: "10.100.1.5"},
		{InstanceID: "/subscriptions/x/dc02", Name: "3.1-goad-dreadgoad-DC02-vm", PrivateIP: "10.100.1.4"},
		{InstanceID: "/subscriptions/x/srv02", Name: "3.1-goad-dreadgoad-SRV02-vm", PrivateIP: "10.100.1.6"},
		// The controller is a real machine in the resource group but has no
		// inventory host; it must not error the sync.
		{InstanceID: "/subscriptions/x/ctl", Name: "3.1-goad-controller-vm", PrivateIP: "10.100.3.4"},
	}
	if err := applyInstanceUpdates(invPath, instances); err != nil {
		t.Fatalf("applyInstanceUpdates: %v", err)
	}

	got, err := os.ReadFile(invPath)
	if err != nil {
		t.Fatal(err)
	}
	result := string(got)
	// The resource ID must never land in ansible_host — the private IP does.
	for host, ip := range map[string]string{"dc01": "10.100.1.5", "dc02": "10.100.1.4", "srv02": "10.100.1.6"} {
		if !strings.Contains(result, host+" ansible_host="+ip) {
			t.Errorf("missing %s -> %s in:\n%s", host, ip, result)
		}
	}
	if strings.Contains(result, "PENDING") {
		t.Errorf("placeholder survived the sync:\n%s", result)
	}
}

// The regression that cost two full apply cycles: the sync ran, matched
// nothing, and printed "All values are current" over an inventory that was
// entirely PENDING. Silence here is worse than a wrong value.
func TestApplyInstanceUpdatesRejectsSilentNoOp(t *testing.T) {
	invPath := filepath.Join(t.TempDir(), "3.1-inventory")
	content := "[default]\ndc01 ansible_host=PENDING dns_domain=dc01 dict_key=dc01\n"
	if err := os.WriteFile(invPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	err := applyInstanceUpdates(invPath, []instanceInfo{
		{InstanceID: "i-1", Name: "some-unrelated-machine", PrivateIP: "10.0.0.9"},
	})
	if err == nil {
		t.Fatal("a sync that left every host PENDING reported success")
	}
	// The operator needs both halves to debug it: which host is unresolved,
	// and what the discovery actually returned.
	for _, want := range []string{"dc01", "some-unrelated-machine"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

// An unrendered provider template is the same failure wearing a different
// placeholder, and reaches Ansible the same way.
func TestApplyInstanceUpdatesRejectsUnrenderedTemplate(t *testing.T) {
	invPath := filepath.Join(t.TempDir(), "3.1-inventory")
	content := "[default]\ndc01 ansible_host={{ip_range}}.10 dns_domain=dc01 dict_key=dc01\n"
	if err := os.WriteFile(invPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyInstanceUpdates(invPath, []instanceInfo{
		{InstanceID: "i-1", Name: "nope", PrivateIP: "10.0.0.9"},
	}); err == nil {
		t.Fatal("an unrendered {{ip_range}} template reported success")
	}
}

// A fully-resolved inventory must stay quiet. Erroring here would break every
// idempotent re-run of provision.
func TestApplyInstanceUpdatesResolvedInventoryIsNotAnError(t *testing.T) {
	invPath := filepath.Join(t.TempDir(), "3.1-inventory")
	content := "[default]\ndc01 ansible_host=10.100.1.5 dns_domain=dc01 dict_key=dc01\n"
	if err := os.WriteFile(invPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyInstanceUpdates(invPath, []instanceInfo{
		{InstanceID: "/subscriptions/x/dc01", Name: "3.1-goad-dreadgoad-DC01-vm", PrivateIP: "10.100.1.5"},
	}); err != nil {
		t.Fatalf("already-current inventory reported an error: %v", err)
	}
}

// PENDING appearing anywhere other than an ansible_host value (a password, a
// comment) must not trip the guard.
func TestPlaceholderHostsIgnoresOtherFields(t *testing.T) {
	inventory := "; PENDING review\n" +
		"dc01 ansible_host=10.100.1.5 ansible_password=PENDING123\n"
	if got := placeholderHosts(inventory); len(got) != 0 {
		t.Errorf("placeholderHosts = %v, want none", got)
	}
}

// Ansible accepts indented host lines. A gate that only recognises
// column-zero hosts would pass an inventory holding the very failure it
// exists to catch.
func TestPlaceholderHostsSeesIndentedHosts(t *testing.T) {
	for name, body := range map[string]string{
		"spaces": "    dc01 ansible_host=PENDING dict_key=dc01\n",
		"tab":    "\tdc01 ansible_host=PENDING dict_key=dc01\n",
	} {
		got := placeholderHosts(body)
		if len(got) != 1 || got[0] != "dc01" {
			t.Errorf("%s-indented host: placeholderHosts = %v, want [dc01]", name, got)
		}
	}
}

// "pending" must match as a whole value, not as a prefix. A real address that
// merely starts with those letters is correctly configured, and blocking it
// would be a false alarm on a working range.
func TestPlaceholderHostsMatchesWholeValueOnly(t *testing.T) {
	for _, addr := range []string{"pending-lab.example.com", "pendingtonhost", "10.1.1.5"} {
		body := "dc01 ansible_host=" + addr + " dict_key=dc01\n"
		if got := placeholderHosts(body); len(got) != 0 {
			t.Errorf("ansible_host=%s was flagged as a placeholder: %v", addr, got)
		}
	}
	// The bare token, with and without a trailing field, still must be caught.
	for _, body := range []string{"dc01 ansible_host=PENDING\n", "dc01 ansible_host=PENDING", "dc01 ansible_host=PENDING x=1\n"} {
		if got := placeholderHosts(body); len(got) != 1 {
			t.Errorf("bare PENDING not caught in %q: %v", body, got)
		}
	}
}

// A CRLF inventory (edited on Windows, or fetched over a share) must not slip
// past the gate: \r would otherwise sit between the value and the line end.
func TestPlaceholderHostsHandlesCRLF(t *testing.T) {
	if got := placeholderHosts("dc01 ansible_host=PENDING\r\ndc02 ansible_host=10.1.1.5\r\n"); len(got) != 1 || got[0] != "dc01" {
		t.Errorf("placeholderHosts on CRLF = %v, want [dc01]", got)
	}
}
