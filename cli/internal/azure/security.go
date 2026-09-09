package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v8"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v10"
	"github.com/dreadnode/dreadgoad/internal/provider"
)

type vmNICInfo struct {
	vmName string
	tags   map[string]string
	nics   []NICDetail
	vm     *armcompute.VirtualMachine
}

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
	vmInfos := collectVMNICInfo(ctx, c, instances)
	nsgMap, err := listSecurityGroups(ctx, c, rg)
	if err != nil {
		return nil, err
	}

	results := publicIPChecks(vmInfos)
	results = append(results, nsgPresenceChecks(vmInfos, nsgMap)...)
	results = append(results, nsgRuleChecks(nsgMap, vpcCIDR)...)
	results = append(results, bastionCheck(rg, bastionExists(ctx, c, rg)))
	results = append(results, sshKeyAuthChecks(vmInfos)...)
	return results, nil
}

func collectVMNICInfo(ctx context.Context, c *Client, instances []Instance) []vmNICInfo {
	var vmInfos []vmNICInfo
	for _, instance := range instances {
		rid, err := arm.ParseResourceID(instance.ID)
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
		info := vmNICInfo{vmName: instance.Name, tags: instance.Tags, vm: &vm}
		if vm.Properties != nil && vm.Properties.NetworkProfile != nil {
			info.nics = collectNICDetails(ctx, c, vm.Properties.NetworkProfile.NetworkInterfaces)
		}
		vmInfos = append(vmInfos, info)
	}
	return vmInfos
}

func collectNICDetails(ctx context.Context, c *Client, refs []*armcompute.NetworkInterfaceReference) []NICDetail {
	var nics []NICDetail
	for _, ref := range refs {
		if ref == nil || ref.ID == nil {
			continue
		}
		nic, err := c.describeNIC(ctx, *ref.ID)
		if err != nil {
			nics = append(nics, NICDetail{ID: *ref.ID, Name: nicNameOf(*ref.ID)})
			continue
		}
		nics = append(nics, *nic)
	}
	return nics
}

func listSecurityGroups(ctx context.Context, c *Client, resourceGroup string) (map[string]*armnetwork.SecurityGroup, error) {
	nsgs := make(map[string]*armnetwork.SecurityGroup)
	pager := c.nsgClient.NewListPager(resourceGroup, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list NSGs: %w", err)
		}
		for _, nsg := range page.Value {
			if nsg != nil && nsg.ID != nil {
				nsgs[strings.ToLower(*nsg.ID)] = nsg
			}
		}
	}
	return nsgs, nil
}

func bastionExists(ctx context.Context, c *Client, resourceGroup string) bool {
	pager := c.bastionClient.NewListByResourceGroupPager(resourceGroup, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return false
		}
		if len(page.Value) > 0 {
			return true
		}
	}
	return false
}

func publicIPChecks(vmInfos []vmNICInfo) []provider.SecurityCheckResult {
	var results []provider.SecurityCheckResult
	for _, info := range vmInfos {
		role := strings.ToLower(info.tags["Role"])
		for _, nic := range info.nics {
			switch {
			case nic.PublicIPID == "":
				results = append(results, securityResult(
					"PublicIP", info.vmName, "OK", "critical", "no public IP attached"))
			case role == "bastion":
				results = append(results, securityResult(
					"PublicIP", info.vmName, "OK", "critical", "public IP attached (bastion — expected)"))
			default:
				results = append(results, securityResult(
					"PublicIP", info.vmName, "FAIL", "critical",
					fmt.Sprintf("NIC %s has public IP %s", nic.Name, lastSegment(nic.PublicIPID))))
			}
		}
	}
	return results
}

func nsgPresenceChecks(
	vmInfos []vmNICInfo,
	nsgMap map[string]*armnetwork.SecurityGroup,
) []provider.SecurityCheckResult {
	var results []provider.SecurityCheckResult
	for _, info := range vmInfos {
		for _, nic := range info.nics {
			resource := info.vmName + "/" + nic.Name
			switch {
			case nic.NSGID != "":
				results = append(results, securityResult(
					"NSGPresent", resource, "OK", "critical", "NSG associated: "+lastSegment(nic.NSGID)))
			case subnetHasNSG(nic.SubnetID, nsgMap):
				results = append(results, securityResult(
					"NSGPresent", resource, "OK", "critical", "subnet-level NSG covers this NIC"))
			default:
				results = append(results, securityResult(
					"NSGPresent", resource, "FAIL", "critical", "no NSG on NIC or subnet"))
			}
		}
	}
	return results
}

