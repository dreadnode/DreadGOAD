package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	inv "github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/spf13/cobra"
)

var inventoryCmd = &cobra.Command{
	Use:   "inventory",
	Short: "Manage Ansible inventory",
}

var inventorySyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize inventory with provider instance IDs",
	RunE:  runInventorySync,
}

var inventoryShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current inventory",
	RunE:  runInventoryShow,
}

var inventoryMappingCmd = &cobra.Command{
	Use:   "mapping",
	Short: "Generate instance-to-IP mapping for Ansible optimization",
	RunE:  runInventoryMapping,
}

func init() {
	rootCmd.AddCommand(inventoryCmd)
	inventoryCmd.AddCommand(inventorySyncCmd)
	inventoryCmd.AddCommand(inventoryShowCmd)
	inventoryCmd.AddCommand(inventoryMappingCmd)

	inventorySyncCmd.Flags().Bool("backup", false, "Create backup before modifying")
	inventorySyncCmd.Flags().String("json", "", "Path to JSON file with instance data")
	inventoryMappingCmd.Flags().StringP("output", "o", "", "Output file path")
}

type instanceInfo struct {
	InstanceID string `json:"InstanceId"`
	Name       string `json:"Name"`
	PrivateIP  string `json:"PrivateIP,omitempty"`
}

func runInventorySync(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	invPath := cfg.InventoryPath()

	if err := bootstrapInventory(invPath); err != nil {
		return err
	}

	backup, _ := cmd.Flags().GetBool("backup")
	if backup {
		if err := backupInventory(invPath); err != nil {
			return err
		}
	}

	if err := updateEnvField(invPath, cfg.Env); err != nil {
		return err
	}
	if err := ensureAWSInventoryTransport(cfg); err != nil {
		return fmt.Errorf("AWS inventory transport: %w", err)
	}

	// Passwords first, and independently of the address sync below: an
	// unresolvable host makes applyInstanceUpdates return an error, and the
	// credentials are worth repairing even on a run that then reports that.
	// This is the command both preflight gates point the operator at, so it has
	// to reconcile everything the inventory gets wrong, not just addresses.
	if cfg.ResolvedProvider() == provider.NameAzure {
		if err := syncAzureInventoryPasswords(cfg); err != nil {
			return err
		}
	}

	jsonFile, _ := cmd.Flags().GetString("json")
	instances, err := loadInstances(context.Background(), jsonFile, invPath, cfg)
	if err != nil {
		return err
	}

	// Discovering nothing means the query looked in the wrong place — usually
	// the wrong region. Falling through would write the inventory back
	// unchanged and report "all values are current", a false success.
	if len(instances) == 0 {
		if region, rerr := cfg.ResolveRegion(); rerr == nil && cfg.IsAWS() {
			return fmt.Errorf("no instances found for env=%s in %s: nothing to sync", cfg.Env, region)
		}
		return fmt.Errorf("no instances found for env=%s: nothing to sync", cfg.Env)
	}

	return applyInstanceUpdatesForProvider(invPath, instances, cfg.IsAWS())
}

func backupInventory(invPath string) error {
	backupPath := invPath + ".bak." + time.Now().Format("20060102150405")
	data, err := os.ReadFile(invPath)
	if err != nil {
		return fmt.Errorf("read inventory for backup: %w", err)
	}
	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	fmt.Printf("Created backup: %s\n", backupPath)
	return nil
}

func updateEnvField(invPath, env string) error {
	data, err := os.ReadFile(invPath)
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`(?m)^(\s*env=).*$`)
	updated := re.ReplaceAllString(string(data), "${1}"+env)
	if err := os.WriteFile(invPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write inventory: %w", err)
	}
	return nil
}

