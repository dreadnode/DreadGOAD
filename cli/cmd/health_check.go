package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var healthCheckCmd = &cobra.Command{
	Use:   "health-check",
	Short: "Verify all GOAD instances are healthy",
	Long: `Runs health checks across all GOAD instances via SSM to verify:
  - Domain controllers are responding
  - AD replication is working with no failures
  - Domain trusts are established
  - DNS resolution across domains
  - Member servers can reach their DCs
  - Critical services (IIS, MSSQL) are running`,
	Example: `  dreadgoad health-check
  dreadgoad health-check --env staging`,
	RunE: runHealthCheck,
}

func init() {
	rootCmd.AddCommand(healthCheckCmd)
}

// healthCheck defines a single check: a name, the host to run on, a PS command, and a function to evaluate the output.
type healthCheck struct {
	name    string
	host    string
	command string
	eval    func(stdout string) (ok bool, detail string)
}

func runHealthCheck(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	title := " GOAD Health Check "
	pad := 90 - len(title)
	left := pad / 2
	right := pad - left
	fmt.Printf("%s%s%s\n", strings.Repeat("=", left), title, strings.Repeat("=", right))

	infra, err := requireInfra(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("%-40s %-10s %s\n", "CHECK", "STATUS", "DETAIL")
	fmt.Println(strings.Repeat("-", 90))

	checks := buildChecks()

	passed := 0
	failed := 0

	for _, check := range checks {
		instanceID, ok := infra.HostMap[check.host]
		if !ok {
			color.Red("%-40s %-10s %s", check.name, "SKIP", "instance not found")
			failed++
			continue
		}

		result, err := infra.Client.RunPowerShellCommand(ctx, instanceID, check.command, 90*time.Second)
		if err != nil {
			color.Red("%-40s %-10s %s", check.name, "FAIL", err.Error())
			failed++
			continue
		}
		if result.Status != "Success" {
			color.Red("%-40s %-10s %s", check.name, "FAIL", "command status: "+result.Status)
			failed++
			continue
		}

		ok, detail := check.eval(result.Stdout)
		if ok {
			color.Green("%-40s %-10s %s", check.name, "OK", detail)
			passed++
		} else {
			color.Red("%-40s %-10s %s", check.name, "FAIL", detail)
			failed++
		}
	}

	fmt.Println(strings.Repeat("-", 90))
	fmt.Printf("Results: %d passed, %d failed\n", passed, failed)

	if failed > 0 {
		return fmt.Errorf("%d health check(s) failed", failed)
	}
	return nil
}

func buildChecks() []healthCheck {
	return []healthCheck{
		// DC01 - AD responding
		{
			name:    "DC01 AD Domain Controller",
			host:    "DC01",
			command: `(Get-ADDomainController -Filter *).Name -join ','`,
			eval:    nonEmptyEval("no domain controllers returned"),
		},
		// DC01 - Replication
		{
			name:    "DC01 AD Replication",
			host:    "DC01",
			command: `$r = repadmin /replsummary 2>&1 | Out-String; if ($r -match 'fails/total.*[1-9]\d*/') { Write-Output "REPL_ERRORS:$r" } else { Write-Output "REPL_OK" }`,
			eval:    replEval,
		},
		// DC01 - Trusts
		{
			name:    "DC01 Domain Trusts",
			host:    "DC01",
			command: `Get-ADTrust -Filter * | ForEach-Object { "$($_.Name)|$($_.Direction)|$($_.TrustType)" }`,
			eval:    dc01TrustsEval,
		},
		// DC02 - AD responding
		{
			name:    "DC02 AD Domain Controller",
			host:    "DC02",
			command: `(Get-ADDomainController -Filter *).Name -join ','`,
			eval:    nonEmptyEval("no domain controllers returned"),
		},
		// DC02 - DNS cross-domain
		{
			name:    "DC02 DNS (sevenkingdoms.local)",
			host:    "DC02",
			command: `(Resolve-DnsName kingslanding.sevenkingdoms.local -ErrorAction Stop).IPAddress`,
			eval:    nonEmptyEval("DNS resolution failed"),
		},
		{
			name:    "DC02 DNS (essos.local)",
			host:    "DC02",
			command: `(Resolve-DnsName meereen.essos.local -ErrorAction Stop).IPAddress`,
			eval:    nonEmptyEval("DNS resolution failed"),
		},
		// DC03 - AD responding
		{
			name:    "DC03 AD Domain Controller",
			host:    "DC03",
			command: `(Get-ADDomainController -Filter *).Name -join ','`,
			eval:    nonEmptyEval("no domain controllers returned"),
		},
		// DC03 - Forest trust
		{
			name:    "DC03 Forest Trust",
			host:    "DC03",
			command: `Get-ADTrust -Filter * | ForEach-Object { "$($_.Name)|$($_.ForestTransitive)" }`,
			eval:    forestTrustEval,
		},
		// SRV02 - Domain membership
		{
			name:    "SRV02 Domain Membership",
			host:    "SRV02",
			command: `(Get-WmiObject Win32_ComputerSystem).Domain`,
			eval:    nonEmptyEval("not domain-joined"),
		},
		// SRV02 - DC reachable
		{
			name:    "SRV02 DC Locator",
			host:    "SRV02",
			command: `$r = nltest /dsgetdc: 2>&1 | Out-String; if ($r -match 'DC: \\\\(\S+)') { Write-Output $Matches[1] } else { Write-Output "FAIL" }`,
			eval:    dcLocatorEval,
		},
		// SRV02 - IIS
		{
			name:    "SRV02 IIS (W3SVC)",
			host:    "SRV02",
			command: `(Get-Service W3SVC -ErrorAction SilentlyContinue).Status`,
			eval:    serviceRunningEval,
		},
		// SRV02 - MSSQL
		{
			name:    "SRV02 MSSQL",
			host:    "SRV02",
			command: `(Get-Service 'MSSQL$SQLEXPRESS' -ErrorAction SilentlyContinue).Status`,
			eval:    serviceRunningEval,
		},
		// SRV03 - Domain membership
		{
			name:    "SRV03 Domain Membership",
			host:    "SRV03",
			command: `(Get-WmiObject Win32_ComputerSystem).Domain`,
			eval:    nonEmptyEval("not domain-joined"),
		},
		// SRV03 - DC reachable
		{
			name:    "SRV03 DC Locator",
			host:    "SRV03",
			command: `$r = nltest /dsgetdc: 2>&1 | Out-String; if ($r -match 'DC: \\\\(\S+)') { Write-Output $Matches[1] } else { Write-Output "FAIL" }`,
			eval:    dcLocatorEval,
		},
		// SRV03 - IIS
		{
			name:    "SRV03 IIS (W3SVC)",
			host:    "SRV03",
			command: `(Get-Service W3SVC -ErrorAction SilentlyContinue).Status`,
			eval:    serviceRunningEval,
		},
		// SRV03 - MSSQL
		{
			name:    "SRV03 MSSQL",
			host:    "SRV03",
			command: `(Get-Service 'MSSQL$SQLEXPRESS' -ErrorAction SilentlyContinue).Status`,
			eval:    serviceRunningEval,
		},
	}
}

func serviceRunningEval(stdout string) (bool, string) {
	val := strings.TrimSpace(strings.ToLower(stdout))
	if val == "running" {
		return true, "running"
	}
	if val == "" {
		return false, "service not found"
	}
	return false, val
}

// nonEmptyEval returns an eval func that passes when trimmed stdout is non-empty.
func nonEmptyEval(failMsg string) func(string) (bool, string) {
	return func(stdout string) (bool, string) {
		val := strings.TrimSpace(stdout)
		if val == "" {
			return false, failMsg
		}
		return true, val
	}
}

func replEval(stdout string) (bool, string) {
	if strings.Contains(stdout, "REPL_OK") {
		return true, "no replication failures"
	}
	return false, "replication errors detected"
}

func dc01TrustsEval(stdout string) (bool, string) {
	lower := strings.ToLower(stdout)
	hasNorth := strings.Contains(lower, "north.sevenkingdoms.local")
	hasEssos := strings.Contains(lower, "essos.local")
	if hasNorth && hasEssos {
		return true, "north.sevenkingdoms.local + essos.local"
	}
	var missing []string
	if !hasNorth {
		missing = append(missing, "north.sevenkingdoms.local")
	}
	if !hasEssos {
		missing = append(missing, "essos.local")
	}
	return false, "missing: " + strings.Join(missing, ", ")
}

func forestTrustEval(stdout string) (bool, string) {
	lower := strings.ToLower(stdout)
	if strings.Contains(lower, "sevenkingdoms.local") && strings.Contains(lower, "true") {
		return true, "sevenkingdoms.local (forest transitive)"
	}
	return false, "forest trust to sevenkingdoms.local not found"
}

func dcLocatorEval(stdout string) (bool, string) {
	val := strings.TrimSpace(stdout)
	if val == "FAIL" || val == "" {
		return false, "cannot locate domain controller"
	}
	return true, val
}
