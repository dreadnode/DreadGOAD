package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dreadnode/dreadgoad/internal/validate"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate GOAD vulnerability configurations",
	Long: `Validates that all GOAD vulnerabilities are properly configured by
running checks via SSM PowerShell commands against live instances.

Checks credentials, Kerberos, SMB, delegation, MSSQL (linked servers, impersonation,
xp_cmdshell, sysadmins), ADCS (templates), ACLs, trusts, SID filtering, scheduled tasks,
LLMNR/NBT-NS, GPO abuse, gMSA, LAPS, and services.`,
	Example: `  dreadgoad validate
  dreadgoad validate --env staging --verbose
  dreadgoad validate --format json --output /tmp/results.json
  dreadgoad validate --no-fail
  dreadgoad validate --quick`,
	RunE: runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)

	validateCmd.Flags().String("format", "table", "Output format: table or json")
	validateCmd.Flags().String("output", "", "JSON report output path")
	validateCmd.Flags().Bool("verbose", false, "Enable verbose output")
	validateCmd.Flags().Bool("no-fail", false, "Don't exit with error on failed checks")
	validateCmd.Flags().Bool("quick", false, "Quick validation of critical vulnerabilities only")
}

func runValidate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	verbose, _ := cmd.Flags().GetBool("verbose")
	outputPath, _ := cmd.Flags().GetString("output")
	noFail, _ := cmd.Flags().GetBool("no-fail")
	quick, _ := cmd.Flags().GetBool("quick")

	fmt.Println("==========================================")
	fmt.Println("GOAD Vulnerability Validation")
	fmt.Println("==========================================")

	infra, err := requireInfra(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Environment: %s\n", infra.Env)
	fmt.Printf("Region: %s\n", infra.Region)

	v := validate.NewValidator(infra.Client, infra.Env, verbose, slog.Default(), infra.Lab)

	if err := v.DiscoverHosts(ctx); err != nil {
		return fmt.Errorf("discover hosts: %w", err)
	}

	if quick {
		v.RunQuickChecks(ctx)
	} else {
		v.RunAllChecks(ctx)
	}

	report := v.GetReport()

	if outputPath == "" {
		outputPath = fmt.Sprintf("/tmp/goad-validation-%s.json", time.Now().Format("20060102-150405"))
	}
	if err := v.SaveReport(outputPath); err != nil {
		fmt.Printf("Warning: could not save report: %v\n", err)
	}

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
