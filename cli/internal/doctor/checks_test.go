package doctor

import "testing"

func TestClassifyAnsibleVersionRejectsUnsupportedVersionForEveryProvider(t *testing.T) {
	for _, provider := range []string{"aws", "azure", "proxmox"} {
		t.Run(provider, func(t *testing.T) {
			got := classifyAnsibleVersion("ansible [core 2.20.8]", provider)
			if got.Status != "fail" {
				t.Fatalf("status = %q, want fail: %#v", got.Status, got)
			}
		})
	}
}

func TestClassifyAnsibleVersionAcceptsSupportedVersion(t *testing.T) {
	got := classifyAnsibleVersion("ansible [core 2.17.14]", "azure")
	if got.Status != "pass" {
		t.Fatalf("status = %q, want pass: %#v", got.Status, got)
	}
}

func TestClassifyAnsibleVersionRejectsUnparseableOutput(t *testing.T) {
	got := classifyAnsibleVersion("not an ansible version", "azure")
	if got.Status != "fail" {
		t.Fatalf("status = %q, want fail: %#v", got.Status, got)
	}
}
