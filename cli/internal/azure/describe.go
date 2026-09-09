package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v8"
)

// DiskDetail is one managed disk attached to a VM.
//
// Everything here comes off the VM's own StorageProfile, so describing a VM's
// disks costs no request beyond the VM Get itself. Resolving each disk through
// the Disks API would add a call per disk to surface little the profile does
// not already carry.
type DiskDetail struct {
	Name string `json:"name"`
	// "os" or "data" — the OS disk is the one the machine boots from, and it is
	// the distinction an operator is looking for first.
	Role string `json:"role"`
	// Only data disks have one; the OS disk is not addressed by LUN.
	Lun           *int32 `json:"lun,omitempty"`
	SizeGB        *int32 `json:"size_gb,omitempty"`
	StorageType   string `json:"storage_type,omitempty"`
	Caching       string `json:"caching,omitempty"`
	CreateOption  string `json:"create_option,omitempty"`
	ManagedDiskID string `json:"managed_disk_id,omitempty"`
}

// NICDetail is one network interface attached to a VM.
//
// Unlike disks, NICs are only referenced by ID on the VM, so each one costs a
// Get against the network API.
type NICDetail struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	// A NIC can hold several IP configurations; the range's VMs use one, but
	// reporting all of them avoids implying there is only ever one.
	PrivateIPs            []string `json:"private_ips"`
	SubnetID              string   `json:"subnet_id,omitempty"`
	NSGID                 string   `json:"nsg_id,omitempty"`
	MACAddress            string   `json:"mac_address,omitempty"`
	Primary               *bool    `json:"primary,omitempty"`
	AcceleratedNetworking *bool    `json:"accelerated_networking,omitempty"`
	// Set when an IP configuration references a public IP. The address itself
	// is deliberately not resolved: that needs another client and another call
	// per NIC, and these ranges reach their hosts through Bastion rather than
	// public addresses.
	PublicIPID string `json:"public_ip_id,omitempty"`
}

// InstanceDetail is the attached-resource view of one VM.
type InstanceDetail struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	ResourceGroup string       `json:"resource_group"`
	Location      string       `json:"location,omitempty"`
	VMSize        string       `json:"vm_size,omitempty"`
	PowerState    string       `json:"power_state,omitempty"`
	Disks         []DiskDetail `json:"disks"`
	NICs          []NICDetail  `json:"nics"`
}

// DescribeInstance returns the disks and network interfaces attached to one VM.
//
// Takes a full ARM resource ID rather than a hostname on purpose. The hostname
// path (FindInstanceByHostname) resolves by listing every VM in the
// subscription and substring-matching the name, which is the right trade for a
// command an operator types occasionally and the wrong one for a UI panel: the
// caller already holds the ID from discovery, so a direct Get is both cheaper
// and unambiguous.
func (c *Client) DescribeInstance(ctx context.Context, id string) (*InstanceDetail, error) {
	if err := c.ensureSDK(ctx); err != nil {
		return nil, err
	}
	rid, err := arm.ParseResourceID(id)
	if err != nil {
		return nil, fmt.Errorf("parse VM resource ID %q: %w", id, err)
	}
	// The SDK clients are built around the credential's subscription, so the ID's
	// own subscription is ignored on the wire. Without this check an ID from a
	// different subscription would quietly describe whatever VM happens to share
	// its resource group and name here — the wrong machine, reported as the right
	// one. ParseResourceID is lenient enough to yield an empty subscription, so
	// this catches malformed IDs too.
	if rid.SubscriptionID != c.SubscriptionID {
		return nil, fmt.Errorf(
			"VM %s belongs to subscription %s, but this client is authenticated to %s",
			rid.Name, rid.SubscriptionID, c.SubscriptionID)
	}

	// Expand to the instance view: without it the response describes only the
	// VM's configuration and cannot say whether the machine is actually running.
	// It is the same read against the same resource, so it costs no extra call.
	view := armcompute.InstanceViewTypesInstanceView
	resp, err := c.vmClient.Get(ctx, rid.ResourceGroupName, rid.Name,
		&armcompute.VirtualMachinesClientGetOptions{Expand: &view})
	if err != nil {
		return nil, fmt.Errorf("get VM %s: %w", rid.Name, err)
	}
	vm := resp.VirtualMachine

	detail := &InstanceDetail{
		ID:            id,
		Name:          rid.Name,
		ResourceGroup: rid.ResourceGroupName,
		Disks:         []DiskDetail{},
		NICs:          []NICDetail{},
	}
	if vm.Location != nil {
		detail.Location = *vm.Location
	}
	if vm.Properties == nil {
		return detail, nil
	}
	if vm.Properties.HardwareProfile != nil && vm.Properties.HardwareProfile.VMSize != nil {
		detail.VMSize = string(*vm.Properties.HardwareProfile.VMSize)
	}

	detail.PowerState = powerStateOf(vm.Properties.InstanceView)
	detail.Disks = disksFromProfile(vm.Properties.StorageProfile)

	if vm.Properties.NetworkProfile != nil {
		for _, ref := range vm.Properties.NetworkProfile.NetworkInterfaces {
			if ref == nil || ref.ID == nil {
				continue
			}
			nic, err := c.describeNIC(ctx, *ref.ID)
			if err != nil {
				// One unreadable NIC should not blank the whole panel — the
				// disks and the other interfaces are still worth showing.
				detail.NICs = append(detail.NICs, NICDetail{ID: *ref.ID, Name: nicNameOf(*ref.ID)})
				continue
			}
			detail.NICs = append(detail.NICs, *nic)
		}
	}
	return detail, nil
}