func nsgRuleChecks(
	nsgMap map[string]*armnetwork.SecurityGroup,
	vpcCIDR string,
) []provider.SecurityCheckResult {
	var results []provider.SecurityCheckResult
	for _, nsg := range nsgMap {
		name := derefStr(nsg.Name)
		rules := securityRulesOf(nsg)
		results = append(results,
			denyAllCheck(name, rules),
			wildcardCheck(name, rules),
			inboundSourceCheck(name, rules, vpcCIDR),
		)
	}
	return results
}

func denyAllCheck(resource string, rules []inboundRule) provider.SecurityCheckResult {
	if hasDenyAllInbound(rules) {
		return securityResult("NSGDenyAll", resource, "OK", "critical", "DenyAllInbound rule present")
	}
	return securityResult("NSGDenyAll", resource, "FAIL", "critical", "no DenyAllInbound rule found")
}

func wildcardCheck(resource string, rules []inboundRule) provider.SecurityCheckResult {
	wildcards := wildcardAllowRules(rules)
	if len(wildcards) == 0 {
		return securityResult("NSGNoWild", resource, "OK", "high", "no wildcard/Internet inbound Allow rules")
	}
	return securityResult("NSGNoWild", resource, "FAIL", "high",
		fmt.Sprintf("wildcard inbound Allow: %s", strings.Join(wildcards, ", ")))
}

func inboundSourceCheck(resource string, rules []inboundRule, vpcCIDR string) provider.SecurityCheckResult {
	unexpected := unexpectedSources(rules, vpcCIDR)
	if len(unexpected) == 0 {
		return securityResult("NSGInbound", resource, "OK", "high", "all inbound Allow sources are expected")
	}
	return securityResult("NSGInbound", resource, "WARN", "high",
		fmt.Sprintf("unexpected inbound sources: %s", strings.Join(unexpected, ", ")))
}

func bastionCheck(resource string, found bool) provider.SecurityCheckResult {
	if found {
		return securityResult("BastionExists", resource, "OK", "high", "Azure Bastion host found")
	}
	return securityResult("BastionExists", resource, "FAIL", "high", "no Azure Bastion found in resource group")
}

func sshKeyAuthChecks(vmInfos []vmNICInfo) []provider.SecurityCheckResult {
	var results []provider.SecurityCheckResult
	for _, info := range vmInfos {
		if info.vm == nil || info.vm.Properties == nil || info.vm.Properties.OSProfile == nil {
			continue
		}
		linuxCfg := info.vm.Properties.OSProfile.LinuxConfiguration
		if linuxCfg == nil {
			continue
		}
		if linuxCfg.DisablePasswordAuthentication != nil && *linuxCfg.DisablePasswordAuthentication {
			results = append(results, securityResult(
				"SSHKeyAuth", info.vmName, "OK", "info", "password authentication disabled"))
			continue
		}
		results = append(results, securityResult(
			"SSHKeyAuth", info.vmName, "WARN", "info", "password authentication enabled on Linux VM"))
	}
	return results
}

func securityResult(name, resource, status, severity, detail string) provider.SecurityCheckResult {
	return provider.SecurityCheckResult{
		Name: name, Resource: resource, Status: status, Severity: severity, Detail: detail,
	}
}

// inboundRule is a flattened view of one NSG security rule's relevant fields.
type inboundRule struct {
	name         string
	access       string
	direction    string
	sourcePrefix string
	priority     int32
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
		if p.Direction == nil || p.Access == nil || string(*p.Direction) != "Inbound" {
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
		"*":                      true, // deny rules use *
		"azureloadbalancer":      true,
		strings.ToLower(vpcCIDR): true,
		"virtualnetwork":         true,
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
