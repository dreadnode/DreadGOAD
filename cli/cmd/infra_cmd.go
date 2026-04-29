package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/ludus"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/dreadnode/dreadgoad/internal/terraform"
	"github.com/dreadnode/dreadgoad/internal/terragrunt"
	"github.com/dreadnode/dreadgoad/internal/tfrender"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var infraCmd = &cobra.Command{
	Use:   "infra",
	Short: "Manage DreadGOAD infrastructure",
	Long: `Manage the DreadGOAD lab infrastructure lifecycle.

For AWS (provider: aws): uses Terragrunt to manage VPC, EC2 instances, etc.
For Proxmox (provider: proxmox): uses Terraform with the bpg/proxmox provider
to clone VMs from templates.
For Ludus (provider: ludus): uses the Ludus CLI to manage ranges and VMs.

By default, commands operate on all modules (run-all). Use --module to
target a specific module (e.g. network, goad/dc01).`,
}

var infraInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize infrastructure modules",
	RunE:  runInfraAction("init"),
}

var infraPlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Plan infrastructure changes",
	RunE:  runInfraAction("plan"),
}

var infraApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply infrastructure changes",
	RunE:  runInfraAction("apply"),
}

var infraDestroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy infrastructure",
	RunE:  runInfraAction("destroy"),
}

var infraOutputCmd = &cobra.Command{
	Use:   "output",
	Short: "Show infrastructure outputs (JSON)",
	RunE:  runInfraOutput,
}

var infraValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate environment configuration",
	RunE:  runInfraValidate,
}

func init() {
	rootCmd.AddCommand(infraCmd)
	infraCmd.AddCommand(infraInitCmd)
	infraCmd.AddCommand(infraPlanCmd)
	infraCmd.AddCommand(infraApplyCmd)
	infraCmd.AddCommand(infraDestroyCmd)
	infraCmd.AddCommand(infraOutputCmd)
	infraCmd.AddCommand(infraValidateCmd)

	for _, cmd := range []*cobra.Command{infraInitCmd, infraPlanCmd, infraApplyCmd, infraDestroyCmd, infraOutputCmd} {
		cmd.Flags().StringP("module", "m", "", "Target module path (e.g. network, goad/dc01)")
		cmd.Flags().String("exclude", "", "Exclude modules (comma-separated, e.g. goad/dc01,goad/dc02)")
	}

	infraApplyCmd.Flags().Bool("auto-approve", false, "Skip confirmation prompt")
	infraApplyCmd.Flags().Bool("individual", false, "Apply each subdirectory individually (for module groups like goad/)")
	infraDestroyCmd.Flags().Bool("auto-approve", false, "Skip confirmation prompt")

	infraCmd.PersistentFlags().StringP("deployment", "d", "", "Deployment name (default: from config)")
}

// materializeLabConfig ensures the merged lab config JSON exists at the path
// terragrunt HCL expects (ad/GOAD/data/{env}-config.json). When an overlay
// file exists, the base config.json is merged with the overlay and written
// to disk so that terragrunt's file() function can read it directly.
func materializeLabConfig(cfg *config.Config) error {
	resolved, err := cfg.ResolvedLabConfigPath()
	if err != nil {
		return nil // no config to materialize -- let terragrunt surface the error
	}

	dataDir := filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "data")
	expected := filepath.Join(dataDir, cfg.Env+"-config.json")

	if resolved == expected {
		return nil // already in the right place (legacy layout)
	}

	// Read the resolved (merged) config and write it where terragrunt expects.
	data, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("read resolved config: %w", err)
	}
	return os.WriteFile(expected, data, 0o644)
}

func runInfraAction(action string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Get()
		if err != nil {
			return err
		}

		switch cfg.ResolvedProvider() {
		case "aws":
			return runInfraActionAWS(cmd, cfg, action)
		case "ludus":
			return runInfraActionLudus(cmd, cfg, action)
		default:
			return runInfraActionTerraform(cmd, cfg, action)
		}
	}
}

