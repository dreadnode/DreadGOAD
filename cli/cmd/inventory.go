package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	inv "github.com/dreadnode/dreadgoad/internal/inventory"
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

	jsonFile, _ := cmd.Flags().GetString("json")
	instances, err := loadInstances(context.Background(), jsonFile, invPath, cfg)
	if err != nil {
		return err
	}

	return applyInstanceUpdates(invPath, instances)
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

	parsed, err := inv.Parse(invPath)
	if err != nil {
		return nil, err
	}

	if !parsed.IsSSM() {
		// For non-SSM inventories, use the provider to discover instances.
		prov, err := cfg.NewProvider(ctx)
		if err != nil {
			return nil, fmt.Errorf("inventory sync: use --json to provide instance data manually, or configure a provider: %w", err)
		}
		provInstances, err := prov.DiscoverInstances(ctx, cfg.Env)
		if err != nil {
			return nil, fmt.Errorf("discover instances: %w", err)
		}
		var instances []instanceInfo
		for _, i := range provInstances {
			instances = append(instances, instanceInfo{InstanceID: i.ID, Name: i.Name})
		}
		return instances, nil
	}

	// SSM inventory: use provider to discover.
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return nil, err
	}
	provInstances, err := prov.DiscoverInstances(ctx, cfg.Env)
	if err != nil {
		return nil, fmt.Errorf("discover instances: %w", err)
	}
	var instances []instanceInfo
	for _, i := range provInstances {
		instances = append(instances, instanceInfo{InstanceID: i.ID, Name: i.Name})
	}
	return instances, nil
}

func applyInstanceUpdates(invPath string, instances []instanceInfo) error {
	content, err := os.ReadFile(invPath)
	if err != nil {
		return fmt.Errorf("read inventory: %w", err)
	}
	lines := string(content)
	updates := 0

	for _, inst := range instances {
		if !strings.Contains(inst.Name, "dreadgoad-") {
			continue
		}
		parts := strings.SplitN(inst.Name, "dreadgoad-", 2)
		if len(parts) < 2 {
			continue
		}
		hostname := strings.ToLower(parts[1])
		re := regexp.MustCompile(`(?mi)^(` + regexp.QuoteMeta(hostname) + `\s+ansible_host=)\S+`)
		if re.MatchString(lines) {
			newLines := re.ReplaceAllString(lines, "${1}"+inst.InstanceID)
			if newLines != lines {
				lines = newLines
				fmt.Printf("Updated %s with instance ID: %s\n", hostname, inst.InstanceID)
				updates++
			}
		}
	}

	if err := os.WriteFile(invPath, []byte(lines), 0o644); err != nil {
		return fmt.Errorf("write updated inventory: %w", err)
	}

	if updates == 0 {
		fmt.Println("No instance ID updates needed. All IDs are current.")
	} else {
		fmt.Printf("Updated %d instance IDs in %s\n", updates, invPath)
	}
	return nil
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
// This is a no-op for non-SSM inventories (e.g. Ludus, Proxmox).
func generateInstanceMapping(ctx context.Context, outputPath string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	parsed, err := inv.Parse(cfg.InventoryPath())
	if err != nil {
		return err
	}

	if !parsed.IsSSM() {
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
