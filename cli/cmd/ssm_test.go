package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/provider"
)

type ssmDiscoveryProvider struct {
	provider.Provider
	byName        *provider.Instance
	byNameErr     error
	instances     []provider.Instance
	discoverErr   error
	findCalls     int
	discoverCalls int
}

func (p *ssmDiscoveryProvider) FindInstanceByHostname(context.Context, string, string) (*provider.Instance, error) {
	p.findCalls++
	if p.byNameErr != nil {
		return nil, p.byNameErr
	}
	if p.byName == nil {
		return nil, fmt.Errorf("not found")
	}
	return p.byName, nil
}

func (p *ssmDiscoveryProvider) DiscoverInstances(context.Context, string) ([]provider.Instance, error) {
	p.discoverCalls++
	return p.instances, p.discoverErr
}

func TestResolveSSMHostFallsBackToDiscovery(t *testing.T) {
	want := &provider.Instance{ID: "i-kali", Name: "test-goad-dreadgoad-kali", State: "running"}
	prov := &ssmDiscoveryProvider{byName: want}

	got, err := resolveSSMHost(context.Background(), prov, "test", nil, "kali")
	if err != nil {
		t.Fatalf("resolveSSMHost() error = %v", err)
	}
	if got.ID != want.ID {
		t.Fatalf("resolveSSMHost() ID = %q, want %q", got.ID, want.ID)
	}
}

func TestResolveSSMProviderOptionsPrefersInventoryRegion(t *testing.T) {
	cfg := &config.Config{Region: "us-west-2"}
	inv := &inventory.Inventory{Vars: map[string]string{
		"ansible_aws_ssm_region": "us-east-2",
	}}

	opts, err := resolveSSMProviderOptions(cfg, inv)
	if err != nil {
		t.Fatalf("resolveSSMProviderOptions() error = %v", err)
	}
	if opts.Region != "us-east-2" {
		t.Fatalf("resolveSSMProviderOptions() region = %q, want us-east-2", opts.Region)
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
	if prov.findCalls != 0 || prov.discoverCalls != 1 {
		t.Fatalf("resolveSSMHost() calls = find:%d discover:%d, want find:0 discover:1", prov.findCalls, prov.discoverCalls)
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

func TestResolveSSMHostPropagatesHostnameDiscoveryError(t *testing.T) {
	wantErr := errors.New("describe instances: access denied")
	prov := &ssmDiscoveryProvider{byNameErr: wantErr}

	_, err := resolveSSMHost(context.Background(), prov, "test", nil, "kali")
	if !errors.Is(err, wantErr) {
		t.Fatalf("resolveSSMHost() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestResolveSSMHostPropagatesAttackBoxDiscoveryError(t *testing.T) {
	wantErr := errors.New("describe instances: access denied")
	prov := &ssmDiscoveryProvider{discoverErr: wantErr}

	_, err := resolveSSMHost(context.Background(), prov, "test", nil, "attack-box")
	if !errors.Is(err, wantErr) {
		t.Fatalf("resolveSSMHost() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestResolveSSMHostRejectsStoppedDiscoveryTarget(t *testing.T) {
	prov := &ssmDiscoveryProvider{byName: &provider.Instance{
		ID:    "i-kali",
		Name:  "test-goad-dreadgoad-kali",
		State: "stopped",
	}}

	_, err := resolveSSMHost(context.Background(), prov, "test", nil, "kali")
	if err == nil || !strings.Contains(err.Error(), "is stopped") {
		t.Fatalf("resolveSSMHost() error = %v, want actionable stopped-state error", err)
	}
}
