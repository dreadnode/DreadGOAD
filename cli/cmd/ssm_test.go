package cmd

import (
	"context"
	"fmt"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/provider"
)

type ssmDiscoveryProvider struct {
	provider.Provider
	byName    *provider.Instance
	instances []provider.Instance
}

func (p *ssmDiscoveryProvider) FindInstanceByHostname(context.Context, string, string) (*provider.Instance, error) {
	if p.byName == nil {
		return nil, fmt.Errorf("not found")
	}
	return p.byName, nil
}

func (p *ssmDiscoveryProvider) DiscoverInstances(context.Context, string) ([]provider.Instance, error) {
	return p.instances, nil
}

func TestResolveSSMHostFallsBackToDiscovery(t *testing.T) {
	want := &provider.Instance{ID: "i-kali", Name: "test-goad-dreadgoad-kali"}
	prov := &ssmDiscoveryProvider{byName: want}

	got, err := resolveSSMHost(context.Background(), prov, "test", nil, "kali")
	if err != nil {
		t.Fatalf("resolveSSMHost() error = %v", err)
	}
	if got.ID != want.ID {
		t.Fatalf("resolveSSMHost() ID = %q, want %q", got.ID, want.ID)
	}
}

func TestResolveSSMHostAcceptsAttackBoxRole(t *testing.T) {
	prov := &ssmDiscoveryProvider{instances: []provider.Instance{
		{ID: "i-dc", Tags: map[string]string{"Role": "DomainController"}},
		{ID: "i-kali", Name: "custom-attacker", Tags: map[string]string{"Role": "AttackBox"}},
	}}

	got, err := resolveSSMHost(context.Background(), prov, "test", nil, "attack-box")
	if err != nil {
		t.Fatalf("resolveSSMHost() error = %v", err)
	}
	if got.ID != "i-kali" {
		t.Fatalf("resolveSSMHost() ID = %q, want i-kali", got.ID)
	}
}

func TestResolveSSMHostPrefersInventory(t *testing.T) {
	inv := &inventory.Inventory{Hosts: map[string]*inventory.Host{
		"dc01": {Name: "dc01", InstanceID: "i-inventory"},
	}}
	prov := &ssmDiscoveryProvider{byName: &provider.Instance{ID: "i-discovery", Name: "dc01"}}

	got, err := resolveSSMHost(context.Background(), prov, "test", inv, "dc01")
	if err != nil {
		t.Fatalf("resolveSSMHost() error = %v", err)
	}
	if got.ID != "i-inventory" {
		t.Fatalf("resolveSSMHost() ID = %q, want i-inventory", got.ID)
	}
}

func TestFilterProviderInstancesAllExcludesAttackBox(t *testing.T) {
	instances := []provider.Instance{
		{ID: "i-dc", Name: "dc01", Tags: map[string]string{"Role": "DomainController"}},
		{ID: "i-kali", Name: "kali", Tags: map[string]string{"Role": "AttackBox"}},
	}

	ids, names := filterProviderInstances(instances, "all")
	if len(ids) != 1 || ids[0] != "i-dc" || len(names) != 1 || names[0] != "dc01" {
		t.Fatalf("filterProviderInstances(all) = ids=%v names=%v, want only dc01", ids, names)
	}
}
