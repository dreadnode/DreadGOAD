package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	daws "github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/validate"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate GOAD vulnerability configurations",
	Long: `Validates that all GOAD vulnerabilities are properly configured by
running checks via SSM PowerShell commands against live instances.

Checks credentials, Kerberos, SMB, delegation, MSSQL, ADCS, ACLs, trusts, and services.`,
	Example: `  dreadgoad validate
  dreadgoad validate --env staging --verbose
  dreadgoad validate --format json --output /tmp/results.json
  dreadgoad validate --no-fail`,
	RunE: runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)

	validateCmd.Flags().String("format", "table", "Output format: table or json")
	validateCmd.Flags().String("output", "", "JSON report output path")
	validateCmd.Flags().Bool("verbose", false, "Enable verbose output")
	validateCmd.Flags().Bool("no-fail", false, "Don't exit with error on failed checks")
}

func runValidate(cmd *cobra.Command, args []string) error {
	cfg := config.Get()
	ctx := context.Background()

	verbose, _ := cmd.Flags().GetBool("verbose")
	outputPath, _ := cmd.Flags().GetString("output")
	noFail, _ := cmd.Flags().GetBool("no-fail")

	// Determine region
	region := cfg.Region
	if region == "" {
		region = "us-west-1" // validate default matches Taskfile
	}

	client, err := daws.NewClient(ctx, region)
	if err != nil {
		return fmt.Errorf("create AWS client: %w", err)
	}

	fmt.Println("==========================================")
	fmt.Println("GOAD Vulnerability Validation")
	fmt.Println("==========================================")
	fmt.Printf("Environment: %s\n", cfg.Env)
	fmt.Printf("Region: %s\n", region)

	v := validate.NewValidator(client, cfg.Env, verbose, slog.Default())

	if err := v.DiscoverHosts(ctx); err != nil {
		return fmt.Errorf("discover hosts: %w", err)
	}

	v.RunAllChecks(ctx)

	report := v.GetReport()

	// Save JSON report
	if outputPath == "" {
		outputPath = fmt.Sprintf("/tmp/goad-validation-%s.json", time.Now().Format("20060102-150405"))
	}
	if err := v.SaveReport(outputPath); err != nil {
		fmt.Printf("Warning: could not save report: %v\n", err)
	}

	// Print summary
	fmt.Println("\n==========================================")
	fmt.Println("Validation Summary")
	fmt.Println("==========================================")
	fmt.Printf("Total Checks:    %d\n", report.Total)
	color.Green("Passed:          %d", report.Passed)
	color.Red("Failed:          %d", report.Failed)
	color.Yellow("Warnings:        %d", report.Warnings)

	if report.Total > 0 {
		pct := report.Passed * 100 / report.Total
		fmt.Printf("\nSuccess Rate: %d%%\n", pct)
	}

	fmt.Printf("\nResults saved to: %s\n", outputPath)

	if !noFail && report.Failed > 0 {
		return fmt.Errorf("validation failed with %d errors", report.Failed)
	}
	return nil
}