// runInfraActionAWS handles infra commands for AWS via Terragrunt.
func runInfraActionAWS(cmd *cobra.Command, cfg *config.Config, action string) error {
	if err := materializeLabConfig(cfg); err != nil {
		return fmt.Errorf("materialize lab config: %w", err)
	}

	module, _ := cmd.Flags().GetString("module")
	exclude, _ := cmd.Flags().GetString("exclude")
	deployment := resolveDeployment(cmd, cfg)

	region, err := cfg.ResolveRegion()
	if err != nil {
		return err
	}

	opts := terragrunt.Options{
		Action:           action,
		TerragruntBinary: cfg.Infra.TerragruntBinary,
		TerraformBinary:  cfg.Infra.TerraformBinary,
		NonInteractive:   true,
		ExcludeDirs:      exclude,
		Debug:            cfg.Debug,
	}

	if action == "apply" || action == "destroy" {
		autoApprove, _ := cmd.Flags().GetBool("auto-approve")
		opts.AutoApprove = autoApprove
	}

	basePath := filepath.Join(cfg.ProjectRoot, "infra", deployment)
	workDir := filepath.Join(basePath, cfg.Env, region)

	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		return fmt.Errorf("infra working directory not found: %s\nRun 'dreadgoad infra validate' to check your setup", workDir)
	}

	opts.LogFile = infraLogPath(cfg, action, deployment, module)

	fmt.Printf("Infra %s [AWS/Terragrunt] (%s/%s)\n", action, cfg.Env, region)
	if module != "" {
		fmt.Printf("Module: %s\n", module)
	}
	fmt.Printf("Log: %s\n\n", opts.LogFile)

	ctx := context.Background()

	if module != "" {
		modulePath := filepath.Join(workDir, module)
		if _, err := os.Stat(modulePath); os.IsNotExist(err) {
			return fmt.Errorf("module not found: %s", modulePath)
		}

		if action == "apply" {
			individual, _ := cmd.Flags().GetBool("individual")
			if individual {
				var excludeList []string
				if exclude != "" {
					excludeList = strings.Split(exclude, ",")
				}
				results, err := terragrunt.RunIndividual(ctx, opts, modulePath, excludeList)
				if err != nil {
					return err
				}
				return printIndividualResults(results)
			}
		}

		opts.WorkDir = modulePath
		return terragrunt.Run(ctx, opts)
	}

	opts.WorkDir = workDir
	return terragrunt.RunAll(ctx, opts)
}

// runInfraActionTerraform handles infra commands for Proxmox (and other
// template-based providers) via direct Terraform.
func runInfraActionTerraform(cmd *cobra.Command, cfg *config.Config, action string) error {
	providerName := cfg.ResolvedProvider()
	workDir := cfg.ProxmoxWorkDir()

	// For init/apply/plan, render templates first.
	if action != "destroy" {
		if err := renderProxmoxTemplates(cfg, workDir); err != nil {
			return fmt.Errorf("render templates: %w", err)
		}
	} else {
		// For destroy, the workdir must already exist.
		if _, err := os.Stat(workDir); os.IsNotExist(err) {
			return fmt.Errorf("no infrastructure found at %s; nothing to destroy", workDir)
		}
	}

	opts := terraform.Options{
		Action:          action,
		WorkDir:         workDir,
		TerraformBinary: cfg.Infra.TerraformBinary,
		Debug:           cfg.Debug,
		LogFile:         infraLogPath(cfg, action, providerName, ""),
	}

	if action == "apply" || action == "destroy" {
		autoApprove, _ := cmd.Flags().GetBool("auto-approve")
		opts.AutoApprove = autoApprove
	}

	// Pass the password as a var so it's not stored in rendered files.
	password := cfg.Proxmox.Password
	if envPass := os.Getenv("DREADGOAD_PROXMOX_PASSWORD"); envPass != "" {
		password = envPass
	}
	if password != "" {
		opts.Vars = append(opts.Vars, "pm_password="+password)
	}

	fmt.Printf("Infra %s [%s/Terraform] (%s)\n", action, providerName, cfg.Env)
	fmt.Printf("Work dir: %s\n", workDir)
	fmt.Printf("Lab: %s\n", cfg.ProxmoxLab())
	fmt.Printf("Log: %s\n\n", opts.LogFile)

	ctx := context.Background()
	return terraform.Run(ctx, opts)
}

