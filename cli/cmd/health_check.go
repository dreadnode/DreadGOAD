package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/labmap"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var healthCheckCmd = &cobra.Command{
	Use:   "health-check",
	Short: "Verify all lab instances are healthy",
	Long: `Runs health checks across all lab instances via SSM to verify:
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

	title := " Lab Health Check "
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

	checks := buildChecks(infra.Lab)

	passed := 0
	failed := 0

	for _, check := range checks {
		instanceID, ok := infra.HostMap[check.host]
		if !ok {
			color.Yellow("%-40s %-10s %s", check.name, "SKIP", "instance not found")
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

func buildChecks(lab *labmap.LabMap) []healthCheck {
	var checks []healthCheck

	// For each DC: AD responding + replication
	for _, role := range lab.DCs() {
		host := strings.ToUpper(role)
		checks = append(checks,
			healthCheck{
				name:    fmt.Sprintf("%s AD Domain Controller", host),
				host:    host,
				command: `(Get-ADDomainController -Filter *).Name -join ','`,
				eval:    nonEmptyEval("no domain controllers returned"),
			},
			healthCheck{
				name:    fmt.Sprintf("%s AD Replication", host),
				host:    host,
				command: `$r = repadmin /replsummary 2>&1 | Out-String; if ($r -match 'fails/total.*[1-9]\d*/') { Write-Output "REPL_ERRORS:$r" } else { Write-Output "REPL_OK" }`,
				eval:    replEval,
			},
		)
	}

	// Trust checks — derived from config
	for _, tf := range lab.DomainTrusts() {
		if tf.SourceDCRole != "" {
			srcHost := strings.ToUpper(tf.SourceDCRole)
			checks = append(checks, healthCheck{
				name:    fmt.Sprintf("%s Trusts (%s)", srcHost, tf.TargetDomain),
				host:    srcHost,
				command: `Get-ADTrust -Filter * | ForEach-Object { "$($_.Name)|$($_.Direction)|$($_.TrustType)" }`,
				eval:    trustContainsEval(tf.TargetDomain),
			})
		}
		if tf.TargetDCRole != "" {
			tgtHost := strings.ToUpper(tf.TargetDCRole)
			checks = append(checks, healthCheck{
				name:    fmt.Sprintf("%s Trusts (%s)", tgtHost, tf.SourceDomain),
				host:    tgtHost,
				command: `Get-ADTrust -Filter * | ForEach-Object { "$($_.Name)|$($_.Direction)|$($_.TrustType)" }`,
				eval:    trustContainsEval(tf.SourceDomain),
			})
		}
	}

	// Cross-domain DNS resolution between DCs on different domains
	dcRoles := lab.DCs()
	for i := 0; i < len(dcRoles); i++ {
		for j := i + 1; j < len(dcRoles); j++ {
			roleA, roleB := dcRoles[i], dcRoles[j]
			domainA := lab.DomainForHost(roleA)
			domainB := lab.DomainForHost(roleB)
			if domainA == domainB {
				continue
			}
			fqdnA := lab.FQDN(roleA)
			fqdnB := lab.FQDN(roleB)
			hostA := strings.ToUpper(roleA)
			hostB := strings.ToUpper(roleB)

			if fqdnB != "" {
				checks = append(checks, healthCheck{
					name:    fmt.Sprintf("%s DNS (%s)", hostA, domainB),
					host:    hostA,
					command: fmt.Sprintf(`(Resolve-DnsName %s -ErrorAction Stop).IPAddress`, fqdnB),
					eval:    nonEmptyEval("DNS resolution failed"),
				})
			}
			if fqdnA != "" {
				checks = append(checks, healthCheck{
					name:    fmt.Sprintf("%s DNS (%s)", hostB, domainA),
					host:    hostB,
					command: fmt.Sprintf(`(Resolve-DnsName %s -ErrorAction Stop).IPAddress`, fqdnA),
					eval:    nonEmptyEval("DNS resolution failed"),
				})
			}
		}
	}

	// Windows servers: domain membership + DC locator + services
	for _, role := range lab.WindowsServers() {
		host := strings.ToUpper(role)

		checks = append(checks,
			healthCheck{
				name:    fmt.Sprintf("%s Domain Membership", host),
				host:    host,
				command: `(Get-WmiObject Win32_ComputerSystem).Domain`,
				eval:    nonEmptyEval("not domain-joined"),
			},
			healthCheck{
				name:    fmt.Sprintf("%s DC Locator", host),
				host:    host,
				command: `$r = nltest /dsgetdc: 2>&1 | Out-String; if ($r -match 'DC: \\\\(\S+)') { Write-Output $Matches[1] } else { Write-Output "FAIL" }`,
				eval:    dcLocatorEval,
			},
		)

		// IIS check (optional — passes if not installed)
		checks = append(checks, healthCheck{
			name:    fmt.Sprintf("%s IIS (W3SVC)", host),
			host:    host,
			command: `(Get-Service W3SVC -ErrorAction SilentlyContinue).Status`,
			eval:    optionalServiceEval,
		})
	}

	// MSSQL on hosts that have it configured
	for _, role := range lab.HostsWithMSSQL() {
		host := strings.ToUpper(role)
		checks = append(checks, healthCheck{
			name:    fmt.Sprintf("%s MSSQL", host),
			host:    host,
			command: `(Get-Service 'MSSQL$SQLEXPRESS','MSSQLSERVER' -ErrorAction SilentlyContinue | Where-Object {$_.Status -eq 'Running'}).Name`,
			eval:    nonEmptyEval("MSSQL not running"),
		})
	}

	return checks
}

// optionalServiceEval passes if running, skips (passes) if not installed.
func optionalServiceEval(stdout string) (bool, string) {
	val := strings.TrimSpace(strings.ToLower(stdout))
	if val == "running" {
		return true, "running"
	}
	if val == "" {
		return true, "not installed (OK)"
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

// trustContainsEval returns an eval func that checks if the trust output mentions a domain.
func trustContainsEval(expectedDomain string) func(string) (bool, string) {
	return func(stdout string) (bool, string) {
		if strings.Contains(strings.ToLower(stdout), strings.ToLower(expectedDomain)) {
			return true, expectedDomain
		}
		return false, "trust to " + expectedDomain + " not found"
	}
}

func dcLocatorEval(stdout string) (bool, string) {
	val := strings.TrimSpace(stdout)
	if val == "FAIL" || val == "" {
		return false, "cannot locate domain controller"
	}
	return true, val
}