func loadInstances(ctx context.Context, jsonFile, invPath string, cfg *config.Config) ([]instanceInfo, error) {
	if jsonFile != "" {
		raw, err := os.ReadFile(jsonFile)
		if err != nil {
			return nil, fmt.Errorf("read JSON: %w", err)
		}
		var instances []instanceInfo
		if err := json.Unmarshal(raw, &instances); err != nil {
			return nil, fmt.Errorf("parse instance JSON: %w", err)
		}
		return instances, nil
	}

	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("inventory sync: use --json to provide instance data manually, or configure a provider: %w", err)
	}
	provInstances, err := prov.DiscoverInstances(ctx, cfg.Env)
	if err != nil {
		return nil, fmt.Errorf("discover instances: %w", err)
	}
	return providerInstanceUpdates(provInstances), nil
}

func providerInstanceUpdates(discovered []provider.Instance) []instanceInfo {
	instances := make([]instanceInfo, 0, len(discovered))
	for _, instance := range discovered {
		instances = append(instances, instanceInfo{
			InstanceID: instance.ID,
			Name:       instance.Name,
			PrivateIP:  instance.PrivateIP,
		})
	}
	return instances
}

// extractHostRole extracts the Ansible inventory hostname from a VM name.
// Supports multiple naming conventions:
//   - AWS: "dreadgoad-dc01" -> "dc01"
//   - Azure: "A-dreadgoad-DC01-vm" -> "dc01"
//   - Ludus/Proxmox: "DG-GOAD-DC01" -> "dc01"
//
// Falls back to the last hyphen-separated segment for unknown patterns.
func extractHostRole(vmName string) string {
	lower := strings.ToLower(vmName)

	// Azure suffixes every machine name with "-vm". Left in place it yields
	// "dc01-vm", which matches no inventory host, and the sync then reports
	// "all values are current" over an inventory that is still all PENDING.
	lower = strings.TrimSuffix(lower, "-vm")

	// AWS convention: "dreadgoad-<role>". Anchored on the *last* occurrence
	// so it works regardless of what the env/deployment prefix contains.
	if i := strings.LastIndex(lower, "dreadgoad-"); i >= 0 {
		if role := lower[i+len("dreadgoad-"):]; role != "" {
			return role
		}
	}

	// Ludus/Proxmox convention: last segment is the role (e.g. "DG-GOAD-DC01" -> "dc01")
	parts := strings.Split(lower, "-")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}

	return ""
}

func applyInstanceUpdates(invPath string, instances []instanceInfo) error {
	return applyInstanceUpdatesForProvider(invPath, instances, false)
}

func applyInstanceUpdatesForProvider(invPath string, instances []instanceInfo, aws bool) error {
	addresses, err := inventoryAddressesForProvider(invPath, instances, aws)
	if err != nil {
		return err
	}
	lines, updates, err := applyInventoryAddressUpdates(invPath, addresses)
	if err != nil {
		return err
	}
	if aws {
		if err := validateAWSInventoryAddresses(invPath, addresses); err != nil {
			return err
		}
	}

	// A host still holding a placeholder is unreachable — Ansible resolves
	// ansible_host to the literal string and every play fails "unreachable".
	// Reporting "all values are current" over that state is a false success
	// that surfaces minutes later as a provisioning failure, so name it here.
	if stale := placeholderHosts(lines); len(stale) > 0 {
		return unresolvedInventoryHostsError(invPath, stale, instances)
	}

	if updates == 0 {
		fmt.Println("No inventory updates needed. All values are current.")
	} else {
		fmt.Printf("Updated %d entries in %s\n", updates, invPath)
	}
	return nil
}

func applyInventoryAddressUpdates(invPath string, addresses map[string]string) (string, int, error) {
	content, err := os.ReadFile(invPath)
	if err != nil {
		return "", 0, fmt.Errorf("read inventory: %w", err)
	}
	lines, updates := applyInventoryAddresses(string(content), addresses)
	if updates > 0 {
		if err := writeInventoryAtomically(invPath, []byte(lines)); err != nil {
			return "", 0, fmt.Errorf("write updated inventory: %w", err)
		}
	}
	return lines, updates, nil
}

