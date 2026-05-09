package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractHostRole(t *testing.T) {
	tests := []struct {
		name   string
		vmName string
		want   string
	}{
		{"aws naming", "dreadgoad-dc01", "dc01"},
		{"aws naming uppercase", "dreadgoad-DC01", "dc01"},
		{"aws srv02", "dreadgoad-srv02", "srv02"},
		{"ludus naming", "DG-GOAD-DC01", "dc01"},
		{"ludus srv", "DG-GOAD-SRV02", "srv02"},
		{"ludus dc03", "DG-GOAD-DC03", "dc03"},
		{"proxmox naming", "GOAD-DC01", "dc01"},
		{"extension host", "DG-GOAD-WS01", "ws01"},
		{"linux host", "DG-GOAD-LX01", "lx01"},
		{"single segment", "dc01", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHostRole(tt.vmName)
			if got != tt.want {
				t.Errorf("extractHostRole(%q) = %q, want %q", tt.vmName, got, tt.want)
			}
		})
	}
}

func TestApplyInstanceUpdates_AWS(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "staging-inventory")

	content := "[default]\ndc01 ansible_host=i-old001 dns_domain=dc01 dict_key=dc01\ndc02 ansible_host=i-old002 dns_domain=dc02 dict_key=dc02\nsrv02 ansible_host=i-old003 dns_domain=dc02 dict_key=srv02\n"
	if err := os.WriteFile(invPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write inventory: %v", err)
	}

	instances := []instanceInfo{
		{InstanceID: "i-new001", Name: "dreadgoad-dc01"},
		{InstanceID: "i-new002", Name: "dreadgoad-dc02"},
		{InstanceID: "i-new003", Name: "dreadgoad-srv02"},
	}

	if err := applyInstanceUpdates(invPath, instances); err != nil {
		t.Fatalf("applyInstanceUpdates() error: %v", err)
	}

	got, err := os.ReadFile(invPath)
	if err != nil {
		t.Fatalf("read inventory: %v", err)
	}

	result := string(got)
	for _, tc := range []struct {
		host string
		want string
	}{
		{"dc01", "i-new001"},
		{"dc02", "i-new002"},
		{"srv02", "i-new003"},
	} {
		expected := tc.host + " ansible_host=" + tc.want
		if !contains(result, expected) {
			t.Errorf("expected %q in inventory, got:\n%s", expected, result)
		}
	}
}

func TestApplyInstanceUpdates_Ludus(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "staging-inventory")

	content := "[default]\ndc01 ansible_host=10.0.10.10 dns_domain=dc01 dict_key=dc01\ndc02 ansible_host=10.0.10.11 dns_domain=dc02 dict_key=dc02\nsrv02 ansible_host=10.0.10.22 dns_domain=dc02 dict_key=srv02\n"
	if err := os.WriteFile(invPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write inventory: %v", err)
	}

	// Ludus instances have PrivateIP set and use "DG-GOAD-<ROLE>" naming.
	instances := []instanceInfo{
		{InstanceID: "104", Name: "DG-GOAD-DC01", PrivateIP: "10.2.10.10"},
		{InstanceID: "105", Name: "DG-GOAD-DC02", PrivateIP: "10.2.10.11"},
		{InstanceID: "106", Name: "DG-GOAD-SRV02", PrivateIP: "10.2.10.22"},
	}

	if err := applyInstanceUpdates(invPath, instances); err != nil {
		t.Fatalf("applyInstanceUpdates() error: %v", err)
	}

	got, err := os.ReadFile(invPath)
	if err != nil {
		t.Fatalf("read inventory: %v", err)
	}

	result := string(got)
	// Should use PrivateIP, not InstanceID (Proxmox VMID).
	for _, tc := range []struct {
		host    string
		wantIP  string
		notWant string
	}{
		{"dc01", "10.2.10.10", "104"},
		{"dc02", "10.2.10.11", "105"},
		{"srv02", "10.2.10.22", "106"},
	} {
		expected := tc.host + " ansible_host=" + tc.wantIP
		if !contains(result, expected) {
			t.Errorf("expected %q in inventory, got:\n%s", expected, result)
		}
		bad := tc.host + " ansible_host=" + tc.notWant
		if contains(result, bad) {
			t.Errorf("inventory should not contain VMID as ansible_host: %q", bad)
		}
	}
}

func TestApplyInstanceUpdates_NoChanges(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "staging-inventory")

	content := "[default]\ndc01 ansible_host=10.2.10.10 dns_domain=dc01 dict_key=dc01\n"
	if err := os.WriteFile(invPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write inventory: %v", err)
	}

	// Same IP as already in inventory, should be a no-op.
	instances := []instanceInfo{
		{InstanceID: "104", Name: "DG-GOAD-DC01", PrivateIP: "10.2.10.10"},
	}

	if err := applyInstanceUpdates(invPath, instances); err != nil {
		t.Fatalf("applyInstanceUpdates() error: %v", err)
	}

	got, err := os.ReadFile(invPath)
	if err != nil {
		t.Fatalf("read inventory: %v", err)
	}
	if string(got) != content {
		t.Errorf("inventory was modified when no changes were needed")
	}
}

func TestApplyInstanceUpdates_MixedProviders(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "staging-inventory")

	content := "[default]\ndc01 ansible_host=OLD dns_domain=dc01 dict_key=dc01\nws01 ansible_host=OLD dns_domain=dc01 dict_key=ws01\n"
	if err := os.WriteFile(invPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write inventory: %v", err)
	}

	// Extension host with Ludus naming should also work.
	instances := []instanceInfo{
		{InstanceID: "104", Name: "DG-GOAD-DC01", PrivateIP: "10.2.10.10"},
		{InstanceID: "107", Name: "DG-GOAD-WS01", PrivateIP: "10.2.10.30"},
	}

	if err := applyInstanceUpdates(invPath, instances); err != nil {
		t.Fatalf("applyInstanceUpdates() error: %v", err)
	}

	got, err := os.ReadFile(invPath)
	if err != nil {
		t.Fatalf("read inventory: %v", err)
	}

	result := string(got)
	if !contains(result, "dc01 ansible_host=10.2.10.10") {
		t.Errorf("dc01 not updated, got:\n%s", result)
	}
	if !contains(result, "ws01 ansible_host=10.2.10.30") {
		t.Errorf("ws01 not updated, got:\n%s", result)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
