package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var securityCheckCmd = &cobra.Command{
	Use:   "security-check",
	Short: "Audit network security posture of a deployed range",
	Long: `Queries Azure Resource Manager APIs to verify:
  - No lab VMs have public IPs attached
  - Every NIC or subnet has an NSG associated
  - NSGs carry a DenyAllInbound rule
  - No inbound Allow rules use wildcard/Internet sources
  - Inbound Allow sources are limited to VNet CIDR or AzureLoadBalancer
  - Azure Bastion exists in the resource group
  - Linux VMs use SSH key auth`,
	Example: `  dreadgoad security-check
  dreadgoad security-check --json`,
	RunE: runSecurityCheck,
}

var securityCheckJSON bool

func init() {
	rootCmd.AddCommand(securityCheckCmd)
	securityCheckCmd.Flags().BoolVar(&securityCheckJSON, "json", false,
		"Output machine-readable JSON (per-check results + counts)")
}

func runSecurityCheck(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	jsonOut := securityCheckJSON

	if !jsonOut {
		title := " Security Check "
		pad := 90 - len(title)
		left := pad / 2
		right := pad - left
		fmt.Printf("%s%s%s\n", strings.Repeat("=", left), title, strings.Repeat("=", right))
	}

	cfg, err := config.Get()
	if err != nil {
		return err
	}

	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return fmt.Errorf("create %s provider: %w", cfg.ResolvedProvider(), err)
	}

	checker, ok := prov.(provider.SecurityChecker)
	if !ok {
		return fmt.Errorf("provider %q does not support security checks", cfg.ResolvedProvider())
	}

	vpcCIDR := cfg.VpcCIDR(cfg.Env)

	if !jsonOut {
		fmt.Printf("%-50s %-8s %-8s %s\n", "CHECK", "STATUS", "SEV", "DETAIL")
		fmt.Println(strings.Repeat("-", 90))
	}

	results, err := checker.SecurityCheck(ctx, cfg.Env, vpcCIDR)
	if err != nil {
		return err
	}

	passed, failed, warned, skipped := 0, 0, 0, 0
	emit := func(res provider.SecurityCheckResult) {
		if jsonOut {
			if b, err := json.Marshal(res); err == nil {
				fmt.Println(string(b))
			}
		}
	}

	for _, res := range results {
		emit(res)
		switch res.Status {
		case "OK":
			passed++
			if !jsonOut {
				color.Green("%-50s %-8s %-8s %s", res.Name+" ["+res.Resource+"]", res.Status, res.Severity, res.Detail)
			}
		case "FAIL":
			failed++
			if !jsonOut {
				color.Red("%-50s %-8s %-8s %s", res.Name+" ["+res.Resource+"]", res.Status, res.Severity, res.Detail)
			}
		case "WARN":
			warned++
			if !jsonOut {
				color.Yellow("%-50s %-8s %-8s %s", res.Name+" ["+res.Resource+"]", res.Status, res.Severity, res.Detail)
			}
		case "SKIP":
			skipped++
			if !jsonOut {
				color.Yellow("%-50s %-8s %-8s %s", res.Name+" ["+res.Resource+"]", res.Status, res.Severity, res.Detail)
			}
		}
	}

	if jsonOut {
		report := provider.SecurityReport{
			Passed:  passed,
			Failed:  failed,
			Warned:  warned,
			Skipped: skipped,
			Checks:  results,
		}
		b, err := json.Marshal(report)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	} else {
		fmt.Println(strings.Repeat("-", 90))
		fmt.Printf("Results: %d passed, %d failed, %d warned, %d skipped\n",
			passed, failed, warned, skipped)
	}

	if failed > 0 {
		return fmt.Errorf("%d security check(s) failed", failed)
	}
	return nil
}
