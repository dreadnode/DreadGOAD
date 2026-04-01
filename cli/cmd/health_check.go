package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	daws "github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/dreadnode/dreadgoad/internal/config"
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
	cfg := config.Get()
	ctx := context.Background()

	region := cfg.Region
	if region == "" {
		region = "us-west-1"
	}

	client, err := daws.NewClient(ctx, region)
	if err != nil {
		return fmt.Errorf("create AWS client: %w", err)
	}

	title := fmt.Sprintf(" GOAD Health Check (%s) ", cfg.Env)
	pad := 90 - len(title)
	left := pad / 2
	right := pad - left
	fmt.Printf("%s%s%s\n\n", strings.Repeat("=", left), title, strings.Repeat("=", right))

	instances, err := client.DiscoverInstances(ctx, cfg.Env)
	if err != nil {
		return fmt.Errorf("discover instances: %w", err)
	}

	if len(instances) == 0 {
		return fmt.Errorf("no running GOAD instances found for env=%s", cfg.Env)
	}

	// Map hostnames to instance IDs
	hostMap := make(map[string]string)
	for _, inst := range instances {
		name := strings.ToUpper(inst.Name)
		for _, h := range []string{"DC01", "DC02", "DC03", "SRV02", "SRV03"} {
			if strings.Contains(name, h) {
				hostMap[h] = inst.InstanceID
			}
		}
	}

	fmt.Printf("%-40s %-10s %s\n", "CHECK", "STATUS", "DETAIL")
	fmt.Println(strings.Repeat("-", 90))

	checks := buildChecks()

	passed := 0
	failed := 0

	for _, check := range checks {
		instanceID, ok := hostMap[check.host]
		if !ok {
			color.Red("%-40s %-10s %s", check.name, "SKIP", "instance not found")
			failed++
			continue
		}

		result, err := client.RunPowerShellCommand(ctx, instanceID, check.command, 90*time.Second)
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
			eval: func(stdout string) (bool, string) {
				names := strings.TrimSpace(stdout)
				if names == "" {
					return false, "no domain controllers returned"
				}
				return true, names
			},
		},
		// DC01 - Replication
		{
			name:    "DC01 AD Replication",
			host:    "DC01",
			command: `$r = repadmin /replsummary 2>&1 | Out-String; if ($r -match 'fails/total.*[1-9]\d*/') { Write-Output "REPL_ERRORS:$r" } else { Write-Output "REPL_OK" }`,
			eval: func(stdout string) (bool, string) {
				if strings.Contains(stdout, "REPL_OK") {
					return true, "no replication failures"
				}
				return false, "replication errors detected"
			},
		},
		// DC01 - Trusts
		{
			name:    "DC01 Domain Trusts",
			host:    "DC01",
			command: `Get-ADTrust -Filter * | ForEach-Object { "$($_.Name)|$($_.Direction)|$($_.TrustType)" }`,
			eval: func(stdout string) (bool, string) {
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
			},
		},
		// DC02 - AD responding
		{
			name:    "DC02 AD Domain Controller",
			host:    "DC02",
			command: `(Get-ADDomainController -Filter *).Name -join ','`,
			eval: func(stdout string) (bool, string) {
				names := strings.TrimSpace(stdout)
				if names == "" {
					return false, "no domain controllers returned"
				}
				return true, names
			},
		},
		// DC02 - DNS cross-domain
		{
			name:    "DC02 DNS (sevenkingdoms.local)",
			host:    "DC02",
			command: `(Resolve-DnsName kingslanding.sevenkingdoms.local -ErrorAction Stop).IPAddress`,
			eval: func(stdout string) (bool, string) {
				ip := strings.TrimSpace(stdout)
				if ip == "" {
					return false, "DNS resolution failed"
				}
				return true, ip
			},
		},
		{
			name:    "DC02 DNS (essos.local)",
			host:    "DC02",
			command: `(Resolve-DnsName meereen.essos.local -ErrorAction Stop).IPAddress`,
			eval: func(stdout string) (bool, string) {
				ip := strings.TrimSpace(stdout)
				if ip == "" {
					return false, "DNS resolution failed"
				}
				return true, ip
			},
		},
		// DC03 - AD responding
		{
			name:    "DC03 AD Domain Controller",
			host:    "DC03",
			command: `(Get-ADDomainController -Filter *).Name -join ','`,
			eval: func(stdout string) (bool, string) {
				names := strings.TrimSpace(stdout)
				if names == "" {
					return false, "no domain controllers returned"
				}
				return true, names
			},
		},
		// DC03 - Forest trust
		{
			name:    "DC03 Forest Trust",
			host:    "DC03",
			command: `Get-ADTrust -Filter * | ForEach-Object { "$($_.Name)|$($_.ForestTransitive)" }`,
			eval: func(stdout string) (bool, string) {
				lower := strings.ToLower(stdout)
				if strings.Contains(lower, "sevenkingdoms.local") && strings.Contains(lower, "true") {
					return true, "sevenkingdoms.local (forest transitive)"
				}
				return false, "forest trust to sevenkingdoms.local not found"
			},
		},
		// SRV02 - Domain membership
		{
			name:    "SRV02 Domain Membership",
			host:    "SRV02",
			command: `(Get-WmiObject Win32_ComputerSystem).Domain`,
			eval: func(stdout string) (bool, string) {
				domain := strings.TrimSpace(stdout)
				if domain == "" {
					return false, "not domain-joined"
				}
				return true, domain
			},
		},
		// SRV02 - DC reachable
		{
			name:    "SRV02 DC Locator",
			host:    "SRV02",
			command: `$r = nltest /dsgetdc: 2>&1 | Out-String; if ($r -match 'DC: \\\\(\S+)') { Write-Output $Matches[1] } else { Write-Output "FAIL" }`,
			eval: func(stdout string) (bool, string) {
				val := strings.TrimSpace(stdout)
				if val == "FAIL" || val == "" {
					return false, "cannot locate domain controller"
				}
				return true, val
			},
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
			eval: func(stdout string) (bool, string) {
				domain := strings.TrimSpace(stdout)
				if domain == "" {
					return false, "not domain-joined"
				}
				return true, domain
			},
		},
		// SRV03 - DC reachable
		{
			name:    "SRV03 DC Locator",
			host:    "SRV03",
			command: `$r = nltest /dsgetdc: 2>&1 | Out-String; if ($r -match 'DC: \\\\(\S+)') { Write-Output $Matches[1] } else { Write-Output "FAIL" }`,
			eval: func(stdout string) (bool, string) {
				val := strings.TrimSpace(stdout)
				if val == "FAIL" || val == "" {
					return false, "cannot locate domain controller"
				}
				return true, val
			},
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
