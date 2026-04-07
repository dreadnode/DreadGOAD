package cmd

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/variant"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage deployment environments",
}

var envCreateCmd = &cobra.Command{
	Use:   "create <env-name>",
	Short: "Create a new deployment environment",
	Long: `Scaffold a new deployment environment with all required infrastructure
and configuration files.

Creates:
  - infra/goad-deployment/{env}/env.hcl
  - infra/goad-deployment/{env}/{region}/region.hcl
  - infra/goad-deployment/{env}/{region}/network/terragrunt.hcl
  - infra/goad-deployment/{env}/{region}/goad/{host}/terragrunt.hcl + templates
  - ad/GOAD/data/{env}-config.json
  - {env}-inventory (Ansible inventory with PENDING instance IDs)

Use --variant to generate randomized entity names for the environment config.
Without --variant, the base config (dev-config.json) is copied as-is.`,
	Args: cobra.ExactArgs(1),
	RunE: runEnvCreate,
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available environments",
	RunE:  runEnvList,
}

func init() {
	rootCmd.AddCommand(envCmd)
	envCmd.AddCommand(envCreateCmd)
	envCmd.AddCommand(envListCmd)

	envCreateCmd.Flags().String("region", "us-east-1", "AWS region for the environment")
	envCreateCmd.Flags().String("vpc-cidr", "", "VPC CIDR block (default: auto-assigned)")
	envCreateCmd.Flags().String("reference", "staging", "Reference environment to copy infrastructure from")
	envCreateCmd.Flags().Bool("variant", false, "Generate randomized variant config")
	envCreateCmd.Flags().Bool("force", false, "Overwrite existing environment")
}

func vpcCIDRForEnv(envName string) string {
	knownCIDRs := map[string]string{
		"dev":     "10.0.0.0/16",
		"staging": "10.1.0.0/16",
		"prod":    "10.2.0.0/16",
		"test":    "10.8.0.0/16",
	}
	if cidr, ok := knownCIDRs[envName]; ok {
		return cidr
	}
	// Generate a deterministic second octet from env name (range 10-250)
	var hash byte
	for _, c := range envName {
		hash = hash*31 + byte(c)
	}
	octet := int(hash)%240 + 10
	return fmt.Sprintf("10.%d.0.0/16", octet)
}

func runEnvCreate(cmd *cobra.Command, args []string) error {
	envName := strings.TrimSpace(args[0])
	if envName == "" {
		return fmt.Errorf("environment name cannot be empty")
	}

	cfg, err := config.Get()
	if err != nil {
		return err
	}

	region, _ := cmd.Flags().GetString("region")
	vpcCIDR, _ := cmd.Flags().GetString("vpc-cidr")
	reference, _ := cmd.Flags().GetString("reference")
	useVariant, _ := cmd.Flags().GetBool("variant")
	force, _ := cmd.Flags().GetBool("force")

	if vpcCIDR == "" {
		vpcCIDR = vpcCIDRForEnv(envName)
	}

	deployment := cfg.Infra.Deployment
	infraBase := filepath.Join(cfg.ProjectRoot, "infra", deployment)
	envDir := filepath.Join(infraBase, envName)
	regionDir := filepath.Join(envDir, region)

	if _, err := os.Stat(envDir); err == nil && !force {
		return fmt.Errorf("environment %q already exists at %s\nUse --force to overwrite", envName, envDir)
	}

	refRegionDir := findReferenceRegion(infraBase, reference)
	if refRegionDir == "" {
		return fmt.Errorf("reference environment %q not found in %s", reference, infraBase)
	}

	color.Cyan("Creating environment: %s", envName)
	fmt.Printf("  %-14s %s\n", "Region:", region)
	fmt.Printf("  %-14s %s\n", "VPC CIDR:", vpcCIDR)
	fmt.Printf("  %-14s %s\n", "Reference:", reference)
	fmt.Printf("  %-14s %v\n", "Variant:", useVariant)
	fmt.Println()

	if err := createEnvHCL(envDir, envName, vpcCIDR); err != nil {
		return fmt.Errorf("create env.hcl: %w", err)
	}
	color.Green("  Created env.hcl")

	if err := createRegionHCL(regionDir, region); err != nil {
		return fmt.Errorf("create region.hcl: %w", err)
	}
	color.Green("  Created %s/region.hcl", region)

	if err := copyInfrastructure(refRegionDir, regionDir); err != nil {
		return fmt.Errorf("copy infrastructure: %w", err)
	}
	color.Green("  Copied infrastructure from %s", reference)

	configPath := filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "data", envName+"-config.json")
	if useVariant {
		if err := generateVariantConfig(cfg.ProjectRoot, envName); err != nil {
			return fmt.Errorf("generate variant config: %w", err)
		}
		color.Green("  Generated variant config: %s-config.json", envName)
	} else {
		if err := copyBaseConfig(cfg.ProjectRoot, envName); err != nil {
			return fmt.Errorf("copy base config: %w", err)
		}
		color.Green("  Created config: %s-config.json", envName)
	}

	invPath := filepath.Join(cfg.ProjectRoot, envName+"-inventory")
	if err := generateInventory(cfg.ProjectRoot, envName, region, reference); err != nil {
		return fmt.Errorf("generate inventory: %w", err)
	}
	color.Green("  Created inventory: %s", filepath.Base(invPath))

	fmt.Println()
	color.Green("Environment %q created successfully!", envName)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Review: %s\n", envDir)
	fmt.Printf("  2. Review: %s\n", configPath)
	fmt.Printf("  3. Review: %s\n", invPath)
	fmt.Printf("  4. Initialize: dreadgoad --env %s --region %s infra init\n", envName, region)
	fmt.Printf("  5. Plan:       dreadgoad --env %s --region %s infra plan\n", envName, region)
	fmt.Printf("  6. Apply:      dreadgoad --env %s --region %s infra apply --auto-approve\n", envName, region)
	fmt.Printf("  7. Sync IDs:   dreadgoad --env %s --region %s inventory sync\n", envName, region)

	return nil
}