func inventoryAddressesForProvider(
	invPath string,
	instances []instanceInfo,
	aws bool,
) (map[string]string, error) {
	if aws {
		parsed, err := inv.Parse(invPath)
		if err != nil {
			return nil, fmt.Errorf("parse AWS inventory before sync: %w", err)
		}
		return expectedAWSInventoryAddresses(parsed, instances)
	}

	addresses := make(map[string]string)
	for _, instance := range instances {
		hostname := extractHostRole(instance.Name)
		if hostname == "" {
			continue
		}
		address := instance.InstanceID
		if instance.PrivateIP != "" {
			address = instance.PrivateIP
		}
		addresses[hostname] = address
	}
	return addresses, nil
}

func applyInventoryAddresses(content string, addresses map[string]string) (string, int) {
	lines := content
	updates := 0
	hostnames := make([]string, 0, len(addresses))
	for hostname := range addresses {
		hostnames = append(hostnames, hostname)
	}
	sort.Strings(hostnames)
	for _, hostname := range hostnames {
		newValue := addresses[hostname]
		// Allow Ansible's valid indentation and arbitrary host-variable order.
		// \S* also repairs an explicitly blank ansible_host value.
		re := regexp.MustCompile(
			`(?mi)^(\s*` + regexp.QuoteMeta(hostname) + `\s+[^\r\n]*?\bansible_host=)\S*`,
		)
		if re.MatchString(lines) {
			newLines := re.ReplaceAllString(lines, "${1}"+newValue)
			if newLines != lines {
				lines = newLines
				fmt.Printf("Updated %s with ansible_host: %s\n", hostname, newValue)
				updates++
			}
		}
	}
	return lines, updates
}

func unresolvedInventoryHostsError(invPath string, stale []string, instances []instanceInfo) error {
	names := make([]string, 0, len(instances))
	for _, instance := range instances {
		names = append(names, instance.Name)
	}
	// Says "for these hosts", not "nothing matched": a sync routinely resolves
	// most of the inventory and leaves one host behind, and an error claiming
	// total failure would send the operator looking in the wrong place.
	return fmt.Errorf(
		"inventory %s still has placeholder ansible_host for %s — "+
			"no discovered machine name maps to those hosts\n"+
			"  discovered %d machine(s): %s",
		invPath, strings.Join(stale, ", "), len(instances), strings.Join(names, ", "))
}

type awsInventoryReconcileError struct {
	message       string
	affectedHosts map[string]bool
	global        bool
}

func (e *awsInventoryReconcileError) Error() string {
	return e.message
}

func expectedAWSInventoryAddresses(parsed *inv.Inventory, instances []instanceInfo) (map[string]string, error) {
	wantedRoles, problems := wantedAWSInventoryRoles(parsed)
	byRole, ambiguousRoles, discoveryProblems := discoveredAWSInstancesByRole(instances, wantedRoles)
	problems = append(problems, discoveryProblems...)
	affectedHosts := make(map[string]bool)
	globalProblem := len(problems) > len(discoveryProblems)
	for role := range ambiguousRoles {
		affectedHosts[role] = true
	}

	addresses := make(map[string]string, len(parsed.Hosts))
	for name, host := range parsed.Hosts {
		role := strings.ToLower(name)
		instance, exists := byRole[role]
		if !exists {
			affectedHosts[role] = true
			if !ambiguousRoles[role] {
				problems = append(problems, fmt.Sprintf("%s: no discovered instance maps to this host", name))
			}
			continue
		}

		address, err := expectedAWSHostAddress(name, host, parsed.Vars, instance)
		if err != nil {
			affectedHosts[role] = true
			problems = append(problems, err.Error())
			continue
		}
		addresses[name] = address
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return addresses, &awsInventoryReconcileError{
			message:       "cannot reconcile AWS inventory addresses: " + strings.Join(problems, "; "),
			affectedHosts: affectedHosts,
			global:        globalProblem,
		}
	}
	return addresses, nil
}