// renderProxmoxTemplates renders the Terraform templates for Proxmox.
func renderProxmoxTemplates(cfg *config.Config, workDir string) error {
	labName := cfg.ProxmoxLab()

	renderOpts := tfrender.RenderOptions{
		ProjectRoot:   cfg.ProjectRoot,
		LabName:       labName,
		Provider:      cfg.ResolvedProvider(),
		IPRange:       cfg.Proxmox.IPRange,
		LabIdentifier: fmt.Sprintf("%s-%s", strings.ToLower(labName), cfg.Env),
		OutputDir:     workDir,
		Proxmox: tfrender.ProxmoxConfig{
			APIURL:        cfg.Proxmox.APIURL,
			User:          cfg.Proxmox.User,
			Node:          cfg.Proxmox.Node,
			Pool:          cfg.Proxmox.Pool,
			FullClone:     cfg.Proxmox.FullClone,
			Storage:       cfg.Proxmox.Storage,
			VLAN:          cfg.Proxmox.VLAN,
			NetworkBridge: cfg.Proxmox.NetworkBridge,
			NetworkModel:  cfg.Proxmox.NetworkModel,
			TemplateIDs:   cfg.Proxmox.TemplateIDs,
		},
	}

	return tfrender.Render(renderOpts)
}

func runInfraOutput(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	if cfg.ResolvedProvider() == "ludus" {
		// Ludus outputs range status instead of terraform output.
		ctx := context.Background()
		prov, err := cfg.NewProvider(ctx)
		if err != nil {
			return err
		}
		instances, err := prov.DiscoverAllInstances(ctx, cfg.Env)
		if err != nil {
			return err
		}
		fmt.Printf("Ludus Range VMs (%s):\n", cfg.Env)
		for _, inst := range instances {
			fmt.Printf("  %-6s  %-20s  %-15s  %s\n", inst.ID, inst.Name, inst.PrivateIP, inst.State)
		}
		return nil
	}

	if cfg.ResolvedProvider() != "aws" {
		// Direct terraform output for Proxmox and other providers.
		workDir := cfg.ProxmoxWorkDir()
		if _, err := os.Stat(workDir); os.IsNotExist(err) {
			return fmt.Errorf("no infrastructure found at %s", workDir)
		}
		opts := terraform.Options{
			WorkDir:         workDir,
			TerraformBinary: cfg.Infra.TerraformBinary,
		}
		out, err := terraform.Output(context.Background(), opts)
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}

	module, _ := cmd.Flags().GetString("module")
	deployment := resolveDeployment(cmd, cfg)

	region, err := cfg.ResolveRegion()
	if err != nil {
		return err
	}

	workDir := filepath.Join(cfg.ProjectRoot, "infra", deployment, cfg.Env, region)
	if module != "" {
		workDir = filepath.Join(workDir, module)
	}

	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		return fmt.Errorf("directory not found: %s", workDir)
	}

	opts := terragrunt.Options{
		Action:           "output",
		WorkDir:          workDir,
		TerragruntBinary: cfg.Infra.TerragruntBinary,
		TerraformBinary:  cfg.Infra.TerraformBinary,
	}

	ctx := context.Background()
	out, err := terragrunt.Output(ctx, opts)
	if err != nil {
		return err
	}

	fmt.Println(string(out))
	return nil
}

func runInfraValidate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	switch cfg.ResolvedProvider() {
	case "ludus":
		return runInfraValidateLudus(cfg)
	case "proxmox":
		return runInfraValidateProxmox(cfg)
	}

	deployment := resolveDeployment(cmd, cfg)

	region, err := cfg.ResolveRegion()
	if err != nil {
		return err
	}

	basePath := filepath.Join(cfg.ProjectRoot, "infra", deployment)
	result := terragrunt.ValidateEnvironment(basePath, cfg.Env, region)
	terragrunt.PrintValidationResult(result, cfg.Env, region)

	if !result.OK() {
		return fmt.Errorf("validation failed")
	}
	return nil
}