func runEnvList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	deployment := cfg.Infra.Deployment
	infraBase := filepath.Join(cfg.ProjectRoot, "infra", deployment)

	entries, err := os.ReadDir(infraBase)
	if err != nil {
		return fmt.Errorf("read deployment directory: %w", err)
	}

	color.Cyan("Available environments:")
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		envHCL := filepath.Join(infraBase, name, "env.hcl")
		if _, err := os.Stat(envHCL); err != nil {
			continue
		}

		var regions []string
		regionEntries, _ := os.ReadDir(filepath.Join(infraBase, name))
		for _, re := range regionEntries {
			if !re.IsDir() {
				continue
			}
			regionHCL := filepath.Join(infraBase, name, re.Name(), "region.hcl")
			if _, err := os.Stat(regionHCL); err == nil {
				regions = append(regions, re.Name())
			}
		}

		configFile := filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "data", name+"-config.json")
		hasConfig := false
		if _, err := os.Stat(configFile); err == nil {
			hasConfig = true
		}

		marker := " "
		if name == cfg.Env {
			marker = "*"
		}

		configStatus := color.RedString("no config")
		if hasConfig {
			configStatus = color.GreenString("config OK")
		}

		fmt.Printf("  %s %-12s  regions: %-20s  %s\n",
			marker, name, strings.Join(regions, ", "), configStatus)
	}

	return nil
}

func findReferenceRegion(infraBase, reference string) string {
	refDir := filepath.Join(infraBase, reference)
	entries, err := os.ReadDir(refDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		regionHCL := filepath.Join(refDir, entry.Name(), "region.hcl")
		if _, err := os.Stat(regionHCL); err == nil {
			return filepath.Join(refDir, entry.Name())
		}
	}
	return ""
}

func createEnvHCL(envDir, envName, vpcCIDR string) error {
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf(`# Set common variables for the environment.
# This is automatically pulled in by the root terragrunt.hcl configuration.
locals {
  deployment_name = "goad"           # Change to your deployment name
  aws_account_id  = get_aws_account_id()
  env             = %q
  vpc_cidr        = %q
}
`, envName, vpcCIDR)
	return os.WriteFile(filepath.Join(envDir, "env.hcl"), []byte(content), 0o644)
}

