package config

import (
	"context"
	"fmt"
	"os"

	"github.com/dreadnode/dreadgoad/internal/provider"

	// Register provider constructors.
	_ "github.com/dreadnode/dreadgoad/internal/aws"
	_ "github.com/dreadnode/dreadgoad/internal/azure"
	_ "github.com/dreadnode/dreadgoad/internal/ludus"
	_ "github.com/dreadnode/dreadgoad/internal/proxmox"
)

// NewProvider creates a provider instance from the current configuration.
func (c *Config) NewProvider(ctx context.Context) (provider.Provider, error) {
	name := c.ResolvedProvider()

	opts := provider.ConstructorOpts{}

	switch name {
	case provider.NameAWS:
		region, err := c.ResolveRegion()
		if err != nil {
			return nil, err
		}
		opts.Region = region

	case provider.NameAzure:
		region, err := c.ResolveRegion()
		if err != nil {
			return nil, err
		}
		opts.Region = region

	case provider.NameProxmox:
		opts.ProxmoxAPIURL = c.Proxmox.APIURL
		opts.ProxmoxUser = c.Proxmox.User
		opts.ProxmoxNode = c.Proxmox.Node
		opts.ProxmoxPool = c.Proxmox.Pool
		opts.ProxmoxPass = c.Proxmox.Password
		// Allow environment variable override for the password.
		if envPass := os.Getenv("DREADGOAD_PROXMOX_PASSWORD"); envPass != "" {
			opts.ProxmoxPass = envPass
		}
		if opts.ProxmoxPass == "" {
			return nil, fmt.Errorf("proxmox password not configured: set proxmox.password in dreadgoad.yaml or export DREADGOAD_PROXMOX_PASSWORD")
		}

	case provider.NameLudus:
		opts.LudusAPIKey = c.Ludus.APIKey
		// Allow environment variable override for the API key.
		if envKey := os.Getenv("LUDUS_API_KEY"); envKey != "" {
			opts.LudusAPIKey = envKey
		}
		if opts.LudusAPIKey == "" {
			return nil, fmt.Errorf("ludus API key not configured: set ludus.api_key in dreadgoad.yaml or export LUDUS_API_KEY")
		}
		opts.LudusUseImpersonation = c.Ludus.UseImpersonation
		opts.LudusSSHHost = c.Ludus.SSHTarget()
		opts.LudusSSHUser = c.Ludus.SSHUser
		opts.LudusSSHKeyPath = c.Ludus.SSHKeyPath
		opts.LudusSSHPassword = c.Ludus.SSHPassword
		// Allow environment variable override for SSH password.
		if envPass := os.Getenv("LUDUS_SSH_PASSWORD"); envPass != "" {
			opts.LudusSSHPassword = envPass
		}
		opts.LudusSSHPort = c.Ludus.SSHPort
		opts.InventoryPath = c.InventoryPath()

	default:
		return nil, fmt.Errorf("unsupported provider %q", name)
	}

	return provider.New(ctx, name, opts)
}