func runInfraValidateProxmox(cfg *config.Config) error {
	providerName := cfg.ResolvedProvider()
	labName := cfg.ProxmoxLab()

	fmt.Printf("Validating %s infrastructure configuration...\n\n", providerName)

	checks := []struct {
		name string
		ok   bool
		msg  string
	}{
		{
			name: "Proxmox API URL",
			ok:   cfg.Proxmox.APIURL != "",
			msg:  cfg.Proxmox.APIURL,
		},
		{
			name: "Proxmox user",
			ok:   cfg.Proxmox.User != "",
			msg:  cfg.Proxmox.User,
		},
		{
			name: "Proxmox password",
			ok:   cfg.Proxmox.Password != "" || os.Getenv("DREADGOAD_PROXMOX_PASSWORD") != "",
			msg:  "(set via config or DREADGOAD_PROXMOX_PASSWORD)",
		},
		{
			name: "Proxmox node",
			ok:   cfg.Proxmox.Node != "",
			msg:  cfg.Proxmox.Node,
		},
		{
			name: "IP range",
			ok:   cfg.Proxmox.IPRange != "",
			msg:  cfg.Proxmox.IPRange,
		},
		{
			name: "Lab provider directory",
			ok:   dirExists(filepath.Join(cfg.ProjectRoot, "ad", labName, "providers", providerName)),
			msg:  filepath.Join("ad", labName, "providers", providerName),
		},
		{
			name: "Template provider directory",
			ok:   dirExists(filepath.Join(cfg.ProjectRoot, "template", "provider", providerName)),
			msg:  filepath.Join("template", "provider", providerName),
		},
	}

	allOK := true
	for _, c := range checks {
		if c.ok {
			color.Green("  OK  %s: %s", c.name, c.msg)
		} else {
			color.Red("  FAIL %s: %s", c.name, c.msg)
			allOK = false
		}
	}

	// Check terraform binary.
	tfBin := cfg.Infra.TerraformBinary
	if tfBin == "" {
		tfBin = "tofu"
	}
	if _, err := exec.LookPath(tfBin); err != nil {
		color.Red("  FAIL Terraform binary: %s not found in PATH", tfBin)
		allOK = false
	} else {
		color.Green("  OK  Terraform binary: %s", tfBin)
	}

	fmt.Println()
	if !allOK {
		return fmt.Errorf("validation failed")
	}
	color.Green("All checks passed.")
	return nil
}

// runInfraActionLudus handles infra commands via the Ludus CLI.
func runInfraActionLudus(cmd *cobra.Command, cfg *config.Config, action string) error {
	ctx := context.Background()
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Infra %s [Ludus] (%s)\n\n", action, cfg.Env)

	switch action {
	case "init":
		return runLudusInit(ctx, prov)
	case "plan":
		return runLudusPlan(ctx, prov, cfg)
	case "apply":
		return runLudusApply(ctx, prov, cfg)
	case "destroy":
		return runLudusDestroy(ctx, cmd, prov)
	default:
		return fmt.Errorf("unsupported Ludus action: %s", action)
	}
}

func runLudusInit(ctx context.Context, prov provider.Provider) error {
	identity, err := prov.VerifyCredentials(ctx)
	if err != nil {
		return fmt.Errorf("cannot connect to Ludus: %w", err)
	}
	fmt.Printf("Connected to %s\n", identity)
	fmt.Println("Ludus does not require init; use 'infra apply' to deploy the range.")
	return nil
}

func runLudusPlan(ctx context.Context, prov provider.Provider, cfg *config.Config) error {
	configPath := filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "providers", "ludus", "config.yml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("ludus config not found: %s", configPath)
	}
	fmt.Printf("Config: %s\n", configPath)
	fmt.Println("\nCurrent range status:")
	instances, err := prov.DiscoverAllInstances(ctx, cfg.Env)
	if err != nil {
		fmt.Println("  No active range (will be created on apply)")
	} else {
		printInstances(instances)
	}
	return nil
}

func runLudusApply(ctx context.Context, prov provider.Provider, cfg *config.Config) error {
	configPath := filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "providers", "ludus", "config.yml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("ludus config not found: %s", configPath)
	}

	client, err := ludusClient(prov)
	if err != nil {
		return err
	}

	fmt.Println("Setting range configuration...")
	if err := client.RangeSetConfig(ctx, configPath); err != nil {
		return fmt.Errorf("set range config: %w", err)
	}

	fmt.Println("Deploying range (this may take a while)...")
	if err := client.RangeDeploy(ctx); err != nil {
		return fmt.Errorf("deploy range: %w", err)
	}

	fmt.Println("Waiting for deployment to complete...")
	if err := client.WaitForDeployment(ctx, 30*time.Second, 60*time.Minute); err != nil {
		return err
	}

	fmt.Println("\nDeployment complete!")
	instances, err := prov.DiscoverAllInstances(ctx, cfg.Env)
	if err != nil {
		return err
	}
	printInstances(instances)
	return nil
}

