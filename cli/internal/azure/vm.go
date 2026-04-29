package azure

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Instance represents a discovered Azure VM relevant to dreadgoad.
type Instance struct {
	ID            string // full Azure resource ID (used for run-command targeting)
	Name          string // VM resource name (e.g. "test-goad-dreadgoad-kingslanding-vm")
	ResourceGroup string
	PrivateIP     string
	State         string // "running", "stopped", etc. (normalized from PowerState/* )
}

// vmListItem is the shape `az vm list -d` returns.
type vmListItem struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	ResourceGroup     string            `json:"resourceGroup"`
	PowerState        string            `json:"powerState"`
	PrivateIPs        string            `json:"privateIps"`
	Tags              map[string]string `json:"tags"`
	ProvisioningState string            `json:"provisioningState"`
}

// DiscoverInstances finds GOAD VMs for the given env (running by default).
// Mirrors the AWS provider's tag-based discovery: lab is identified by
// Project=DreadGOAD + Environment=<env> tags applied by Terraform.
func (c *Client) DiscoverInstances(ctx context.Context, env string, includeStopped bool) ([]Instance, error) {
	var raw []vmListItem
	if err := c.runJSON(ctx, &raw,
		"vm", "list", "-d", "-o", "json",
		"--query", fmt.Sprintf(
			"[?tags.Project=='DreadGOAD' && tags.Environment=='%s']", env)); err != nil {
		return nil, err
	}

	var instances []Instance
	for _, v := range raw {
		state := normalizePowerState(v.PowerState)
		if !includeStopped && state != "running" {
			continue
		}
		instances = append(instances, Instance{
			ID:            v.ID,
			Name:          v.Name,
			ResourceGroup: v.ResourceGroup,
			PrivateIP:     firstIP(v.PrivateIPs),
			State:         state,
		})
	}
	return instances, nil
}

// FindInstanceByHostname locates a VM whose ComputerName tag (or name) matches.
func (c *Client) FindInstanceByHostname(ctx context.Context, env, hostname string) (*Instance, error) {
	instances, err := c.DiscoverInstances(ctx, env, true)
	if err != nil {
		return nil, err
	}
	wanted := strings.ToUpper(hostname)
	for _, inst := range instances {
		if strings.Contains(strings.ToUpper(inst.Name), wanted) {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("instance not found for hostname %s", hostname)
}

// StartInstances starts the given VMs (by full resource IDs).
func (c *Client) StartInstances(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"vm", "start", "--ids"}, ids...)
	_, err := c.run(ctx, args...)
	return err
}

// StopInstances deallocates the given VMs (stops billing for compute).
func (c *Client) StopInstances(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"vm", "deallocate", "--ids"}, ids...)
	_, err := c.run(ctx, args...)
	return err
}

// WaitForInstanceStopped polls until the VM reaches a deallocated state.
func (c *Client) WaitForInstanceStopped(ctx context.Context, id string) error {
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
		var info struct {
			Statuses []struct {
				Code string `json:"code"`
			} `json:"statuses"`
		}
		if err := c.runJSON(ctx, &info,
			"vm", "get-instance-view", "--ids", id, "-o", "json",
			"--query", "instanceView.{statuses: statuses}"); err != nil {
			return err
		}
		for _, s := range info.Statuses {
			if s.Code == "PowerState/deallocated" || s.Code == "PowerState/stopped" {
				return nil
			}
		}
	}
	return fmt.Errorf("timed out waiting for VM %s to stop", id)
}

// DestroyInstances deletes the given VMs (does not delete NICs/disks/RG).
// For full cleanup, prefer terragrunt destroy.
func (c *Client) DestroyInstances(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"vm", "delete", "--yes", "--ids"}, ids...)
	_, err := c.run(ctx, args...)
	return err
}

// normalizePowerState maps Azure's "VM running"/"VM deallocated" strings to
// the dreadgoad-canonical lowercase verbs the rest of the CLI uses.
func normalizePowerState(s string) string {
	s = strings.TrimPrefix(s, "VM ")
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "running":
		return "running"
	case "deallocated":
		return "stopped"
	case "stopped":
		return "stopped"
	default:
		return s
	}
}

// firstIP returns the first IP in a comma-separated list (Azure CLI joins
// multiple private IPs with commas).
func firstIP(s string) string {
	if i := strings.IndexByte(s, ','); i > 0 {
		return s[:i]
	}
	return s
}