func wantedAWSInventoryRoles(parsed *inv.Inventory) (map[string]string, []string) {
	wanted := make(map[string]string, len(parsed.Hosts))
	var problems []string
	for name := range parsed.Hosts {
		role := strings.ToLower(name)
		if previous, exists := wanted[role]; exists {
			problems = append(problems, fmt.Sprintf(
				"inventory hosts %q and %q map to the same discovered role %q",
				previous, name, role,
			))
			continue
		}
		wanted[role] = name
	}
	if len(parsed.Hosts) == 0 {
		problems = append(problems, "inventory has no host definitions")
	}
	return wanted, problems
}

func discoveredAWSInstancesByRole(
	instances []instanceInfo,
	wanted map[string]string,
) (map[string]instanceInfo, map[string]bool, []string) {
	byRole := make(map[string]instanceInfo, len(wanted))
	ambiguous := make(map[string]bool)
	var problems []string
	for _, instance := range instances {
		role := extractHostRole(instance.Name)
		if _, exists := wanted[role]; role == "" || !exists {
			continue
		}
		if ambiguous[role] {
			continue
		}
		if previous, exists := byRole[role]; exists {
			problems = append(problems, fmt.Sprintf(
				"%s: discovered names %q and %q map to the same inventory host",
				role, previous.Name, instance.Name,
			))
			delete(byRole, role)
			ambiguous[role] = true
			continue
		}
		byRole[role] = instance
	}
	return byRole, ambiguous, problems
}

func expectedAWSHostAddress(
	name string,
	host *inv.Host,
	globalVars map[string]string,
	instance instanceInfo,
) (string, error) {
	connection := strings.TrimSpace(host.Connection)
	if connection == "" {
		connection = globalVars["ansible_connection"]
	}
	connection = strings.ToLower(strings.TrimSpace(connection))
	if connection == "" || strings.Contains(connection, "aws_ssm") {
		if instance.InstanceID == "" {
			return "", fmt.Errorf("%s: SSM transport requires an EC2 instance ID", name)
		}
		return instance.InstanceID, nil
	}
	if instance.PrivateIP == "" {
		return "", fmt.Errorf("%s: %s transport requires a discovered private IP", name, connection)
	}
	return instance.PrivateIP, nil
}

func validateAWSInventoryAddresses(path string, expected map[string]string) error {
	parsed, err := inv.Parse(path)
	if err != nil {
		return fmt.Errorf("parse AWS inventory after sync: %w", err)
	}
	var mismatches []string
	for name, want := range expected {
		host := parsed.HostByName(name)
		if host == nil {
			mismatches = append(mismatches, fmt.Sprintf("%s is missing", name))
			continue
		}
		if host.InstanceID != want {
			mismatches = append(mismatches, fmt.Sprintf("%s has %q, expected %q", name, host.InstanceID, want))
		}
	}
	if len(mismatches) > 0 {
		sort.Strings(mismatches)
		return fmt.Errorf("AWS inventory remains out of sync after repair: %s", strings.Join(mismatches, "; "))
	}
	return nil
}

// placeholderRe matches an inventory host line whose ansible_host is still a
// scaffolding placeholder: PENDING (written by `env create`) or an unrendered
// {{ip_range}} template from a provider inventory.
//
// Leading whitespace is allowed because Ansible accepts indented host lines,
// and a gate that misses them would let the exact failure it guards against
// through. The character class after it excludes ";" and "#" so a commented-out
// host is not reported as live; RE2 has no lookahead, so the exclusion is
// spelled into the class rather than written as (?![;#]).
//
// The trailing (\s|$) makes the value match whole-token: without it "pending"
// also matches the prefix of a real address like "pending-lab.example.com",
// blocking a run over a host that is perfectly well configured.
var placeholderRe = regexp.MustCompile(`(?mi)^\s*([^;#\s]\S*)\s+ansible_host=(pending|\{\{[^}]*\}\}\S*)(\s|$)`)

