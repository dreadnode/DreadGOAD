package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/terragrunt"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var infraCmd = &cobra.Command{
	Use:   "infra",
	Short: "Manage GOAD infrastructure via Terragrunt",
	Long: `Manage the GOAD lab infrastructure lifecycle using Terragrunt.

Operates on the infra/ directory which contains Terragrunt configurations
for deploying the GOAD lab (VPC, EC2 instances, security groups, etc.).

By default, commands operate on all modules (run-all). Use --module to
target a specific module (e.g. network, goad/dc01).`,
}

var infraInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Terragrunt modules",
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
	Short: "Show Terragrunt outputs (JSON)",
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

	// Shared flags for action commands
	for _, cmd := range []*cobra.Command{infraInitCmd, infraPlanCmd, infraApplyCmd, infraDestroyCmd, infraOutputCmd} {
		cmd.Flags().StringP("module", "m", "", "Target module path (e.g. network, goad/dc01)")
		cmd.Flags().String("exclude", "", "Exclude modules (comma-separated, e.g. goad/dc01,goad/dc02)")
	}

	infraApplyCmd.Flags().Bool("auto-approve", false, "Skip confirmation prompt")
	infraApplyCmd.Flags().Bool("individual", false, "Apply each subdirectory individually (for module groups like goad/)")
	infraDestroyCmd.Flags().Bool("auto-approve", false, "Skip confirmation prompt")

	infraCmd.PersistentFlags().StringP("deployment", "d", "", "Deployment name (default: from config)")
}

func runInfraAction(action string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Get()
		if err != nil {
			return err
		}

		module, _ := cmd.Flags().GetString("module")
		exclude, _ := cmd.Flags().GetString("exclude")
		deployment := resolveDeployment(cmd, cfg)

		region := cfg.Region
		if region == "" {
			region = "us-west-1"
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

		// Build working directory
		basePath := filepath.Join(cfg.ProjectRoot, "infra", deployment)
		workDir := filepath.Join(basePath, cfg.Env, region)

		if _, err := os.Stat(workDir); os.IsNotExist(err) {
			return fmt.Errorf("infra working directory not found: %s\nRun 'dreadgoad infra validate' to check your setup", workDir)
		}

		// Set up log file
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
		opts.LogFile = filepath.Join(logDir, fmt.Sprintf("infra_%s_%s_%s_%s_%s.log",
			action, deployment, cfg.Env, moduleSlug, timestamp))

		fmt.Printf("Infra %s (%s/%s)\n", action, cfg.Env, region)
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

			// Check if --individual flag is set and module has subdirectories
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

		// Full stack: run-all
		opts.WorkDir = workDir
		return terragrunt.RunAll(ctx, opts)
	}
}

func runInfraOutput(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	module, _ := cmd.Flags().GetString("module")
	deployment := resolveDeployment(cmd, cfg)

	region := cfg.Region
	if region == "" {
		region = "us-west-1"
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

	deployment := resolveDeployment(cmd, cfg)

	region := cfg.Region
	if region == "" {
		region = "us-west-1"
	}

	basePath := filepath.Join(cfg.ProjectRoot, "infra", deployment)
	result := terragrunt.ValidateEnvironment(basePath, cfg.Env, region)
	terragrunt.PrintValidationResult(result, cfg.Env, region)

	if !result.OK() {
		return fmt.Errorf("validation failed")
	}
	return nil
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
