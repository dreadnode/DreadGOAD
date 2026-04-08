package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var verifyTrustsCmd = &cobra.Command{
	Use:   "verify-trusts",
	Short: "Verify domain trust relationships between all lab domains",
	Long: `Validates that all domain trusts are properly configured:
  - Parent-child trusts
  - Forest trusts
  - Cross-domain authentication

Domain names and trust relationships are resolved from the lab config.`,
	Example: `  dreadgoad verify-trusts
  dreadgoad verify-trusts --env staging`,
	RunE: runVerifyTrusts,
}

func init() {
	rootCmd.AddCommand(verifyTrustsCmd)
}

func runVerifyTrusts(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	title := " Trust Verification "
	pad := 90 - len(title)
	left := pad / 2
	right := pad - left
	fmt.Printf("%s%s%s\n", strings.Repeat("=", left), title, strings.Repeat("=", right))

	infra, err := requireInfra(ctx)
	if err != nil {
		return err
	}

	lab := infra.Lab
	trusts := lab.DomainTrusts()
	if len(trusts) == 0 {
		color.Yellow("No domain trusts configured for this lab")
		return nil
	}

	allGood := true

	for _, tf := range trusts {
		// Verify from the source DC
		if tf.SourceDCRole == "" {
			continue
		}
		srcHost := strings.ToUpper(tf.SourceDCRole)
		srcID, ok := infra.HostMap[srcHost]
		if !ok {
			color.Red("  ✗ %s not found for trust verification", srcHost)
			allGood = false
			continue
		}

		fmt.Printf("\nVerifying trusts from %s (%s)...\n", srcHost, tf.SourceDomain)

		// Build a verification script that checks trusts and cross-domain queries
		var script strings.Builder
		fmt.Fprintf(&script, "Write-Host '=== Domain Trusts from %s ==='\n", tf.SourceDomain)
		script.WriteString("Get-ADTrust -Filter * | Format-Table Name, Direction, TrustType, ForestTransitive, TrustAttributes -AutoSize\n")
		script.WriteString("\nWrite-Host ''\nWrite-Host '=== Trust Validation ==='\n")
		script.WriteString("nltest /domain_trusts /all_trusts\n")

		// Cross-domain query if we have the target DC FQDN
		if tf.TargetDCRole != "" {
			tgtFQDN := lab.FQDN(tf.TargetDCRole)
			if tgtFQDN != "" {
				fmt.Fprintf(&script, "\nWrite-Host ''\nWrite-Host '=== Cross-Domain Query Test ==='\n")
				fmt.Fprintf(&script, "Write-Host 'Querying %s:'\n", tf.TargetDomain)
				fmt.Fprintf(&script, "Get-ADUser -Filter * -Server %s | Select -First 3 Name | Format-Table -AutoSize\n", tgtFQDN)
			}
		}

		script.WriteString("\nWrite-Host ''\nWrite-Host '=== Trust Status ==='\n")
		script.WriteString("$trusts = Get-ADTrust -Filter *\n")
		script.WriteString("foreach ($t in $trusts) {\n")
		script.WriteString("    $null = nltest /sc_verify:$($t.Name) 2>&1\n")
		script.WriteString("    if ($LASTEXITCODE -eq 0) { Write-Host \"$($t.Name): HEALTHY\" } else { Write-Host \"$($t.Name): Check manually\" }\n")
		script.WriteString("}\n")

		result, err := infra.Client.RunPowerShellCommand(ctx, srcID, script.String(), 2*time.Minute)
		if err != nil {
			color.Red("  ✗ Trust verification failed: %v", err)
			allGood = false
			continue
		}

		fmt.Printf("Status: %s\n\n", result.Status)

		if result.Stdout != "" {
			fmt.Println(result.Stdout)
		}
		if result.Stderr != "" {
			color.Yellow("STDERR: %s", result.Stderr)
		}

		if result.Status == "Success" {
			output := strings.ToLower(result.Stdout)
			if strings.Contains(output, strings.ToLower(tf.TargetDomain)) {
				color.Green("  ✓ Trust: %s -> %s", tf.SourceDomain, tf.TargetDomain)
			} else {
				color.Red("  ✗ Trust: %s -> %s NOT found", tf.SourceDomain, tf.TargetDomain)
				allGood = false
			}
		} else {
			allGood = false
		}
	}

	fmt.Println("\n=== Trust Verification Complete ===")

	if !allGood {
		return fmt.Errorf("one or more trust verifications failed")
	}
	return nil
}