func runLudusDestroy(ctx context.Context, cmd *cobra.Command, prov provider.Provider) error {
	autoApprove, _ := cmd.Flags().GetBool("auto-approve")
	if !autoApprove {
		fmt.Print("Are you sure you want to destroy the Ludus range? [y/N]: ")
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil || (answer != "y" && answer != "Y") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	client, err := ludusClient(prov)
	if err != nil {
		return err
	}

	fmt.Println("Destroying range...")
	if err := client.RangeDestroy(ctx); err != nil {
		return fmt.Errorf("destroy range: %w", err)
	}
	fmt.Println("Range destroyed.")
	return nil
}

func ludusClient(prov provider.Provider) (*ludus.Client, error) {
	lp, ok := prov.(*ludus.LudusProvider)
	if !ok {
		return nil, fmt.Errorf("provider is not a Ludus provider")
	}
	return lp.Client(), nil
}

func printInstances(instances []provider.Instance) {
	for _, inst := range instances {
		fmt.Printf("  %-6s  %-20s  %-15s  %s\n", inst.ID, inst.Name, inst.PrivateIP, inst.State)
	}
}

func runInfraValidateLudus(cfg *config.Config) error {
	fmt.Println("Validating Ludus infrastructure configuration...")
	fmt.Println()

	apiKey := cfg.Ludus.APIKey
	if envKey := os.Getenv("LUDUS_API_KEY"); envKey != "" {
		apiKey = envKey
	}

	ludusBin, ludusErr := exec.LookPath("ludus")
	configPath := filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "providers", "ludus", "config.yml")

	checks := []struct {
		name string
		ok   bool
		msg  string
	}{
		{
			name: "Ludus CLI",
			ok:   ludusErr == nil,
			msg:  ludusBin,
		},
		{
			name: "Ludus API key",
			ok:   apiKey != "",
			msg:  "(set via ludus.api_key or LUDUS_API_KEY)",
		},
		{
			name: "Lab config",
			ok:   fileExistsCheck(configPath),
			msg:  filepath.Join("ad", "GOAD", "providers", "ludus", "config.yml"),
		},
		{
			name: "Inventory template",
			ok:   fileExistsCheck(filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "providers", "ludus", "inventory")),
			msg:  filepath.Join("ad", "GOAD", "providers", "ludus", "inventory"),
		},
	}

	allOK := true
	for _, c := range checks {
		if c.ok {
			color.Green("  OK  %s: %s", c.name, c.msg)
		} else {
			color.Red("  FAIL %s: %s", c.name, c.msg)
			allOK = false
		}
	}

	// If Ludus CLI is available and API key is set, try connecting.
	if ludusErr == nil && apiKey != "" {
		ctx := context.Background()
		prov, err := cfg.NewProvider(ctx)
		if err == nil {
			identity, err := prov.VerifyCredentials(ctx)
			if err != nil {
				color.Red("  FAIL Ludus API connectivity: %s", err)
				allOK = false
			} else {
				color.Green("  OK  Ludus API connectivity: %s", identity)
			}
		}
	}

	fmt.Println()
	if !allOK {
		return fmt.Errorf("validation failed")
	}
	color.Green("All checks passed.")
	return nil
}

func fileExistsCheck(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func infraLogPath(cfg *config.Config, action, deployment, module string) string {
	logDir := cfg.LogDir
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = filepath.Join(home, ".ansible", "logs", "goad")
	}
	timestamp := time.Now().Format("20060102_150405")
	moduleSlug := "all"
	if module != "" {
		moduleSlug = strings.ReplaceAll(module, "/", "_")
	}
	return filepath.Join(logDir, fmt.Sprintf("infra_%s_%s_%s_%s_%s.log",
		action, deployment, cfg.Env, moduleSlug, timestamp))
}

func resolveDeployment(cmd *cobra.Command, cfg *config.Config) string {
	if d, _ := cmd.Flags().GetString("deployment"); d != "" {
		return d
	}
	return cfg.Infra.Deployment
}

func printIndividualResults(results []terragrunt.Result) error {
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Println("Summary")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	var failed []string
	for _, r := range results {
		if r.Success {
			color.Green("  OK: %s", r.Module)
		} else {
			color.Red("  FAIL: %s", r.Module)
			failed = append(failed, r.Module)
		}
	}

	fmt.Printf("\nTotal: %d, Succeeded: %d, Failed: %d\n",
		len(results), len(results)-len(failed), len(failed))

	if len(failed) > 0 {
		return fmt.Errorf("failed modules: %s", strings.Join(failed, ", "))
	}
	return nil
}
