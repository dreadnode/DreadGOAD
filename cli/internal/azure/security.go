package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v8"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v9"
	"github.com/dreadnode/dreadgoad/internal/provider"
)

// SecurityCheck audits the network security posture of a deployed range.
func (p *AzureProvider) SecurityCheck(ctx context.Context, env, vpcCIDR string) ([]provider.SecurityCheckResult, error) {
	c := p.client
	if err := c.ensureSDK(ctx); err != nil {
		return nil, err
	}

	instances, err := c.DiscoverInstances(ctx, env, true)
	if err != nil {
		return nil, fmt.Errorf("discover instances: %w", err)
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("no instances found for env=%s", env)
	}

	rg := instances[0].ResourceGroup

	// Collect NIC details for every VM.
	type vmNICInfo struct {
		vmName string
		tags   map[string]string
		nics   []NICDetail
		vm     *armcompute.VirtualMachine
	}
	var vmInfos []vmNICInfo
	for _, inst := range instances {
		rid, err := arm.ParseResourceID(inst.ID)
		if err != nil {
			continue
		}
		view := armcompute.InstanceViewTypesInstanceView
		resp, err := c.vmClient.Get(ctx, rid.ResourceGroupName, rid.Name,
			&armcompute.VirtualMachinesClientGetOptions{Expand: &view})
		if err != nil {
			continue
		}
		vm := resp.VirtualMachine
		info := vmNICInfo{vmName: inst.Name, tags: inst.Tags, vm: &vm}
		if vm.Properties != nil && vm.Properties.NetworkProfile != nil {
			for _, ref := range vm.Properties.NetworkProfile.NetworkInterfaces {
				if ref == nil || ref.ID == nil {
					continue
				}
				nic, err := c.describeNIC(ctx, *ref.ID)
				if err != nil {
					info.nics = append(info.nics, NICDetail{ID: *ref.ID, Name: nicNameOf(*ref.ID)})
					continue
				}
				info.nics = append(info.nics, *nic)
			}
		}
		vmInfos = append(vmInfos, info)
	}

	// List all NSGs in the resource group.
	nsgMap := make(map[string]*armnetwork.SecurityGroup)
	pager := c.nsgClient.NewListPager(rg, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list NSGs: %w", err)
		}
		for _, nsg := range page.Value {
			if nsg != nil && nsg.ID != nil {
				nsgMap[strings.ToLower(*nsg.ID)] = nsg
			}
		}
	}

	// Check for bastion.
	var bastionFound bool
	bPager := c.bastionClient.NewListByResourceGroupPager(rg, nil)
	for bPager.More() {
		page, err := bPager.NextPage(ctx)
		if err != nil {
			break
		}
		if len(page.Value) > 0 {
			bastionFound = true
			break
		}
	}

	var results []provider.SecurityCheckResult

	// Check 1: PublicIP — no lab VM should have a public IP.
	for _, info := range vmInfos {
		role := strings.ToLower(info.tags["Role"])
		for _, nic := range info.nics {
			if nic.PublicIPID != "" {
				if role == "bastion" {
					results = append(results, provider.SecurityCheckResult{
						Name: "PublicIP", Resource: info.vmName,
						Status: "OK", Severity: "critical",
						Detail: "public IP attached (bastion — expected)",
					})
				} else {
					results = append(results, provider.SecurityCheckResult{
						Name: "PublicIP", Resource: info.vmName,
						Status: "FAIL", Severity: "critical",
						Detail: fmt.Sprintf("NIC %s has public IP %s", nic.Name, lastSegment(nic.PublicIPID)),
					})
				}
			} else {
				results = append(results, provider.SecurityCheckResult{
					Name: "PublicIP", Resource: info.vmName,
					Status: "OK", Severity: "critical",
					Detail: "no public IP attached",
				})
			}
		}
	}

	// Check 2: NSGPresent — every NIC must have an NSG (direct or via subnet).
	for _, info := range vmInfos {
		for _, nic := range info.nics {
			if nic.NSGID != "" {
				results = append(results, provider.SecurityCheckResult{
					Name: "NSGPresent", Resource: info.vmName + "/" + nic.Name,
					Status: "OK", Severity: "critical",
					Detail: "NSG associated: " + lastSegment(nic.NSGID),
				})
			} else if subnetHasNSG(nic.SubnetID, nsgMap) {
				results = append(results, provider.SecurityCheckResult{
					Name: "NSGPresent", Resource: info.vmName + "/" + nic.Name,
					Status: "OK", Severity: "critical",
					Detail: "subnet-level NSG covers this NIC",
				})
			} else {
				results = append(results, provider.SecurityCheckResult{
					Name: "NSGPresent", Resource: info.vmName + "/" + nic.Name,
					Status: "FAIL", Severity: "critical",
					Detail: "no NSG on NIC or subnet",
				})
			}
		}
	}

	// Check 3-5: NSG rule checks against each NSG in the resource group.
	for _, nsg := range nsgMap {
		name := derefStr(nsg.Name)
		rules := securityRulesOf(nsg)

		// NSGDenyAll: must have an inbound Deny rule.
		if hasDenyAllInbound(rules) {
			results = append(results, provider.SecurityCheckResult{
				Name: "NSGDenyAll", Resource: name,
				Status: "OK", Severity: "critical",
				Detail: "DenyAllInbound rule present",
			})
		} else {
			results = append(results, provider.SecurityCheckResult{
				Name: "NSGDenyAll", Resource: name,
				Status: "FAIL", Severity: "critical",
				Detail: "no DenyAllInbound rule found",
			})
		}

		// NSGNoWild: no inbound Allow rule with source * or Internet.
		wildcards := wildcardAllowRules(rules)
		if len(wildcards) == 0 {
			results = append(results, provider.SecurityCheckResult{
				Name: "NSGNoWild", Resource: name,
				Status: "OK", Severity: "high",
				Detail: "no wildcard/Internet inbound Allow rules",
			})
		} else {
			results = append(results, provider.SecurityCheckResult{
				Name: "NSGNoWild", Resource: name,
				Status: "FAIL", Severity: "high",
				Detail: fmt.Sprintf("wildcard inbound Allow: %s", strings.Join(wildcards, ", ")),
			})
		}

		// NSGInbound: inbound Allow sources should be VNet CIDR or AzureLoadBalancer.
		unexpected := unexpectedSources(rules, vpcCIDR)
		if len(unexpected) == 0 {
			results = append(results, provider.SecurityCheckResult{
				Name: "NSGInbound", Resource: name,
				Status: "OK", Severity: "high",
				Detail: "all inbound Allow sources are expected",
			})
		} else {
			results = append(results, provider.SecurityCheckResult{
				Name: "NSGInbound", Resource: name,
				Status: "WARN", Severity: "high",
				Detail: fmt.Sprintf("unexpected inbound sources: %s", strings.Join(unexpected, ", ")),
			})
		}
	}

	// Check 6: BastionExists.
	if bastionFound {
		results = append(results, provider.SecurityCheckResult{
			Name: "BastionExists", Resource: rg,
			Status: "OK", Severity: "high",
			Detail: "Azure Bastion host found",
		})
	} else {
		results = append(results, provider.SecurityCheckResult{
			Name: "BastionExists", Resource: rg,
			Status: "FAIL", Severity: "high",
			Detail: "no Azure Bastion found in resource group",
		})
	}

	// Check 7: SSHKeyAuth — Linux VMs should use SSH key auth.
	for _, info := range vmInfos {
		if info.vm == nil || info.vm.Properties == nil || info.vm.Properties.OSProfile == nil {
			continue
		}
		osProfile := info.vm.Properties.OSProfile
		if osProfile.LinuxConfiguration == nil {
			continue
		}
		linuxCfg := osProfile.LinuxConfiguration
		if linuxCfg.DisablePasswordAuthentication != nil && *linuxCfg.DisablePasswordAuthentication {
			results = append(results, provider.SecurityCheckResult{
				Name: "SSHKeyAuth", Resource: info.vmName,
				Status: "OK", Severity: "info",
				Detail: "password authentication disabled",
			})
		} else {
			results = append(results, provider.SecurityCheckResult{
				Name: "SSHKeyAuth", Resource: info.vmName,
				Status: "WARN", Severity: "info",
				Detail: "password authentication enabled on Linux VM",
			})
		}
	}

	return results, nil
}

