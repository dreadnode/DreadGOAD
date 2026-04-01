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
	Short: "Verify domain trust relationships between all GOAD domains",
	Long: `Validates that all domain trusts are properly configured:
  - sevenkingdoms.local <-> north.sevenkingdoms.local (parent-child)
  - sevenkingdoms.local <-> essos.local (forest trust)

Also tests cross-domain authentication by querying users across trusts.`,
	Example: `  dreadgoad verify-trusts
  dreadgoad verify-trusts --env staging`,
	RunE: runVerifyTrusts,
}

func init() {
	rootCmd.AddCommand(verifyTrustsCmd)
}

func runVerifyTrusts(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	title := " GOAD Trust Verification "
	pad := 90 - len(title)
	left := pad / 2
	right := pad - left
	fmt.Printf("%s%s%s\n", strings.Repeat("=", left), title, strings.Repeat("=", right))

	infra, err := requireInfra(ctx)
	if err != nil {
		return err
	}

	dc01ID, ok := infra.HostMap["DC01"]
	if !ok {
		return fmt.Errorf("DC01 not found in discovered instances")
	}

	fmt.Printf("Using DC01 (%s) as trust verification source...\n\n", dc01ID)

	trustScript := `Write-Host "=== Domain Trusts from sevenkingdoms.local ==="
Get-ADTrust -Filter * | Format-Table Name, Direction, TrustType, ForestTransitive, TrustAttributes -AutoSize

Write-Host ""
Write-Host "=== Trust Validation ==="
nltest /domain_trusts /all_trusts

Write-Host ""
Write-Host "=== Cross-Domain Query Test ==="
Write-Host "Querying north.sevenkingdoms.local:"
Get-ADUser -Filter * -Server winterfell.north.sevenkingdoms.local | Select -First 3 Name | Format-Table -AutoSize
Write-Host "Querying essos.local:"
Get-ADUser -Filter * -Server meereen.essos.local | Select -First 3 Name | Format-Table -AutoSize

Write-Host ""
Write-Host "=== Trust Status ==="
$trusts = Get-ADTrust -Filter *
foreach ($t in $trusts) {
    Write-Host "$($t.Name): $(if (Test-ComputerSecureChannel -Server $t.Name -ErrorAction SilentlyContinue) { 'HEALTHY' } else { 'Check manually' })"
}`

	result, err := infra.Client.RunPowerShellCommand(ctx, dc01ID, trustScript, 2*time.Minute)
	if err != nil {
		return fmt.Errorf("run trust verification: %w", err)
	}

	fmt.Printf("Status: %s\n\n", result.Status)

	if result.Stdout != "" {
		fmt.Println(result.Stdout)
	}
	if result.Stderr != "" {
		color.Yellow("STDERR: %s", result.Stderr)
	}

	if result.Status == "Success" {
		// Verify expected trusts are present in output
		output := strings.ToLower(result.Stdout)
		allGood := true

		if strings.Contains(output, "north.sevenkingdoms.local") {
			color.Green("  ✓ Parent-child trust: north.sevenkingdoms.local")
		} else {
			color.Red("  ✗ Parent-child trust: north.sevenkingdoms.local NOT found")
			allGood = false
		}

		if strings.Contains(output, "essos.local") {
			color.Green("  ✓ Forest trust: essos.local")
		} else {
			color.Red("  ✗ Forest trust: essos.local NOT found")
			allGood = false
		}

		fmt.Println("\n=== Trust Verification Complete ===")

		if !allGood {
			return fmt.Errorf("one or more trust verifications failed")
		}
		return nil
	}

	return fmt.Errorf("trust verification returned status: %s", result.Status)
}