func createRegionHCL(regionDir, region string) error {
	if err := os.MkdirAll(regionDir, 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf(`locals {
  aws_region = %q
}
`, region)
	return os.WriteFile(filepath.Join(regionDir, "region.hcl"), []byte(content), 0o644)
}

func copyInfrastructure(srcRegionDir, dstRegionDir string) error {
	return filepath.WalkDir(srcRegionDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcRegionDir, path)
		if err != nil {
			return err
		}

		if strings.Contains(relPath, ".terragrunt-cache") ||
			strings.Contains(relPath, ".terraform") ||
			strings.HasSuffix(relPath, ".terraform.lock.hcl") ||
			relPath == "region.hcl" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dstPath := filepath.Join(dstRegionDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, 0o644)
	})
}

func copyBaseConfig(projectRoot, envName string) error {
	srcPath := filepath.Join(projectRoot, "ad", "GOAD", "data", "dev-config.json")
	dstPath := filepath.Join(projectRoot, "ad", "GOAD", "data", envName+"-config.json")

	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read base config: %w", err)
	}

	var parsed interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("invalid base config JSON: %w", err)
	}

	return os.WriteFile(dstPath, data, 0o644)
}

func generateInventory(projectRoot, envName, region, reference string) error {
	refInvPath := filepath.Join(projectRoot, reference+"-inventory")
	dstInvPath := filepath.Join(projectRoot, envName+"-inventory")

	data, err := os.ReadFile(refInvPath)
	if err != nil {
		return fmt.Errorf("read reference inventory %s: %w", filepath.Base(refInvPath), err)
	}
	content := string(data)

	// Extract reference env and region from the inventory vars
	envRe := regexp.MustCompile(`(?m)^(\s*env=)(.+)$`)
	regionRe := regexp.MustCompile(`(?m)^(\s*ansible_aws_ssm_region=)(.+)$`)
	bucketRe := regexp.MustCompile(`(?m)^(\s*ansible_aws_ssm_bucket_name=)(.+)$`)
	instanceRe := regexp.MustCompile(`(ansible_host=)i-[0-9a-f]+`)
	ipFieldRe := regexp.MustCompile(`\s+(?:dc_ipv4|host_ipv4)=\S+`)

	refEnv := reference
	if m := envRe.FindStringSubmatch(content); len(m) > 2 {
		refEnv = strings.TrimSpace(m[2])
	}
	refRegion := ""
	if m := regionRe.FindStringSubmatch(content); len(m) > 2 {
		refRegion = strings.TrimSpace(m[2])
	}

	// Replace env
	content = envRe.ReplaceAllString(content, "${1}"+envName)

	// Replace region
	content = regionRe.ReplaceAllString(content, "${1}"+region)

	// Replace bucket name: swap ref env/region for new env/region
	if refRegion != "" {
		if m := bucketRe.FindStringSubmatch(content); len(m) > 2 {
			oldBucket := strings.TrimSpace(m[2])
			newBucket := strings.Replace(oldBucket, refEnv+"-"+refRegion, envName+"-"+region, 1)
			content = bucketRe.ReplaceAllString(content, "${1}"+newBucket)
		}
	}

	// Replace instance IDs with PENDING placeholder
	content = instanceRe.ReplaceAllString(content, "${1}PENDING")

	// Strip dc_ipv4/host_ipv4 fields (will be populated after infra apply)
	content = ipFieldRe.ReplaceAllString(content, "")

	return os.WriteFile(dstInvPath, []byte(content), 0o644)
}

func generateVariantConfig(projectRoot, envName string) error {
	source := filepath.Join(projectRoot, "ad", "GOAD")
	target := filepath.Join(projectRoot, "ad", "GOAD-"+envName)

	gen := variant.NewGenerator(source, target, envName)
	if err := gen.Run(); err != nil {
		return fmt.Errorf("variant generation: %w", err)
	}

	srcConfig := filepath.Join(target, "data", "config.json")
	dstConfig := filepath.Join(projectRoot, "ad", "GOAD", "data", envName+"-config.json")

	data, err := os.ReadFile(srcConfig)
	if err != nil {
		return fmt.Errorf("read generated variant config: %w", err)
	}

	return os.WriteFile(dstConfig, data, 0o644)
}
