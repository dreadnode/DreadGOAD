package azure

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v10"
)

func TestPublicIPChecks(t *testing.T) {
	vmInfos := []vmNICInfo{
		{vmName: "dc01", nics: []NICDetail{{Name: "dc01-nic"}}},
		{
			vmName: "bastion",
			tags:   map[string]string{"Role": "bastion"},
			nics:   []NICDetail{{Name: "bastion-nic", PublicIPID: "/publicIPAddresses/bastion-pip"}},
		},
		{
			vmName: "dc02",
			tags:   map[string]string{"Role": "dc"},
			nics:   []NICDetail{{Name: "dc02-nic", PublicIPID: "/publicIPAddresses/dc02-pip"}},
		},
	}

	results := publicIPChecks(vmInfos)
	wantStatuses := []string{"OK", "OK", "FAIL"}
	if len(results) != len(wantStatuses) {
		t.Fatalf("results = %d, want %d", len(results), len(wantStatuses))
	}
	for i, want := range wantStatuses {
		if results[i].Status != want {
			t.Errorf("result %d status = %q, want %q", i, results[i].Status, want)
		}
	}
}

func TestNSGPresenceChecks(t *testing.T) {
	vmInfos := []vmNICInfo{{
		vmName: "dc01",
		nics: []NICDetail{
			{Name: "protected", NSGID: "/networkSecurityGroups/dc01-nsg"},
			{Name: "unprotected"},
		},
	}}

	results := nsgPresenceChecks(vmInfos, nil)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Status != "OK" || results[1].Status != "FAIL" {
		t.Fatalf("statuses = [%s %s], want [OK FAIL]", results[0].Status, results[1].Status)
	}
}

func TestSecurityRulesOfSkipsRuleWithoutAccess(t *testing.T) {
	direction := armnetwork.SecurityRuleDirectionInbound
	nsg := &armnetwork.SecurityGroup{Properties: &armnetwork.SecurityGroupPropertiesFormat{
		SecurityRules: []*armnetwork.SecurityRule{{
			Properties: &armnetwork.SecurityRulePropertiesFormat{Direction: &direction},
		}},
	}}

	if rules := securityRulesOf(nsg); len(rules) != 0 {
		t.Fatalf("rules = %v, want none", rules)
	}
}