// powerStateOf pulls the running state out of a VM's instance view.
//
// Azure reports it as one status among several ("PowerState/running" alongside
// "ProvisioningState/succeeded"), so the code is matched on its prefix rather
// than by position — the order is not contractual. Empty when the instance view
// is absent, which omits the field rather than claiming the VM is off.
//
// Runs the result through normalizePowerState, the same mapping ListInstances
// applies. Without it Azure's "deallocated" would reach the panel verbatim while
// the graph node beside it reads "stopped" for that very machine.
func powerStateOf(view *armcompute.VirtualMachineInstanceView) string {
	if view == nil {
		return ""
	}
	const prefix = "PowerState/"
	for _, st := range view.Statuses {
		if st == nil || st.Code == nil {
			continue
		}
		if code := *st.Code; strings.HasPrefix(code, prefix) {
			return normalizePowerState(strings.TrimPrefix(code, prefix))
		}
	}
	return ""
}

func nicNameOf(id string) string {
	if rid, err := arm.ParseResourceID(id); err == nil {
		return rid.Name
	}
	return id
}

func (c *Client) describeNIC(ctx context.Context, nicID string) (*NICDetail, error) {
	rid, err := arm.ParseResourceID(nicID)
	if err != nil {
		return nil, fmt.Errorf("parse NIC resource ID %q: %w", nicID, err)
	}
	resp, err := c.nicClient.Get(ctx, rid.ResourceGroupName, rid.Name, nil)
	if err != nil {
		return nil, fmt.Errorf("get NIC %s: %w", rid.Name, err)
	}
	out := &NICDetail{Name: rid.Name, ID: nicID, PrivateIPs: []string{}}
	props := resp.Properties
	if props == nil {
		return out, nil
	}
	out.MACAddress = derefStr(props.MacAddress)
	out.Primary = props.Primary
	out.AcceleratedNetworking = props.EnableAcceleratedNetworking
	if props.NetworkSecurityGroup != nil {
		out.NSGID = derefStr(props.NetworkSecurityGroup.ID)
	}
	for _, cfg := range props.IPConfigurations {
		if cfg == nil || cfg.Properties == nil {
			continue
		}
		if ip := derefStr(cfg.Properties.PrivateIPAddress); ip != "" {
			out.PrivateIPs = append(out.PrivateIPs, ip)
		}
		if out.SubnetID == "" && cfg.Properties.Subnet != nil {
			out.SubnetID = derefStr(cfg.Properties.Subnet.ID)
		}
		if out.PublicIPID == "" && cfg.Properties.PublicIPAddress != nil {
			out.PublicIPID = derefStr(cfg.Properties.PublicIPAddress.ID)
		}
	}
	return out, nil
}

// ptrString renders an optional SDK enum (CachingTypes, DiskCreateOptionTypes)
// as a plain string, empty when unset.
func ptrString[T ~string](v *T) string {
	if v == nil {
		return ""
	}
	return string(*v)
}

// disksFromProfile reads the OS and data disks off a VM's storage profile.
// Split out so the mapping can be tested without an ARM transport.
func disksFromProfile(sp *armcompute.StorageProfile) []DiskDetail {
	out := []DiskDetail{}
	if sp == nil {
		return out
	}
	if d := sp.OSDisk; d != nil {
		out = append(out, diskDetail(d.Name, "os", nil, d.DiskSizeGB, d.ManagedDisk,
			ptrString(d.Caching), ptrString(d.CreateOption)))
	}
	for _, d := range sp.DataDisks {
		if d == nil {
			continue
		}
		out = append(out, diskDetail(d.Name, "data", d.Lun, d.DiskSizeGB, d.ManagedDisk,
			ptrString(d.Caching), ptrString(d.CreateOption)))
	}
	return out
}

func diskDetail(
	name *string, role string, lun, sizeGB *int32,
	managed *armcompute.ManagedDiskParameters, caching, createOption string,
) DiskDetail {
	d := DiskDetail{
		Name:         derefStr(name),
		Role:         role,
		Lun:          lun,
		SizeGB:       sizeGB,
		Caching:      caching,
		CreateOption: createOption,
	}
	if managed != nil {
		d.ManagedDiskID = derefStr(managed.ID)
		if managed.StorageAccountType != nil {
			d.StorageType = string(*managed.StorageAccountType)
		}
	}
	return d
}