// inboundRule is a flattened view of one NSG security rule's relevant fields.
type inboundRule struct {
	name          string
	access        string
	direction     string
	sourcePrefix  string
	priority      int32
}

func securityRulesOf(nsg *armnetwork.SecurityGroup) []inboundRule {
	if nsg == nil || nsg.Properties == nil {
		return nil
	}
	var rules []inboundRule
	for _, r := range nsg.Properties.SecurityRules {
		if r == nil || r.Properties == nil {
			continue
		}
		p := r.Properties
		if p.Direction == nil || string(*p.Direction) != "Inbound" {
			continue
		}
		priority := int32(0)
		if p.Priority != nil {
			priority = *p.Priority
		}
		rules = append(rules, inboundRule{
			name:         derefStr(r.Name),
			access:       string(*p.Access),
			direction:    string(*p.Direction),
			sourcePrefix: derefStr(p.SourceAddressPrefix),
			priority:     priority,
		})
	}
	return rules
}

func hasDenyAllInbound(rules []inboundRule) bool {
	for _, r := range rules {
		if strings.EqualFold(r.access, "Deny") && r.sourcePrefix == "*" {
			return true
		}
	}
	return false
}

func wildcardAllowRules(rules []inboundRule) []string {
	var names []string
	for _, r := range rules {
		if !strings.EqualFold(r.access, "Allow") {
			continue
		}
		src := strings.ToLower(r.sourcePrefix)
		if src == "*" || src == "internet" {
			if !strings.EqualFold(r.name, "AllowAzureLoadBalancer") {
				names = append(names, r.name)
			}
		}
	}
	return names
}

func unexpectedSources(rules []inboundRule, vpcCIDR string) []string {
	expected := map[string]bool{
		"*":                     true, // deny rules use *
		"azureloadbalancer":     true,
		strings.ToLower(vpcCIDR): true,
		"virtualnetwork":       true,
	}
	var unexpected []string
	for _, r := range rules {
		if !strings.EqualFold(r.access, "Allow") {
			continue
		}
		src := strings.ToLower(r.sourcePrefix)
		if !expected[src] {
			unexpected = append(unexpected, fmt.Sprintf("%s (rule %s)", r.sourcePrefix, r.name))
		}
	}
	return unexpected
}

func subnetHasNSG(subnetID string, nsgMap map[string]*armnetwork.SecurityGroup) bool {
	if subnetID == "" {
		return false
	}
	for _, nsg := range nsgMap {
		if nsg == nil || nsg.Properties == nil {
			continue
		}
		for _, sub := range nsg.Properties.Subnets {
			if sub != nil && sub.ID != nil && strings.EqualFold(*sub.ID, subnetID) {
				return true
			}
		}
	}
	return false
}

func lastSegment(resourceID string) string {
	parts := strings.Split(resourceID, "/")
	if len(parts) == 0 {
		return resourceID
	}
	return parts[len(parts)-1]
}

var _ provider.SecurityChecker = (*AzureProvider)(nil)
