package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	daws "github.com/dreadnode/dreadgoad/internal/aws"
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
	Short: "Synchronize inventory with AWS instance IDs",
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

func runInventorySync(cmd *cobra.Command, args []string) error {
	cfg := config.Get()
	ctx := context.Background()
	invPath := cfg.InventoryPath()

	if _, err := os.Stat(invPath); os.IsNotExist(err) {
		return fmt.Errorf("inventory file not found: %s", invPath)
	}

	backup, _ := cmd.Flags().GetBool("backup")
	jsonFile, _ := cmd.Flags().GetString("json")

	// Create backup if requested
	if backup {
		backupPath := invPath + ".bak." + time.Now().Format("20060102150405")
		data, _ := os.ReadFile(invPath)
		os.WriteFile(backupPath, data, 0o644)
		fmt.Printf("Created backup: %s\n", backupPath)
	}

	// Update env= field
	data, err := os.ReadFile(invPath)
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`(?m)^(\s*env=).*$`)
	updated := re.ReplaceAllString(string(data), "${1}"+cfg.Env)
	os.WriteFile(invPath, []byte(updated), 0o644)

	// Get instance data
	type instanceInfo struct {
		InstanceID string `json:"InstanceId"`
		Name       string `json:"Name"`
	}

	var instances []instanceInfo

	if jsonFile != "" {
		raw, err := os.ReadFile(jsonFile)
		if err != nil {
			return fmt.Errorf("read JSON: %w", err)
		}
		json.Unmarshal(raw, &instances)
	} else {
		// Fetch from AWS
		parsed, err := inv.Parse(invPath)
		if err != nil {
			return err
		}
		region := parsed.Region()

		client, err := daws.NewClient(ctx, region)
		if err != nil {
			return err
		}

		awsInstances, err := client.DiscoverInstances(ctx, cfg.Env)
		if err != nil {
			return fmt.Errorf("discover instances: %w", err)
		}

		for _, i := range awsInstances {
			instances = append(instances, instanceInfo{InstanceID: i.InstanceID, Name: i.Name})
		}
	}

	// Update inventory file
	content, _ := os.ReadFile(invPath)
	lines := string(content)
	updates := 0

	for _, inst := range instances {
		if !strings.Contains(inst.Name, "dreadgoad-") {
			continue
		}
		// Extract hostname from name (after "dreadgoad-")
		parts := strings.SplitN(inst.Name, "dreadgoad-", 2)
		if len(parts) < 2 {
			continue
		}
		hostname := strings.ToLower(parts[1])

		// Replace ansible_host= for this server
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

	os.WriteFile(invPath, []byte(lines), 0o644)

	if updates == 0 {
		fmt.Println("No instance ID updates needed. All IDs are current.")
	} else {
		fmt.Printf("Updated %d instance IDs in %s\n", updates, invPath)
	}
	return nil
}

func runInventoryShow(cmd *cobra.Command, args []string) error {
	cfg := config.Get()

	parsed, err := inv.Parse(cfg.InventoryPath())
	if err != nil {
		return err
	}

	fmt.Printf("Inventory: %s (env=%s, region=%s)\n\n", parsed.FilePath, cfg.Env, parsed.Region())
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
	cfg := config.Get()
	ctx := context.Background()

	parsed, err := inv.Parse(cfg.InventoryPath())
	if err != nil {
		return err
	}

	outputPath, _ := cmd.Flags().GetString("output")
	if outputPath == "" {
		outputPath = filepath.Join(os.TempDir(), fmt.Sprintf("aws_instance_mapping_%s.json", cfg.Env))
	}

	client, err := daws.NewClient(ctx, parsed.Region())
	if err != nil {
		return err
	}

	instanceIDs := parsed.InstanceIDs()
	fmt.Printf("Querying AWS for %d instance IPs...\n", len(instanceIDs))

	mapping, err := client.GetInstancePrivateIPs(ctx, instanceIDs)
	if err != nil {
		return err
	}

	output := map[string]interface{}{
		"instance_to_ip": mapping,
	}
	data, _ := json.MarshalIndent(output, "", "  ")
	os.WriteFile(outputPath, data, 0o644)

	fmt.Printf("Mapping generated: %s\n", outputPath)
	fmt.Printf("Mapped %d instances\n", len(mapping))
	return nil
}
