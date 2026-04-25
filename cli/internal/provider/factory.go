package provider

import (
	"context"
	"fmt"
)

// ProviderName constants.
const (
	NameAWS     = "aws"
	NameProxmox = "proxmox"
	NameLudus   = "ludus"
)

// Constructor is a function that creates a Provider given the context and config values.
type Constructor func(ctx context.Context, opts ConstructorOpts) (Provider, error)

// ConstructorOpts holds the parameters needed to construct a provider.
type ConstructorOpts struct {
	Region string // AWS region or empty for non-AWS providers

	// Proxmox-specific
	ProxmoxAPIURL string
	ProxmoxUser   string
	ProxmoxPass   string
	ProxmoxNode   string
	ProxmoxPool   string

	// Ludus-specific
	LudusAPIKey           string
	LudusUseImpersonation bool

	// InventoryPath is the path to the Ansible inventory file,
	// used by providers that execute commands via Ansible ad-hoc.
	InventoryPath string
}

var registry = map[string]Constructor{}

// Register adds a provider constructor to the factory registry.
func Register(name string, ctor Constructor) {
	registry[name] = ctor
}

// New creates a provider by name using the registered constructor.
func New(ctx context.Context, name string, opts ConstructorOpts) (Provider, error) {
	ctor, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (available: %v)", name, availableProviders())
	}
	return ctor(ctx, opts)
}

func availableProviders() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	return names
}
