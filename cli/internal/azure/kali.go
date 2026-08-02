package azure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KaliKeyPath derives the ephemeral private-key path the terraform-azure-kali
// module writes. VM names follow "{env}-{deployment}-kali-vm"; the module
// writes to "~/.dreadgoad/keys/azure-{env}-{deployment}-kali".
// Returns "" if the key file does not exist.
func KaliKeyPath(env, vmName string) string {
	deployment := strings.TrimSuffix(strings.TrimPrefix(vmName, env+"-"), "-kali-vm")
	if deployment == "" || deployment == vmName {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".dreadgoad", "keys", fmt.Sprintf("azure-%s-%s-kali", env, deployment))
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// DiscoverKali finds the Kali attack box VM for the given environment by
// looking for a running instance with the Role=AttackBox tag.
func (c *Client) DiscoverKali(ctx context.Context, env string) (*Instance, error) {
	instances, err := c.DiscoverInstances(ctx, env, false)
	if err != nil {
		return nil, fmt.Errorf("discover instances: %w", err)
	}
	for _, inst := range instances {
		if inst.Tags["Role"] == "AttackBox" {
			return &inst, nil
		}
	}
	return nil, nil
}
