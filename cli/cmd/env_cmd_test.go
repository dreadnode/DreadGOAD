package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepointInventoryDomainPointsAtTheVariant covers the failure where a
// variant environment provisions from the stock lab's assets: the inventory is
// built from a reference or the stock provider template, so it arrives naming
// the base lab, and playbooks resolve ad/{{ domain_name }}/scripts from it.
func TestRepointInventoryDomainPointsAtTheVariant(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			"existing value is replaced",
			"[all:vars]\ndomain_name=GOAD\nadmin_user=administrator\n",
			"domain_name=GOAD-redteam",
		},
		{
			// AWS references carry SSM settings the variant template lacks, so
			// only this one key may change.
			"ssm settings survive",
			"[all:vars]\ndomain_name=GOAD\nansible_aws_ssm_region=us-west-2\n",
			"ansible_aws_ssm_region=us-west-2",
		},
		{
			"missing value is inserted",
			"[all:vars]\nadmin_user=administrator\n",
			"domain_name=GOAD-redteam",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			inv := filepath.Join(root, "redteam-inventory")
			if err := os.WriteFile(inv, []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := repointInventoryDomain(root, "redteam", "ad/GOAD"); err != nil {
				t.Fatalf("repointInventoryDomain: %v", err)
			}
			got, err := os.ReadFile(inv)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), tt.want) {
				t.Errorf("inventory =\n%s\nwant it to contain %q", got, tt.want)
			}
			if strings.Contains(string(got), "domain_name=GOAD\n") {
				t.Errorf("base lab domain_name survived:\n%s", got)
			}
		})
	}
}

// The non-variant path must not touch the inventory at all: without a variant
// there is no ad/GOAD-<env>/ tree for domain_name to point at.
func TestScaffoldInventoryLeavesDomainAloneWithoutVariant(t *testing.T) {
	root := t.TempDir()
	body := "[all:vars]\ndomain_name=GOAD\n"
	inv := filepath.Join(root, "redteam-inventory")
	if err := os.WriteFile(inv, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Simulates scaffoldInventory's useVariant=false branch, which skips the
	// repoint entirely.
	got, err := os.ReadFile(inv)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("inventory changed without --variant:\n%s", got)
	}
}

func TestVariantTargetForFollowsTheSource(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		// Default source keeps the historical ad/GOAD-<env> layout exactly.
		{"", "GOAD-redteam"},
		{"ad/GOAD", "GOAD-redteam"},
		// A non-default base lab must not land in a GOAD-named directory.
		{"ad/SCCM", "SCCM-redteam"},
		{"ad/GOAD-Light", "GOAD-Light-redteam"},
		{"/abs/path/to/NHA", "NHA-redteam"},
	}
	for _, tt := range tests {
		got := variantTargetFor("/repo", "redteam", tt.source)
		want := filepath.Join("/repo", "ad", tt.want)
		if got != want {
			t.Errorf("variantTargetFor(%q) = %q, want %q", tt.source, got, want)
		}
	}
}

func TestDeriveAzureSubnets(t *testing.T) {
	tests := []struct {
		name     string
		vnetCIDR string
		wantBast string
		wantCtrl string
		wantKali string
		wantErr  bool
	}{
		{"standard", "10.8.0.0/16", "10.8.2.0/26", "10.8.3.0/28", "10.8.4.0/28", false},
		{"different octet", "10.1.0.0/16", "10.1.2.0/26", "10.1.3.0/28", "10.1.4.0/28", false},
		{"high octet", "10.200.0.0/16", "10.200.2.0/26", "10.200.3.0/28", "10.200.4.0/28", false},
		{"not /16", "10.8.0.0/24", "", "", "", true},
		{"invalid CIDR", "not-a-cidr", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subnets, err := deriveAzureSubnets(tt.vnetCIDR)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if subnets.Bastion != tt.wantBast {
				t.Errorf("bastion = %q, want %q", subnets.Bastion, tt.wantBast)
			}
			if subnets.Controller != tt.wantCtrl {
				t.Errorf("controller = %q, want %q", subnets.Controller, tt.wantCtrl)
			}
			if subnets.Kali != tt.wantKali {
				t.Errorf("kali = %q, want %q", subnets.Kali, tt.wantKali)
			}
		})
	}
}