// placeholderHosts returns the inventory hostnames whose ansible_host has not
// been resolved to a real address.
func placeholderHosts(inventory string) []string {
	var out []string
	for _, m := range placeholderRe.FindAllStringSubmatch(inventory, -1) {
		out = append(out, m[1])
	}
	return out
}

func runInventoryShow(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	parsed, err := inv.Parse(cfg.InventoryPath())
	if err != nil {
		return err
	}

	displayRegion := parsed.Region()
	if displayRegion == "" {
		displayRegion = "(not set in inventory)"
	}
	fmt.Printf("Inventory: %s (env=%s, region=%s)\n\n", parsed.FilePath, cfg.Env, displayRegion)
	fmt.Printf("%-8s %-24s %-10s %-10s %s\n", "HOST", "INSTANCE ID", "DICT_KEY", "DNS_DOMAIN", "GROUPS")
	fmt.Println(strings.Repeat("-", 80))

	for _, host := range parsed.Hosts {
		groups := strings.Join(host.Groups, ",")
		fmt.Printf("%-8s %-24s %-10s %-10s %s\n",
			host.Name, host.InstanceID, host.DictKey, host.DNSDomain, groups)
	}
	return nil
}

func runInventoryMapping(cmd *cobra.Command, args []string) error {
	outputPath, _ := cmd.Flags().GetString("output")
	return generateInstanceMapping(context.Background(), outputPath)
}

// generateInstanceMapping queries the provider for instance IPs and writes the
// mapping to a JSON file that Ansible's network_discovery role uses to avoid
// slow runtime detection. If outputPath is empty, it defaults to
// /tmp/aws_instance_mapping_<env>.json.
// This is a no-op for non-AWS providers (e.g. Ludus, Proxmox).
func generateInstanceMapping(ctx context.Context, outputPath string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	if !cfg.IsAWS() {
		return nil
	}

	if outputPath == "" {
		// Use /tmp explicitly to match the hardcoded path in
		// ansible/roles/network_discovery/tasks/aws_mapping.yml.
		// os.TempDir() on macOS returns a per-user dir under /var/folders/
		// which would not match Ansible's expectation.
		outputPath = filepath.Join("/tmp", fmt.Sprintf("aws_instance_mapping_%s.json", cfg.Env))
	}

	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return err
	}

	instances, err := prov.DiscoverInstances(ctx, cfg.Env)
	if err != nil {
		return err
	}

	fmt.Printf("Querying provider for %d instance IPs...\n", len(instances))

	mapping := make(map[string]string, len(instances))
	for _, inst := range instances {
		if inst.PrivateIP != "" {
			mapping[inst.ID] = inst.PrivateIP
		}
	}

	output := map[string]interface{}{
		"instance_to_ip": mapping,
	}
	if dnsIP := vpcDNSResolver(cfg.VpcCIDR(cfg.Env)); dnsIP != "" {
		output["vpc_dns_resolver"] = dnsIP
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mapping: %w", err)
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("write mapping: %w", err)
	}

	fmt.Printf("Mapping generated: %s\n", outputPath)
	fmt.Printf("Mapped %d instances\n", len(mapping))
	return nil
}

// vpcDNSResolver returns the Amazon-provided DNS resolver IP for a VPC,
// which is always the VPC CIDR base address + 2 (e.g. 10.8.0.2 for 10.8.0.0/16).
func vpcDNSResolver(cidr string) string {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return ""
	}
	ip = ip.To4()
	if ip == nil {
		return ""
	}
	if ip[3] > 253 {
		return ""
	}
	ip[3] += 2
	return ip.String()
}
