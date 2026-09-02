// Package validate provides vulnerability validation checks for GOAD lab
// instances. It runs PowerShell commands against Windows hosts via AWS SSM
// and records pass/fail/warn results in a structured [Report].
package validate

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/labmap"
)

func printHeader(w io.Writer, header string) {
	_, _ = fmt.Fprintf(w, "\n== %s ==\n", header)
}

// probeOutcome classifies a "must-exist" probe so retryMustExist can tell a
// genuine negative from a transient one.
type probeOutcome int

const (
	probePositive   probeOutcome = iota // the expected thing is present (PASS-worthy)
	probeNegative                       // the expected thing is genuinely absent (FAIL-worthy)
	probeIncomplete                     // the probe could not complete (WARN-worthy)
)

// retryMustExist re-runs a probe for a condition that should always hold in a
// correctly provisioned lab, returning as soon as it sees probePositive. Only
// probeNegative is retried: a must-exist entity reported absent by a probe that
// *completed* is far more often a transient blip (a DC mid-replication, an
// admin share momentarily de-registered) than a real defect, and because the
// probe completed the host is responsive, so re-running is cheap. A
// probeIncomplete (transport error) is returned immediately — runPSErr has
// already retried it and it feeds dead-host marking, so retrying again here
// would only amplify latency against a slow or dead host. An outcome that
// persists across every attempt is trusted, so a genuine defect still surfaces.
func (v *Validator) retryMustExist(ctx context.Context, probe func() probeOutcome) probeOutcome {
	var last probeOutcome
	for attempt := 1; attempt <= transientRetries; attempt++ {
		last = probe()
		if last != probeNegative {
			return last
		}
		if attempt < transientRetries {
			if backoffSleep(ctx, attempt) != nil {
				// Backoff was cut short (ctx cancelled or deadline hit); a
				// probeNegative captured mid-cancellation is not authoritative,
				// so surface probeIncomplete (WARN) rather than letting the
				// cancellation masquerade as a genuine defect (FAIL).
				return probeIncomplete
			}
		}
	}
	return last
}

// scriptADUserExists checks whether an AD user exists, distinguishing a genuine
// absence (USER_NOT_FOUND) from a transient directory error. Get-ADUser is run
// with -ErrorAction Stop so a momentary RPC/timeout failure raises instead of
// being silently swallowed into a false "not found"; only the AD-specific
// not-found exception yields USER_NOT_FOUND, while any other error exits
// non-zero and surfaces to the caller as a probe error (WARN, not FAIL).
const scriptADUserExists = `try { Get-ADUser -Identity {{psq .Username}} -ErrorAction Stop > $null; 'USER_FOUND' } ` +
	`catch [Microsoft.ActiveDirectory.Management.ADIdentityNotFoundException] { 'USER_NOT_FOUND' } ` +
	`catch { exit 1 }`

// scriptADUserWithGroups is scriptADUserExists plus the user's group
// memberships (one GROUP=<dn> line each) for membership assertions.
const scriptADUserWithGroups = `try { $u = Get-ADUser -Identity {{psq .Username}} -Properties MemberOf -ErrorAction Stop } ` +
	`catch [Microsoft.ActiveDirectory.Management.ADIdentityNotFoundException] { Write-Output 'USER_NOT_FOUND'; exit 0 } ` +
	`catch { exit 1 }; ` +
	`Write-Output 'USER_FOUND'; ` +
	`foreach ($g in $u.MemberOf) { Write-Output "GROUP=$g" }`

// scriptAdminShares enumerates all SMB shares (failing loudly if the Server
// service can't be queried) and emits the admin shares that are present. A
// query error exits non-zero (WARN); a successful enumeration that is missing
// a share is authoritative for that moment and is retried by the caller, since
// ADMIN$/C$ can briefly de-register while the Server service settles.
const scriptAdminShares = `$ErrorActionPreference='Stop'; ` +
	`try { $s = Get-SmbShare } catch { exit 1 }; ` +
	`$s | Where-Object { $_.Name -eq 'ADMIN$' -or $_.Name -eq 'C$' } | Select-Object -ExpandProperty Name`

func (v *Validator) checkCredentialDiscovery(ctx context.Context, w io.Writer) {
	printHeader(w, "Credential Discovery Vulnerabilities")

	users := v.lab.UsersWithPasswordInDescription()
	if len(users) == 0 {
		v.addResult(w, "SKIP", "Credentials", "No users with password-in-description configured", "")
		return
	}

	for _, uf := range users {
		dcRole := strings.ToUpper(uf.DCRole)
		output, err := runScriptText(ctx, v, dcRole,
			`Get-ADUser -Identity {{psq .Username}} -Properties Description | Select-Object -ExpandProperty Description`,
			map[string]any{"Username": uf.Username})
		if err != nil {
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Could not query description for %s: %v", uf.Username, err), "")
			continue
		}
		if strings.Contains(strings.ToLower(output), strings.ToLower(uf.User.Password)) {
			v.addResult(w, "PASS", "Credentials", fmt.Sprintf("%s has password in description", uf.Username), "")
		} else {
			v.addResult(w, "FAIL", "Credentials", fmt.Sprintf("%s does NOT have password in description", uf.Username), "")
		}
	}
}

func (v *Validator) checkKerberosAttacks(ctx context.Context, w io.Writer) {
	printHeader(w, "Kerberos Attack Vectors")

	v.checkASREPRoasting(ctx, w)
	v.checkKerberoasting(ctx, w)
}

func (v *Validator) checkASREPRoasting(ctx context.Context, w io.Writer) {
	asrepHosts := v.lab.HostsWithScript("asrep_roasting")
	if len(asrepHosts) == 0 {
		v.addResult(w, "SKIP", "Kerberos", "No AS-REP roasting scripts configured", "")
		return
	}

	for _, role := range asrepHosts {
		dcRole := strings.ToUpper(role)
		output, err := v.runPSErr(ctx, dcRole,
			`Get-ADUser -Filter {DoesNotRequirePreAuth -eq $true} -Properties DoesNotRequirePreAuth | Select-Object -ExpandProperty SamAccountName`)
		if err != nil {
			v.addResult(w, "WARN", "Kerberos",
				fmt.Sprintf("Could not query AS-REP roastable users on %s: %v", dcRole, err), "")
			continue
		}
		users := parseOutputLines(output)
		if len(users) > 0 {
			v.addResult(w, "PASS", "Kerberos",
				fmt.Sprintf("AS-REP roastable users on %s: %s", dcRole, strings.Join(users, ", ")), "")
		} else {
			v.addResult(w, "FAIL", "Kerberos",
				fmt.Sprintf("No AS-REP roastable users found on %s", dcRole), "")
		}
	}
}

func (v *Validator) checkKerberoasting(ctx context.Context, w io.Writer) {
	spnUsers := v.lab.UsersWithSPNs()
	if len(spnUsers) == 0 {
		v.addResult(w, "SKIP", "Kerberos", "No users with SPNs configured", "")
		return
	}

	for _, uf := range spnUsers {
		dcRole := strings.ToUpper(uf.DCRole)
		output, err := runScriptText(ctx, v, dcRole,
			`Get-ADUser -Identity {{psq .Username}} -Properties ServicePrincipalName | Select-Object -ExpandProperty ServicePrincipalName`,
			map[string]any{"Username": uf.Username})
		if err != nil {
			v.addResult(w, "WARN", "Kerberos",
				fmt.Sprintf("Could not query SPNs for %s: %v", uf.Username, err), "")
			continue
		}
		if output != "" {
			v.addResult(w, "PASS", "Kerberos",
				fmt.Sprintf("%s has SPNs configured (Kerberoastable)", uf.Username), "")
		} else {
			v.addResult(w, "FAIL", "Kerberos",
				fmt.Sprintf("%s does NOT have SPNs configured", uf.Username), "")
		}
	}
}

func (v *Validator) checkNetworkMisconfigs(ctx context.Context, w io.Writer) {
	printHeader(w, "Network-Level Misconfigurations")

	servers := v.lab.WindowsServers()
	if len(servers) == 0 {
		v.addResult(w, "SKIP", "Network", "No Windows servers configured", "")
		return
	}

	for _, role := range servers {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output, err := v.runPSErr(ctx, host,
			`Get-SmbServerConfiguration | Select-Object RequireSecuritySignature,EnableSecuritySignature | Format-Table -AutoSize | Out-String`)
		if err != nil {
			v.addResult(w, "WARN", "Network", fmt.Sprintf("Could not query SMB signing on %s: %v", hostLabel, err), "")
			continue
		}
		lower := strings.ToLower(output)

		switch {
		case strings.Contains(lower, "false") && strings.Count(lower, "false") >= 2:
			v.addResult(w, "PASS", "Network", fmt.Sprintf("%s has SMB signing disabled", hostLabel), "")
		case strings.Contains(lower, "false"):
			v.addResult(w, "WARN", "Network", fmt.Sprintf("%s has SMB signing enabled but not required", hostLabel), "")
		default:
			v.addResult(w, "FAIL", "Network", fmt.Sprintf("%s has SMB signing enforced", hostLabel), "")
		}
	}
}

func (v *Validator) checkAnonymousSMB(ctx context.Context, w io.Writer) {
	printHeader(w, "Anonymous/Guest SMB Enumeration")

	const lsaPath = `HKLM:\System\CurrentControlSet\Control\Lsa`

	anon := []struct {
		name     string
		passDesc string
	}{
		{"RestrictAnonymous", "NULL sessions enabled"},
		{"RestrictAnonymousSAM", "SAM enum enabled"},
	}
	for _, role := range v.lab.DCs() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		for _, a := range anon {
			v.checkAnonymousRegistry(ctx, w, host, hostLabel, lsaPath, a.name, a.passDesc)
		}
	}

	for _, role := range v.lab.WindowsServers() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output, err := v.runPSErr(ctx, host,
			`Get-LocalUser -Name Guest | Select-Object Name,Enabled | Format-Table -AutoSize | Out-String`)
		if err != nil {
			v.addResult(w, "WARN", "SMB", fmt.Sprintf("Could not query Guest account on %s: %v", hostLabel, err), "")
			continue
		}
		if strings.Contains(strings.ToLower(output), "true") {
			v.addResult(w, "PASS", "SMB", fmt.Sprintf("Guest account enabled on %s", hostLabel), "")
		} else {
			v.addResult(w, "FAIL", "SMB", fmt.Sprintf("Guest account NOT enabled on %s", hostLabel), "")
		}
	}

	for _, role := range v.lab.HostsWithVuln("ntlmdowngrade") {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		v.checkLmCompatibility(ctx, w, host, hostLabel, lsaPath)
	}
}

func (v *Validator) checkAnonymousRegistry(ctx context.Context, w io.Writer, host, hostLabel, lsaPath, name, passDesc string) {
	r, err := runScriptJSON[registryDWORDResult](ctx, v, host, scriptRegistryDWORD,
		map[string]any{"Path": lsaPath, "Name": name})
	switch {
	case err != nil:
		v.addResult(w, "WARN", "SMB",
			fmt.Sprintf("Could not query %s on %s: %v", name, hostLabel, err), "")
	case r.Error != "":
		v.addResult(w, "WARN", "SMB",
			fmt.Sprintf("%s query error on %s: %s", name, hostLabel, r.Error), "")
	case r.Present && r.Value == 0:
		v.addResult(w, "PASS", "SMB",
			fmt.Sprintf("%s is 0 on %s (%s)", name, hostLabel, passDesc), "")
	case r.Present:
		v.addResult(w, "INFO", "SMB",
			fmt.Sprintf("%s is %d on %s", name, r.Value, hostLabel), "")
	default:
		v.addResult(w, "INFO", "SMB",
			fmt.Sprintf("%s not set on %s", name, hostLabel), "")
	}
}

func (v *Validator) checkLmCompatibility(ctx context.Context, w io.Writer, host, hostLabel, lsaPath string) {
	r, err := runScriptJSON[registryDWORDResult](ctx, v, host, scriptRegistryDWORD,
		map[string]any{"Path": lsaPath, "Name": "LmCompatibilityLevel"})
	switch {
	case err != nil:
		v.addResult(w, "WARN", "SMB",
			fmt.Sprintf("Could not query LmCompatibilityLevel on %s: %v", hostLabel, err), "")
	case r.Error != "":
		v.addResult(w, "WARN", "SMB",
			fmt.Sprintf("LmCompatibilityLevel query error on %s: %s", hostLabel, r.Error), "")
	case !r.Present:
		v.addResult(w, "WARN", "SMB",
			fmt.Sprintf("LmCompatibilityLevel not configured on %s (registry key missing)", hostLabel), "")
	case r.Value >= 0 && r.Value <= 2:
		v.addResult(w, "PASS", "SMB",
			fmt.Sprintf("LmCompatibilityLevel is %d on %s (NTLM downgrade vulnerable)", r.Value, hostLabel), "")
	default:
		v.addResult(w, "FAIL", "SMB",
			fmt.Sprintf("LmCompatibilityLevel is %d on %s (expected 0-2)", r.Value, hostLabel), "")
	}
}

func (v *Validator) checkDelegation(ctx context.Context, w io.Writer) {
	printHeader(w, "Delegation Configurations")

	allHosts := v.lab.HostsWithScript("constrained_delegation")
	allHosts = append(allHosts, v.lab.HostsWithScript("unconstrained_delegation")...)
	if len(allHosts) == 0 {
		// Fall back to checking all DCs
		allHosts = v.lab.DCs()
	}
	if len(allHosts) == 0 {
		v.addResult(w, "SKIP", "Delegation", "No domain controllers configured", "")
		return
	}

	checked := make(map[string]bool)
	for _, role := range allHosts {
		host := strings.ToUpper(role)
		if checked[host] || !v.hasHost(host) {
			continue
		}
		checked[host] = true

		output := v.runPS(ctx, host,
			`Get-ADUser -Filter {TrustedForDelegation -eq $true} -Properties TrustedForDelegation | Select-Object -ExpandProperty SamAccountName`)
		users := parseOutputLines(output)
		if len(users) > 0 {
			v.addResult(w, "PASS", "Delegation",
				fmt.Sprintf("Unconstrained delegation users on %s: %s", host, strings.Join(users, ", ")), "")
		}

		output = v.runPS(ctx, host,
			`Get-ADUser -Filter 'msDS-AllowedToDelegateTo -like "*"' -Properties msDS-AllowedToDelegateTo | Select-Object -ExpandProperty SamAccountName`)
		users = parseOutputLines(output)
		if len(users) > 0 {
			v.addResult(w, "PASS", "Delegation",
				fmt.Sprintf("Constrained delegation users on %s: %s", host, strings.Join(users, ", ")), "")
		}
	}
}

func (v *Validator) checkMachineAccountQuota(ctx context.Context, w io.Writer) {
	printHeader(w, "Machine Account Quota")

	checked := make(map[string]bool)
	for _, role := range v.lab.DCs() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		domain := v.lab.DomainForHost(strings.ToLower(host))
		if domain == "" {
			domain = host
		}
		if checked[domain] {
			continue
		}
		checked[domain] = true
		output, err := v.runPSErr(ctx, host,
			`Get-ADObject -Identity ((Get-ADDomain).distinguishedname) -Properties ms-DS-MachineAccountQuota | Select-Object -ExpandProperty ms-DS-MachineAccountQuota`)
		if err != nil {
			v.addResult(w, "WARN", "MachineQuota", fmt.Sprintf("Could not query Machine Account Quota in %s: %v", domain, err), "")
			continue
		}
		val := strings.TrimSpace(output)
		if val == "10" {
			v.addResult(w, "PASS", "MachineQuota", fmt.Sprintf("Machine Account Quota is 10 in %s (allows RBCD)", domain), "")
		} else {
			v.addResult(w, "WARN", "MachineQuota", fmt.Sprintf("Machine Account Quota is %s in %s (default is 10)", val, domain), "")
		}
	}
}

func (v *Validator) checkMSSQL(ctx context.Context, w io.Writer) {
	printHeader(w, "MSSQL Configurations")

	mssqlFacts := v.lab.HostsWithMSSQLConfig()
	if len(mssqlFacts) == 0 {
		v.addResult(w, "SKIP", "MSSQL", "No MSSQL configured for this lab", "")
		return
	}

	for _, mf := range mssqlFacts {
		host := strings.ToUpper(mf.HostRole)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(mf.Hostname)

		// Get-Service writes a non-terminating error for any name it can't find
		// (Cannot find any service with service name 'X'), and PowerShell.exe
		// exits 1 whenever any error was emitted -- even with -ErrorAction
		// SilentlyContinue, which suppresses the message but not $?. The
		// Ludus WinRM runner treats exit!=0 as failure and discards stdout,
		// so the running service name (e.g. MSSQL$SQLEXPRESS) gets thrown
		// away and the check spuriously fails. Probe each service name in
		// isolation, swallow the missing-service error, and force exit 0.
		output, err := v.runPSErr(ctx, host,
			`$ErrorActionPreference='SilentlyContinue'; foreach ($n in 'MSSQL$SQLEXPRESS','MSSQLSERVER') { $s = Get-Service -Name $n -ErrorAction SilentlyContinue; if ($s -and $s.Status -eq 'Running') { $s.Name } }; exit 0`)
		if err != nil {
			v.addResult(w, "WARN", "MSSQL", fmt.Sprintf("Could not query MSSQL on %s: %v", hostLabel, err), "")
			continue
		}
		if strings.TrimSpace(output) == "" {
			v.addResult(w, "FAIL", "MSSQL", fmt.Sprintf("MSSQL NOT running on %s", hostLabel), "")
			continue
		}
		v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("MSSQL running on %s", hostLabel), "")

		sqlQuery := func(sqlTmpl string, vars map[string]any) (string, bool) {
			return v.mssqlProbe(ctx, host, sqlTmpl, vars)
		}

		for _, admin := range mf.MSSQL.SysAdmins {
			rows, ok := sqlQuery(
				`SELECT m.name FROM sys.server_role_members srm `+
					`JOIN sys.server_principals r ON srm.role_principal_id = r.principal_id `+
					`JOIN sys.server_principals m ON srm.member_principal_id = m.principal_id `+
					`WHERE r.name = 'sysadmin' AND m.name = {{psq .Admin}}`,
				map[string]any{"Admin": admin})
			switch {
			case !ok:
				v.addResult(w, "WARN", "MSSQL", fmt.Sprintf("Could not determine sysadmin status of %s on %s (host settling?)", admin, hostLabel), "")
			case rows != "":
				v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("%s is sysadmin on %s", admin, hostLabel), "")
			default:
				v.addResult(w, "FAIL", "MSSQL", fmt.Sprintf("%s is NOT sysadmin on %s", admin, hostLabel), "")
			}
		}

		for grantee, target := range mf.MSSQL.ExecuteAsLogin {
			rows, ok := sqlQuery(
				`SELECT pr.name FROM sys.server_permissions sp `+
					`JOIN sys.server_principals pr ON sp.grantee_principal_id = pr.principal_id `+
					`JOIN sys.server_principals pr2 ON sp.major_id = pr2.principal_id `+
					`WHERE sp.permission_name = 'IMPERSONATE' AND pr.name = {{psq .Grantee}} AND pr2.name = {{psq .Target}}`,
				map[string]any{"Grantee": grantee, "Target": target})
			switch {
			case !ok:
				v.addResult(w, "WARN", "MSSQL", fmt.Sprintf("Could not determine IMPERSONATE grant for %s->%s on %s (host settling?)", grantee, target, hostLabel), "")
			case rows != "":
				v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("%s can impersonate %s on %s", grantee, target, hostLabel), "")
			default:
				v.addResult(w, "FAIL", "MSSQL", fmt.Sprintf("%s CANNOT impersonate %s on %s", grantee, target, hostLabel), "")
			}
		}

		for name, ls := range mf.MSSQL.LinkedServers {
			rows, ok := sqlQuery(
				`SELECT name FROM sys.servers WHERE is_linked = 1 AND name = {{psq .Name}}`,
				map[string]any{"Name": name})
			switch {
			case !ok:
				v.addResult(w, "WARN", "MSSQL", fmt.Sprintf("Could not determine linked server %s on %s (host settling?)", name, hostLabel), "")
			case rows != "":
				v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("Linked server %s -> %s on %s", name, ls.DataSrc, hostLabel), "")
			default:
				v.addResult(w, "FAIL", "MSSQL", fmt.Sprintf("Linked server %s NOT found on %s", name, hostLabel), "")
			}
		}

		v.checkMSSQLExtendedFeatures(w, sqlQuery, hostLabel)
	}
}

// mssqlProbe runs a read-only SQL query on host over a local integrated-auth
// connection and returns the rows (one per line). ok is false only when the
// probe could not be completed after retries — a host still settling right
// after provisioning returns empty or truncated stdout even though the WinRM
// transport reports success. The sqlProbeSentinel, printed only after the
// query finishes, separates that transient case from a genuine empty result
// set: a completed query with no rows returns ("", true). Callers should WARN
// (not FAIL) when ok is false so a healthy lab is never reported as broken.
func (v *Validator) mssqlProbe(ctx context.Context, host, sqlTmpl string, vars map[string]any) (string, bool) {
	script := `$c = New-Object System.Data.SqlClient.SqlConnection 'Server=localhost;Integrated Security=True;TrustServerCertificate=True'; ` +
		`$c.Open(); $cmd = $c.CreateCommand(); ` +
		`$cmd.CommandText = @"` + "\n" + sqlTmpl + "\n" + `"@; ` +
		`$r = $cmd.ExecuteReader(); while ($r.Read()) { Write-Output $r[0].ToString() }; $r.Close(); $c.Close(); ` +
		`Write-Output '` + sqlProbeSentinel + `'`
	// runPSErr already retries empty-but-successful output (a settling host),
	// so a single call suffices: a completed query always prints the sentinel
	// as its final line, even with zero rows, so a matching last line means
	// the result is authoritative (empty rows = a genuine negative). A missing
	// (or non-final) sentinel means the probe never completed (transport
	// error, or truncated output that outlived the retries): report ok=false
	// so the caller WARNs instead of emitting a bogus FAIL.
	out, err := runScriptText(ctx, v, host, script, vars)
	if err != nil {
		return "", false
	}
	// Anchor on the final line rather than substring-searching: a row value
	// that happens to contain the sentinel (or output truncated mid-row but
	// still including the sentinel substring) must not be able to fool the
	// probe into ok=true or corrupt the returned rows.
	idx := strings.LastIndex(out, "\n")
	last := out
	if idx >= 0 {
		last = out[idx+1:]
	}
	if strings.TrimRight(last, "\r") != sqlProbeSentinel {
		return "", false
	}
	if idx < 0 {
		return "", true
	}
	return strings.TrimRight(out[:idx], "\r\n"), true
}

type mssqlQueryFn func(sqlTmpl string, vars map[string]any) (string, bool)

func (v *Validator) checkMSSQLExtendedFeatures(w io.Writer, sqlQuery mssqlQueryFn, hostLabel string) {
	xpOut, ok := sqlQuery(
		`SELECT CONVERT(INT, ISNULL(value, value_in_use)) FROM sys.configurations WHERE name = 'xp_cmdshell'`,
		nil)
	if !ok {
		// Couldn't get a definitive answer; the SeImpersonate probe below
		// depends on xp_cmdshell, so skip the whole group rather than emit a
		// bogus "NOT enabled".
		v.addResult(w, "WARN", "MSSQL", fmt.Sprintf("Could not query xp_cmdshell on %s (host settling?)", hostLabel), "")
		return
	}
	xpEnabled := strings.TrimSpace(xpOut) == "1"
	if xpEnabled {
		v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("xp_cmdshell enabled on %s", hostLabel), "")
	} else {
		v.addResult(w, "FAIL", "MSSQL", fmt.Sprintf("xp_cmdshell NOT enabled on %s", hostLabel), "")
	}

	if xpEnabled {
		if privOut, ok := sqlQuery(`EXEC xp_cmdshell 'whoami /priv'`, nil); ok {
			if strings.Contains(privOut, "SeImpersonatePrivilege") {
				v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("MSSQL service has SeImpersonatePrivilege on %s (potato escalation possible)", hostLabel), "")
			} else if strings.TrimSpace(privOut) != "" {
				v.addResult(w, "INFO", "MSSQL", fmt.Sprintf("SeImpersonatePrivilege NOT found on MSSQL service on %s", hostLabel), "")
			}
		}
	}

	trustworthy, ok := sqlQuery(
		`SELECT name FROM sys.databases WHERE is_trustworthy_on = 1 AND name NOT IN ('master','tempdb')`,
		nil)
	if !ok {
		// Same reasoning as xp_cmdshell above: emit WARN rather than silently
		// dropping the check, so "couldn't query" is distinguishable from
		// "no trustworthy DBs".
		v.addResult(w, "WARN", "MSSQL", fmt.Sprintf("Could not query TRUSTWORTHY databases on %s (host settling?)", hostLabel), "")
		return
	}
	dbs := parseOutputLines(trustworthy)
	if len(dbs) > 0 {
		v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("TRUSTWORTHY databases on %s: %s", hostLabel, strings.Join(dbs, ", ")), "")
	} else {
		v.addResult(w, "INFO", "MSSQL", fmt.Sprintf("No TRUSTWORTHY databases on %s", hostLabel), "")
	}
}

//go:embed scripts/adcs_features.ps1
var scriptADCSFeatures string

//go:embed scripts/adcs_published_templates.ps1
var scriptADCSPublishedTemplates string

type adcsFeaturesResult struct {
	CertAuthority bool   `json:"cert_authority"`
	WebEnrollment bool   `json:"web_enrollment"`
	Error         string `json:"error"`
}

type adcsTemplatesResult struct {
	Templates []string `json:"templates"`
	Error     string   `json:"error"`
}

func (v *Validator) checkADCS(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS Configuration")

	adcsHosts := v.lab.ADCSHosts()
	if len(adcsHosts) == 0 {
		v.addResult(w, "SKIP", "ADCS", "No ADCS configured for this lab", "")
		return
	}

	for _, role := range adcsHosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))

		feats, err := runScriptJSON[adcsFeaturesResult](ctx, v, host, scriptADCSFeatures, nil)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "ADCS",
				fmt.Sprintf("Could not query ADCS features on %s: %v", hostLabel, err), "")
			continue
		case feats.Error != "":
			v.addResult(w, "WARN", "ADCS",
				fmt.Sprintf("ADCS feature query error on %s: %s", hostLabel, feats.Error), "")
		}

		if feats.CertAuthority {
			v.addResult(w, "PASS", "ADCS", fmt.Sprintf("ADCS installed on %s", hostLabel), "")
		} else {
			v.addResult(w, "FAIL", "ADCS", fmt.Sprintf("ADCS NOT installed on %s", hostLabel), "")
		}

		if v.lab.CAWebEnrollment() {
			if feats.WebEnrollment {
				v.addResult(w, "PASS", "ADCS", "ADCS Web Enrollment installed (ESC8 possible)", "")
			} else {
				v.addResult(w, "WARN", "ADCS", "ADCS Web Enrollment not installed", "")
			}
		}

		v.checkADCSPublishedTemplates(ctx, w, role, host, hostLabel)
	}
}

func (v *Validator) checkADCSPublishedTemplates(ctx context.Context, w io.Writer, role, host, hostLabel string) {
	templateQueryHost := host
	if dcRole := v.lab.ADCSDCRole(role); dcRole != "" {
		templateQueryHost = strings.ToUpper(dcRole)
	}
	tmpls, err := runScriptJSON[adcsTemplatesResult](ctx, v, templateQueryHost, scriptADCSPublishedTemplates, nil)
	if err != nil || tmpls.Error != "" || len(tmpls.Templates) == 0 {
		return
	}

	published := make(map[string]bool, len(tmpls.Templates))
	for _, t := range tmpls.Templates {
		published[strings.ToLower(strings.TrimSpace(t))] = true
	}
	configured := v.lab.ADCSConfiguredTemplates()
	for _, tmpl := range []string{"ESC1", "ESC2", "ESC3", "ESC3-CRA", "ESC4", "ESC13"} {
		if !configured[strings.ToUpper(tmpl)] {
			continue
		}
		if published[strings.ToLower(tmpl)] {
			v.addResult(w, "PASS", "ADCS", fmt.Sprintf("Template %s published on %s CA", tmpl, hostLabel), "")
		} else {
			v.addResult(w, "FAIL", "ADCS", fmt.Sprintf("Template %s NOT published on %s CA", tmpl, hostLabel), "")
		}
	}
}

//go:embed scripts/adcs_esc7.ps1
var scriptADCSESC7 string

// adcsESC7Result is the typed JSON envelope emitted by adcs_esc7.ps1.
type adcsESC7Result struct {
	PSPKI bool   `json:"pspki"`
	Found bool   `json:"found"`
	Error string `json:"error"`
}

func (v *Validator) checkADCSESC7(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC7 - ManageCA ACL")

	facts := v.lab.HostsWithESC7()
	if len(facts) == 0 {
		v.addResult(w, "SKIP", "ADCS-ESC7", "No ESC7 (ManageCA) vulns configured", "")
		return
	}

	for _, f := range facts {
		// The probe runs on the host that declared the vuln (the DC), where
		// the upstream vulns_adcs_esc7 role has already installed PSPKI.
		// Querying the CA's ACL still requires AD context (PSPKI's
		// Get-CertificationAuthority hits the AD configuration partition),
		// so the script elevates to a domain admin via Invoke-Command --
		// mirroring the role's `become: runas` pattern.
		host := strings.ToUpper(f.HostRole)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(f.Hostname)
		caLabel := strings.ToUpper(f.CAHostname)
		if caLabel == "" {
			caLabel = hostLabel
		}

		r, err := runScriptJSON[adcsESC7Result](ctx, v, host, scriptADCSESC7,
			map[string]any{
				"Identity":       f.CAManager,
				"DomainNetBIOS":  f.DomainNetBIOS,
				"DomainPassword": f.DomainPassword,
				"AdminUser":      f.AdminUser,
			})
		switch {
		case err != nil:
			v.addResult(w, "WARN", "ADCS-ESC7",
				fmt.Sprintf("Could not verify ManageCA for %s on %s: %v", f.CAManager, caLabel, err), "")
		case !r.PSPKI:
			v.addResult(w, "FAIL", "ADCS-ESC7",
				fmt.Sprintf("PSPKI module not installed on %s", hostLabel), "")
		case r.Error != "":
			v.addResult(w, "WARN", "ADCS-ESC7",
				fmt.Sprintf("ManageCA check error on %s: %s", caLabel, r.Error), "")
		case r.Found:
			v.addResult(w, "PASS", "ADCS-ESC7",
				fmt.Sprintf("%s has ManageCA on %s CA (ESC7 exploitable)", f.CAManager, caLabel), "")
		default:
			v.addResult(w, "FAIL", "ADCS-ESC7",
				fmt.Sprintf("%s does NOT have ManageCA on %s CA", f.CAManager, caLabel), "")
		}
	}
}

//go:embed scripts/certutil_flag.ps1
var scriptCertutilFlag string

//go:embed scripts/registry_dword.ps1
var scriptRegistryDWORD string

type certutilFlagResult struct {
	Present bool   `json:"present"`
	Error   string `json:"error"`
}

type registryDWORDResult struct {
	Present bool   `json:"present"`
	Value   int    `json:"value"`
	Error   string `json:"error"`
}

func (v *Validator) checkADCSESC6(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC6 - EDITF_ATTRIBUTESUBJECTALTNAME2")

	hosts := v.lab.HostsWithVuln("adcs_esc6")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "ADCS-ESC6", "No ESC6 vulns configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[certutilFlagResult](ctx, v, host, scriptCertutilFlag,
			map[string]any{"Key": `policy\EditFlags`, "Flag": "EDITF_ATTRIBUTESUBJECTALTNAME2"})
		switch {
		case err != nil:
			v.addResult(w, "WARN", "ADCS-ESC6",
				fmt.Sprintf("Could not query EditFlags on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "ADCS-ESC6",
				fmt.Sprintf("EditFlags query error on %s: %s", hostLabel, r.Error), "")
		case r.Present:
			v.checkESC6KDCBinding(ctx, w, role, hostLabel)
		default:
			v.addResult(w, "FAIL", "ADCS-ESC6",
				fmt.Sprintf("EDITF_ATTRIBUTESUBJECTALTNAME2 NOT set on %s", hostLabel), "")
		}
	}
}

// kdcBinding is one KDC's StrongCertificateBindingEnforcement state, read from
// the DC that will validate certificates issued in a given domain.
type kdcBinding struct {
	// DCLabel is the hostname of the DC the value was read from.
	DCLabel string
	Value   int
	// Present is false when the value is absent, which means the KDC falls
	// back to its shipped default rather than to anything the lab chose.
	Present bool
}

// unpinnedKDCNote explains what an absent StrongCertificateBindingEnforcement
// value means now that Microsoft has moved the default under us.
//
// The Feb 2025 hardening rollout (KB5014754) made Full Enforcement the built-in
// default, so an unset value is not "unknown, possibly permissive": it is the
// strict mode. That was confirmed behaviourally on this lab's essos KDC, which
// has no value set and refused an ESC9 certificate with event 39 logged at
// *Error* level. Event 39's level is the discriminator, not its presence:
// Compatibility permits the logon and logs 39 as a Warning.
const unpinnedKDCNote = "has no StrongCertificateBindingEnforcement value, so its KDC takes the shipped default of Full Enforcement (KB5014754, Feb 2025)"

// readKDCBinding resolves the DC whose KDC validates certificates for adcsRole's
// domain and reads its StrongCertificateBindingEnforcement value.
//
// The KDC that matters is the DC of the certificate's own domain, not the CA
// host and not whichever DC in the lab happens to be permissive. GOAD pins
// StrongCertificateBindingEnforcement=0 on kingslanding while the CA and every
// vulnerable template live in essos, so a check that reads any DC but this one
// credits an exploit that cannot land.
func (v *Validator) readKDCBinding(ctx context.Context, adcsRole string) (kdcBinding, error) {
	dcRole := v.lab.ADCSDCRole(adcsRole)
	if dcRole == "" {
		if domain := v.lab.DomainForHost(adcsRole); domain != "" {
			dcRole = v.lab.DCForDomain(domain)
		}
	}
	if dcRole == "" {
		return kdcBinding{}, errors.New("no DC resolved for its domain")
	}
	dcHost := strings.ToUpper(dcRole)
	if !v.hasHost(dcHost) {
		return kdcBinding{DCLabel: dcHost}, fmt.Errorf("validating DC %s is unreachable", dcHost)
	}

	b := kdcBinding{DCLabel: strings.ToUpper(v.lab.Hostname(dcRole))}
	r, err := runScriptJSON[registryDWORDResult](ctx, v, dcHost, scriptRegistryDWORD,
		map[string]any{
			"Path": `HKLM:\SYSTEM\CurrentControlSet\Services\Kdc`,
			"Name": "StrongCertificateBindingEnforcement",
		})
	switch {
	case err != nil:
		return b, fmt.Errorf("could not query StrongCertificateBindingEnforcement on %s: %w", b.DCLabel, err)
	case r.Error != "":
		return b, fmt.Errorf("StrongCertificateBindingEnforcement query error on %s: %s", b.DCLabel, r.Error)
	}
	b.Value, b.Present = r.Value, r.Present
	return b, nil
}

// checkESC6KDCBinding decides whether a CA with EDITF_ATTRIBUTESUBJECTALTNAME2
// set can actually win, which the CA-side flag alone does not establish.
//
// ESC6 issues off the stock User template, and EDITF_ATTRIBUTESUBJECTALTNAME2
// only injects a SAN; it cannot touch the szOID_NTDS_CA_SECURITY_EXT security
// extension, so the issued cert carries the *requester's* SID rather than the
// impersonated target's. Only a KDC at StrongCertificateBindingEnforcement=0
// (Disabled) ignores that extension. At 1 (Compatibility) a *present* extension
// is still validated strictly and the mismatch is rejected, and 2 (Full
// Enforcement) rejects it as well. So the pass condition is ==0, not !=2.
func (v *Validator) checkESC6KDCBinding(ctx context.Context, w io.Writer, role, hostLabel string) {
	b, err := v.readKDCBinding(ctx, role)
	if err != nil {
		v.addResult(w, "WARN", "ADCS-ESC6",
			fmt.Sprintf("EDITF_ATTRIBUTESUBJECTALTNAME2 set on %s but %s; cannot confirm ESC6 is exploitable", hostLabel, err), "")
		return
	}
	switch {
	case !b.Present:
		v.addResult(w, "FAIL", "ADCS-ESC6",
			fmt.Sprintf("EDITF_ATTRIBUTESUBJECTALTNAME2 set on %s but %s %s, which rejects the SID mismatch (ESC6 NOT exploitable); pin it with the adcs_esc10_case1 vuln", hostLabel, b.DCLabel, unpinnedKDCNote), "")
	case b.Value == 0:
		v.addResult(w, "PASS", "ADCS-ESC6",
			fmt.Sprintf("EDITF_ATTRIBUTESUBJECTALTNAME2 set on %s and StrongCertificateBindingEnforcement=0 on %s (ESC6 exploitable)", hostLabel, b.DCLabel), "")
	default:
		v.addResult(w, "FAIL", "ADCS-ESC6",
			fmt.Sprintf("EDITF_ATTRIBUTESUBJECTALTNAME2 set on %s but StrongCertificateBindingEnforcement=%d on %s, which validates the security extension and rejects the SID mismatch (ESC6 NOT exploitable)", hostLabel, b.Value, b.DCLabel), "")
	}
}

func (v *Validator) checkADCSESC10(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC10 - Weak Certificate Mapping")

	cases := []struct {
		vuln    string
		path    string
		name    string
		wantVal int
		passFmt string
	}{
		{
			vuln:    "adcs_esc10_case1",
			path:    `HKLM:\SYSTEM\CurrentControlSet\Services\Kdc`,
			name:    "StrongCertificateBindingEnforcement",
			wantVal: 0,
			passFmt: "StrongCertificateBindingEnforcement=0 on %s (ESC10 case 1 exploitable)",
		},
		{
			vuln:    "adcs_esc10_case2",
			path:    `HKLM:\System\CurrentControlSet\Control\SecurityProviders\Schannel`,
			name:    "CertificateMappingMethods",
			wantVal: 4,
			passFmt: "CertificateMappingMethods=0x4 on %s (ESC10 case 2 exploitable)",
		},
	}

	configured := false
	for _, c := range cases {
		hosts := v.lab.HostsWithVuln(c.vuln)
		if len(hosts) == 0 {
			continue
		}
		configured = true
		for _, role := range hosts {
			host := strings.ToUpper(role)
			if !v.hasHost(host) {
				continue
			}
			hostLabel := strings.ToUpper(v.lab.Hostname(role))
			r, err := runScriptJSON[registryDWORDResult](ctx, v, host, scriptRegistryDWORD,
				map[string]any{"Path": c.path, "Name": c.name})
			switch {
			case err != nil:
				v.addResult(w, "WARN", "ADCS-ESC10",
					fmt.Sprintf("Could not query %s on %s: %v", c.name, hostLabel, err), "")
			case r.Error != "":
				v.addResult(w, "WARN", "ADCS-ESC10",
					fmt.Sprintf("%s query error on %s: %s", c.name, hostLabel, r.Error), "")
			case !r.Present:
				v.addResult(w, "WARN", "ADCS-ESC10",
					fmt.Sprintf("%s not configured on %s (registry key missing)", c.name, hostLabel), "")
			case r.Value == c.wantVal:
				v.addResult(w, "PASS", "ADCS-ESC10", fmt.Sprintf(c.passFmt, hostLabel), "")
			default:
				v.addResult(w, "FAIL", "ADCS-ESC10",
					fmt.Sprintf("%s=%d on %s (expected %d)", c.name, r.Value, hostLabel, c.wantVal), "")
			}
		}
	}

	if !configured {
		v.addResult(w, "SKIP", "ADCS-ESC10", "No ESC10 vulns configured", "")
	}
}

func (v *Validator) checkADCSESC11(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC11 - RPC Encryption Disabled")

	hosts := v.lab.HostsWithVuln("adcs_esc11")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "ADCS-ESC11", "No ESC11 vulns configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[certutilFlagResult](ctx, v, host, scriptCertutilFlag,
			map[string]any{"Key": `CA\InterfaceFlags`, "Flag": "IF_ENFORCEENCRYPTICERTREQUEST"})
		switch {
		case err != nil:
			v.addResult(w, "WARN", "ADCS-ESC11",
				fmt.Sprintf("Could not query InterfaceFlags on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "ADCS-ESC11",
				fmt.Sprintf("InterfaceFlags query error on %s: %s", hostLabel, r.Error), "")
		case !r.Present:
			v.addResult(w, "PASS", "ADCS-ESC11",
				fmt.Sprintf("IF_ENFORCEENCRYPTICERTREQUEST disabled on %s (ESC11 exploitable)", hostLabel), "")
		default:
			v.addResult(w, "FAIL", "ADCS-ESC11",
				fmt.Sprintf("IF_ENFORCEENCRYPTICERTREQUEST still set on %s", hostLabel), "")
		}
	}
}

func (v *Validator) checkADCSESC15(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC15 - Web Server Template Enrollment")

	hosts := v.lab.HostsWithVuln("adcs_esc15")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "ADCS-ESC15", "No ESC15 vulns configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		// Query the domain DC for template ACL since ADWS runs on DCs
		templateQueryHost := host
		if dcRole := v.lab.ADCSDCRole(role); dcRole != "" {
			templateQueryHost = strings.ToUpper(dcRole)
		}
		if !v.hasHost(templateQueryHost) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[adcsESC15Result](ctx, v, templateQueryHost, scriptADCSESC15WebServerACL, nil)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "ADCS-ESC15",
				fmt.Sprintf("Could not verify Web Server template ACL on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "ADCS-ESC15",
				fmt.Sprintf("Web Server template ACL probe error on %s: %s", hostLabel, r.Error), "")
		case !r.TemplateFound:
			v.addResult(w, "FAIL", "ADCS-ESC15", fmt.Sprintf("Web Server template NOT found on %s", hostLabel), "")
		case r.EnrollGrant:
			v.addResult(w, "PASS", "ADCS-ESC15", fmt.Sprintf("Domain Users can enroll Web Server template on %s (ESC15 exploitable)", hostLabel), "")
		default:
			v.addResult(w, "FAIL", "ADCS-ESC15", fmt.Sprintf("Domain Users CANNOT enroll Web Server template on %s", hostLabel), "")
		}
	}
}

//go:embed scripts/adcs_esc15_web_server_acl.ps1
var scriptADCSESC15WebServerACL string

type adcsESC15Result struct {
	TemplateFound bool   `json:"template_found"`
	EnrollGrant   bool   `json:"enroll_grant"`
	Error         string `json:"error"`
}

func (v *Validator) checkACLPermissions(ctx context.Context, w io.Writer) {
	printHeader(w, "ACL Permissions")

	acls := v.lab.AllACLs()
	if len(acls) == 0 {
		v.addResult(w, "SKIP", "ACL", "No ACLs configured for this lab", "")
		return
	}

	for _, af := range acls {
		// Skip ACLs targeting computer accounts
		if strings.HasSuffix(af.ACL.To, "$") {
			continue
		}

		dcRole := strings.ToUpper(af.DCRole)
		if !v.hasHost(dcRole) {
			continue
		}

		v.checkSingleACL(ctx, w, af, dcRole)
	}
}

func (v *Validator) checkSingleACL(ctx context.Context, w io.Writer, af labmap.ACLFact, dcRole string) {
	source := v.lab.User(af.ACL.For)
	target := v.lab.User(af.ACL.To)

	// Use the full source name for sAMAccountName lookup.
	// Strip trailing $ for gMSA accounts to match the identity reference.
	sourceSam := strings.TrimSuffix(source, "$")

	// Build the PowerShell lookup for the target object.
	// DN paths (containing = signs) are resolved directly via Get-Acl;
	// SamAccountNames are looked up with Get-ADObject which finds
	// users, groups, and service accounts alike.
	//
	// For well-known accounts (e.g. "NT AUTHORITY\ANONYMOUS LOGON"),
	// we match the full identity reference string directly.
	script, err := renderScript(`
$ErrorActionPreference = 'Stop'
Import-Module ActiveDirectory
Set-Location AD:
$target = {{psq .Target}}
$sourceSam = {{psq .SourceSam}}
$sourceMatch = ('*' + {{psq .SourceSam}} + '*')
try {
  if ($target -match '=') {
    $objDN = $target
    $objAcl = Get-Acl -Path $objDN -ErrorAction Stop
  } else {
    $obj = Get-ADObject -Filter ('SamAccountName -eq "' + $target + '"') -ErrorAction Stop
    if (-not $obj) { Write-Output 'TARGET_NOT_FOUND'; exit }
    $objAcl = Get-Acl -Path $obj.DistinguishedName -ErrorAction Stop
  }
  # Try name-based match: wildcard against identity references.
  # This catches both DOMAIN\user and well-known accounts.
  $ace = $objAcl.Access | Where-Object { $_.IdentityReference -like $sourceMatch }
  if (-not $ace) {
    # Try with just the last component (e.g. "ANONYMOUS LOGON" from "NT AUTHORITY\ANONYMOUS LOGON")
    $shortName = $sourceSam
    if ($sourceSam -match '\\') { $shortName = $sourceSam.Split('\')[-1] }
    $ace = $objAcl.Access | Where-Object { $_.IdentityReference -like ('*' + $shortName + '*') }
  }
  if (-not $ace) {
    # Resolve source to SID and match ACEs stored as SID references
    $srcSID = $null
    foreach ($sam in @($sourceSam, ($sourceSam + '$'))) {
      $srcObj = Get-ADObject -LDAPFilter "(sAMAccountName=$sam)" -Properties objectSID -ErrorAction SilentlyContinue
      if ($srcObj -and $srcObj.objectSID) { $srcSID = $srcObj.objectSID.Value; break }
    }
    if (-not $srcSID) {
      $svc = Get-ADServiceAccount -Identity $sourceSam -Properties objectSID -ErrorAction SilentlyContinue
      if ($svc) { $srcSID = $svc.objectSID.Value }
    }
    if ($srcSID) {
      $ace = $objAcl.Access | Where-Object {
        $ref = $_.IdentityReference
        ($ref.Value -eq $srcSID) -or (
          $ref -is [System.Security.Principal.NTAccount] -and $(
            try { $ref.Translate([System.Security.Principal.SecurityIdentifier]).Value -eq $srcSID } catch { $false }
          )
        )
      }
    }
  }
  if ($ace) { Write-Output 'ACL_FOUND' } else { Write-Output 'ACL_NOT_FOUND' }
} catch {
  Write-Output "CHECK_ERROR: $_"
}`, map[string]any{"Target": target, "SourceSam": sourceSam})
	if err != nil {
		v.addResult(w, "WARN", "ACL", fmt.Sprintf("Could not render ACL script %s -> %s: %v", source, target, err), "")
		return
	}

	// ACL probes can return empty output under WinRM contention (the
	// per-VM lock serializes calls, but queued checks may hit transient
	// WinRM/SOCKS5 issues that persist across runPSErr's internal
	// retries). Retry inconclusive results at the check level with
	// backoff before falling through to WARN.
	var output string
	var runErr error
	for attempt := 1; attempt <= transientRetries; attempt++ {
		output, runErr = v.runPSErr(ctx, dcRole, script)
		if runErr != nil {
			break
		}
		if strings.Contains(output, "ACL_FOUND") || strings.Contains(output, "ACL_NOT_FOUND") || strings.Contains(output, "TARGET_NOT_FOUND") {
			break
		}
		// Inconclusive (empty or CHECK_ERROR) — retry with backoff.
		if attempt < transientRetries {
			if backoffSleep(ctx, attempt) != nil {
				break
			}
		}
	}
	if runErr != nil {
		v.addResult(w, "WARN", "ACL", fmt.Sprintf("Could not verify ACL %s -> %s (%s): %v", source, target, af.ACL.Right, runErr), "")
		return
	}

	switch {
	case strings.Contains(output, "ACL_FOUND"):
		v.addResult(w, "PASS", "ACL", fmt.Sprintf("%s has %s on %s", source, af.ACL.Right, target), "")
	case strings.Contains(output, "ACL_NOT_FOUND"):
		v.addResult(w, "FAIL", "ACL", fmt.Sprintf("%s does NOT have %s on %s", source, af.ACL.Right, target), "")
	default:
		v.addResult(w, "WARN", "ACL", fmt.Sprintf("Could not verify ACL: %s -> %s (%s)", source, target, af.ACL.Right), "")
	}
}

func (v *Validator) checkDomainTrusts(ctx context.Context, w io.Writer) {
	printHeader(w, "Domain Trusts")

	trusts := v.lab.DomainTrusts()
	if len(trusts) == 0 {
		v.addResult(w, "SKIP", "Trusts", "No domain trusts configured for this lab", "")
		return
	}

	for _, tf := range trusts {
		if tf.SourceDCRole != "" {
			srcHost := strings.ToUpper(tf.SourceDCRole)
			if v.hasHost(srcHost) {
				output, err := v.runPSErr(ctx, srcHost,
					`Get-ADTrust -Filter * | Select-Object Name,Direction,TrustType | Format-Table -AutoSize | Out-String`)
				switch {
				case err != nil:
					v.addResult(w, "WARN", "Trusts",
						fmt.Sprintf("Could not query trusts on %s: %v", tf.SourceDomain, err), "")
				case strings.Contains(strings.ToLower(output), strings.ToLower(tf.TargetDomain)):
					v.addResult(w, "PASS", "Trusts",
						fmt.Sprintf("Trust configured: %s -> %s", tf.SourceDomain, tf.TargetDomain), "")
				default:
					v.addResult(w, "FAIL", "Trusts",
						fmt.Sprintf("Trust NOT found: %s -> %s", tf.SourceDomain, tf.TargetDomain), "")
				}
			}
		}

		if tf.TargetDCRole != "" {
			tgtHost := strings.ToUpper(tf.TargetDCRole)
			if v.hasHost(tgtHost) {
				output, err := v.runPSErr(ctx, tgtHost,
					`Get-ADTrust -Filter * | Select-Object Name,Direction,TrustType | Format-Table -AutoSize | Out-String`)
				switch {
				case err != nil:
					v.addResult(w, "WARN", "Trusts",
						fmt.Sprintf("Could not query trusts on %s: %v", tf.TargetDomain, err), "")
				case strings.Contains(strings.ToLower(output), strings.ToLower(tf.SourceDomain)):
					v.addResult(w, "PASS", "Trusts",
						fmt.Sprintf("Trust configured: %s -> %s", tf.TargetDomain, tf.SourceDomain), "")
				default:
					v.addResult(w, "FAIL", "Trusts",
						fmt.Sprintf("Trust NOT found: %s -> %s", tf.TargetDomain, tf.SourceDomain), "")
				}
			}
		}
	}
}

func (v *Validator) checkServices(ctx context.Context, w io.Writer) {
	printHeader(w, "Additional Services")

	for _, role := range v.lab.DCs() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		output, err := v.runPSErr(ctx, host,
			`Get-Service Spooler | Select-Object Status | Format-Table -AutoSize | Out-String`)
		if err != nil {
			v.addResult(w, "WARN", "Services", fmt.Sprintf("Could not query Spooler on %s: %v", host, err), "")
			continue
		}
		if strings.Contains(strings.ToLower(output), "running") {
			v.addResult(w, "PASS", "Services", fmt.Sprintf("Print Spooler running on %s (coercion possible)", host), "")
		} else {
			v.addResult(w, "WARN", "Services", fmt.Sprintf("Print Spooler not running on %s", host), "")
		}
	}

	for _, role := range v.lab.WindowsServers() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output, err := v.runPSErr(ctx, host,
			`Get-Service W3SVC -ErrorAction SilentlyContinue | Select-Object Name,Status | Format-Table -AutoSize | Out-String`)
		if err != nil {
			v.addResult(w, "WARN", "Services", fmt.Sprintf("Could not query IIS on %s: %v", hostLabel, err), "")
			continue
		}
		if strings.Contains(strings.ToLower(output), "running") {
			v.addResult(w, "PASS", "Services", fmt.Sprintf("IIS running on %s", hostLabel), "")
		} else if strings.TrimSpace(output) != "" {
			v.addResult(w, "WARN", "Services", fmt.Sprintf("IIS not running on %s", hostLabel), "")
		}

		output, err = v.runPSErr(ctx, host,
			`Get-Service WebClient -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Status`)
		if err != nil {
			v.addResult(w, "WARN", "Services", fmt.Sprintf("Could not query WebClient on %s: %v", hostLabel, err), "")
			continue
		}
		status := strings.TrimSpace(strings.ToLower(output))
		switch {
		case status == "running":
			v.addResult(w, "PASS", "Services", fmt.Sprintf("WebClient running on %s (WebDAV coercion possible)", hostLabel), "")
		case status == "stopped":
			v.addResult(w, "INFO", "Services", fmt.Sprintf("WebClient stopped on %s (startable for coercion)", hostLabel), "")
		case status != "":
			v.addResult(w, "INFO", "Services", fmt.Sprintf("WebClient status %s on %s", status, hostLabel), "")
		}
	}
}

//go:embed scripts/scheduled_task.ps1
var scriptScheduledTask string

type scheduledTaskResult struct {
	Found bool   `json:"found"`
	State string `json:"state"`
	Error string `json:"error"`
}

func (v *Validator) checkScheduledTasks(ctx context.Context, w io.Writer) {
	printHeader(w, "Scheduled Tasks (Bots)")

	botScripts := map[string]string{
		"rdp_scheduler": "connect_bot",
		"ntlm_relay":    "ntlm_bot",
		"responder":     "responder_bot",
	}

	found := false
	for pattern, taskName := range botScripts {
		hosts := v.lab.HostsWithScript(pattern)
		for _, role := range hosts {
			found = true
			host := strings.ToUpper(role)
			if !v.hasHost(host) {
				continue
			}
			r, err := runScriptJSON[scheduledTaskResult](ctx, v, host, scriptScheduledTask,
				map[string]any{"TaskName": taskName})
			switch {
			case err != nil:
				v.addResult(w, "WARN", "ScheduledTasks",
					fmt.Sprintf("%s state unknown on %s: %v", taskName, host, err), "")
			case r.Error != "":
				v.addResult(w, "WARN", "ScheduledTasks",
					fmt.Sprintf("%s probe error on %s: %s", taskName, host, r.Error), "")
			case !r.Found:
				v.addResult(w, "FAIL", "ScheduledTasks",
					fmt.Sprintf("%s NOT found on %s", taskName, host), "")
			default:
				v.addResult(w, "PASS", "ScheduledTasks",
					fmt.Sprintf("%s is %s on %s", taskName, r.State, host), "")
			}
		}
	}
	if !found {
		v.addResult(w, "SKIP", "ScheduledTasks", "No bot scripts configured", "")
	}
}

func (v *Validator) checkLLMNR(ctx context.Context, w io.Writer) {
	printHeader(w, "LLMNR / NBT-NS")

	llmnrHosts := v.lab.HostsWithVuln("enable_llmnr")
	for _, role := range llmnrHosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[registryDWORDResult](ctx, v, host, scriptRegistryDWORD,
			map[string]any{
				"Path": `HKLM:\Software\policies\Microsoft\Windows NT\DNSClient`,
				"Name": "EnableMulticast",
			})
		switch {
		case err != nil:
			v.addResult(w, "WARN", "LLMNR",
				fmt.Sprintf("Could not query EnableMulticast on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "LLMNR",
				fmt.Sprintf("EnableMulticast query error on %s: %s", hostLabel, r.Error), "")
		case !r.Present, r.Value == 1:
			// Missing key or value=1 means LLMNR is enabled (default behavior).
			v.addResult(w, "PASS", "LLMNR", fmt.Sprintf("LLMNR enabled on %s", hostLabel), "")
		default:
			v.addResult(w, "FAIL", "LLMNR",
				fmt.Sprintf("LLMNR disabled on %s (value=%d)", hostLabel, r.Value), "")
		}
	}

	nbtHosts := v.lab.HostsWithVuln("enable_nbt_ns")
	for _, role := range nbtHosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output := v.runPS(ctx, host,
			`Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Services\NetBT\Parameters\Interfaces\*' -Name NetbiosOptions -ErrorAction SilentlyContinue | Select-Object -ExpandProperty NetbiosOptions`)
		lines := parseOutputLines(output)
		allZero := len(lines) > 0
		for _, l := range lines {
			if strings.TrimSpace(l) != "0" {
				allZero = false
				break
			}
		}
		switch {
		case allZero:
			v.addResult(w, "PASS", "LLMNR", fmt.Sprintf("NBT-NS enabled on %s", hostLabel), "")
		case len(lines) == 0:
			v.addResult(w, "WARN", "LLMNR", fmt.Sprintf("NBT-NS status unknown on %s", hostLabel), "")
		default:
			v.addResult(w, "FAIL", "LLMNR", fmt.Sprintf("NBT-NS disabled on %s", hostLabel), "")
		}
	}

	if len(llmnrHosts) == 0 && len(nbtHosts) == 0 {
		v.addResult(w, "SKIP", "LLMNR", "No LLMNR/NBT-NS vulns configured", "")
	}
}

func (v *Validator) checkGPOAbuse(ctx context.Context, w io.Writer) {
	printHeader(w, "GPO Abuse")

	hosts := v.lab.HostsWithScript("gpo_abuse")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "GPO", "No GPO abuse scripts configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		output, err := v.runPSErr(ctx, host,
			`Get-GPO -All | Where-Object { $_.DisplayName -notmatch 'Default Domain' } | Select-Object -ExpandProperty DisplayName`)
		if err != nil {
			v.addResult(w, "WARN", "GPO", fmt.Sprintf("Could not query GPOs on %s: %v", host, err), "")
			continue
		}
		gpos := parseOutputLines(output)
		if len(gpos) > 0 {
			v.addResult(w, "PASS", "GPO", fmt.Sprintf("Custom GPOs on %s: %s", host, strings.Join(gpos, ", ")), "")
		} else {
			v.addResult(w, "FAIL", "GPO", fmt.Sprintf("No custom GPOs found on %s", host), "")
		}
	}
}

func (v *Validator) checkGMSA(ctx context.Context, w io.Writer) {
	printHeader(w, "gMSA Accounts")

	facts := v.lab.DomainsWithGMSA()
	if len(facts) == 0 {
		v.addResult(w, "SKIP", "gMSA", "No gMSA configured for this lab", "")
		return
	}

	for _, gf := range facts {
		host := strings.ToUpper(gf.DCRole)
		if !v.hasHost(host) {
			continue
		}
		output, err := runScriptText(ctx, v, host,
			`Get-ADServiceAccount -Identity {{psq .Name}} -Properties Enabled | Select-Object -ExpandProperty Enabled`,
			map[string]any{"Name": gf.GMSA.Name})
		switch {
		case err != nil:
			v.addResult(w, "WARN", "gMSA",
				fmt.Sprintf("Could not query gMSA %s in %s: %v", gf.GMSA.Name, gf.Domain, err), "")
		case strings.Contains(strings.ToLower(output), "true"):
			v.addResult(w, "PASS", "gMSA", fmt.Sprintf("gMSA %s exists and enabled in %s", gf.GMSA.Name, gf.Domain), "")
		default:
			v.addResult(w, "FAIL", "gMSA", fmt.Sprintf("gMSA %s NOT found or disabled in %s", gf.GMSA.Name, gf.Domain), "")
		}
	}
}

func (v *Validator) checkLAPS(ctx context.Context, w io.Writer) {
	printHeader(w, "LAPS")

	lapsHosts := v.lab.HostsWithLAPS()
	if len(lapsHosts) == 0 {
		v.addResult(w, "SKIP", "LAPS", "No LAPS hosts configured", "")
		return
	}

	for _, role := range lapsHosts {
		hc := v.lab.HostConfigs[role]
		hostname := strings.ToUpper(hc.Hostname)
		dcRole := v.lab.DCForDomain(hc.Domain)
		if dcRole == "" {
			continue
		}
		dc := strings.ToUpper(dcRole)
		if !v.hasHost(dc) {
			continue
		}
		output, err := runScriptText(ctx, v, dc,
			`Get-ADComputer -Identity {{psq .Hostname}} -Properties ms-Mcs-AdmPwd | Select-Object -ExpandProperty ms-Mcs-AdmPwd`,
			map[string]any{"Hostname": hostname})
		switch {
		case err != nil:
			v.addResult(w, "WARN", "LAPS",
				fmt.Sprintf("Could not query LAPS password for %s: %v", hostname, err), "")
		case output != "":
			v.addResult(w, "PASS", "LAPS", fmt.Sprintf("LAPS password set for %s", hostname), "")
		default:
			v.addResult(w, "FAIL", "LAPS", fmt.Sprintf("LAPS password NOT set for %s", hostname), "")
		}
	}

	// Verify LAPS reader permissions — ensure configured accounts/groups
	// can actually read the ms-Mcs-AdmPwd attribute on computer objects.
	readerFacts := v.lab.DomainsWithLAPSReaders()
	for _, lf := range readerFacts {
		dc := strings.ToUpper(lf.DCRole)
		if !v.hasHost(dc) {
			continue
		}
		for _, reader := range lf.Readers {
			output, err := runScriptText(ctx, v, dc,
				`$computers = Get-ADComputer -Filter {ms-Mcs-AdmPwd -like '*'} -Properties ms-Mcs-AdmPwd -SearchBase (Get-ADDomain).DistinguishedName -ErrorAction SilentlyContinue; `+
					`Import-Module ActiveDirectory; Set-Location AD:; `+
					`$pattern = '*' + {{psq .Reader}} + '*'; `+
					`$found = $false; foreach ($c in $computers) { `+
					`$acl = Get-Acl -Path $c.DistinguishedName; `+
					`$match = $acl.Access | Where-Object { $_.IdentityReference -like $pattern }; `+
					`if ($match) { $found = $true; break } }; `+
					`if ($found) { Write-Output 'READER_OK' } else { Write-Output 'READER_NOT_FOUND' }`,
				map[string]any{"Reader": reader})
			switch {
			case err != nil:
				v.addResult(w, "WARN", "LAPS", fmt.Sprintf("Could not verify LAPS reader %s in %s: %v", reader, lf.Domain, err), "")
			case strings.Contains(output, "READER_OK"):
				v.addResult(w, "PASS", "LAPS", fmt.Sprintf("%s has LAPS read permission in %s", reader, lf.Domain), "")
			case strings.Contains(output, "READER_NOT_FOUND"):
				v.addResult(w, "FAIL", "LAPS", fmt.Sprintf("%s does NOT have LAPS read permission in %s", reader, lf.Domain), "")
			default:
				v.addResult(w, "WARN", "LAPS", fmt.Sprintf("Could not verify LAPS reader %s in %s", reader, lf.Domain), "")
			}
		}
	}
}

func (v *Validator) checkSIDFiltering(ctx context.Context, w io.Writer) {
	printHeader(w, "SID Filtering")

	trusts := v.lab.DomainTrusts()
	if len(trusts) == 0 {
		v.addResult(w, "SKIP", "SIDFiltering", "No domain trusts configured", "")
		return
	}

	for _, tf := range trusts {
		if tf.SourceDCRole == "" {
			continue
		}
		host := strings.ToUpper(tf.SourceDCRole)
		if !v.hasHost(host) {
			continue
		}
		output, err := runScriptText(ctx, v, host,
			`netdom trust {{psq .Source}} /d:{{psq .Target}} /quarantine 2>&1`,
			map[string]any{"Source": tf.SourceDomain, "Target": tf.TargetDomain})
		if err != nil {
			v.addResult(w, "WARN", "SIDFiltering",
				fmt.Sprintf("Could not query SID filtering %s -> %s: %v", tf.SourceDomain, tf.TargetDomain, err), "")
			continue
		}
		lower := strings.ToLower(output)
		switch {
		case strings.Contains(lower, "not enabled"):
			v.addResult(w, "PASS", "SIDFiltering", fmt.Sprintf("SID filtering disabled on %s -> %s (exploitation possible)", tf.SourceDomain, tf.TargetDomain), "")
		case strings.Contains(lower, "enabled"):
			v.addResult(w, "WARN", "SIDFiltering", fmt.Sprintf("SID filtering enabled on %s -> %s", tf.SourceDomain, tf.TargetDomain), "")
		default:
			v.addResult(w, "INFO", "SIDFiltering", fmt.Sprintf("Could not determine SID filtering: %s -> %s", tf.SourceDomain, tf.TargetDomain), "")
		}
	}
}

func (v *Validator) checkSIDHistory(ctx context.Context, w io.Writer) {
	printHeader(w, "SID History on Trusts")

	hosts := v.lab.HostsWithScript("sidhistory")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "SIDHistory", "No SID History scripts configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		// Query all trusts on this DC and check if SID History is enabled
		output := v.runPS(ctx, host,
			`Get-ADTrust -Filter * | ForEach-Object { $name = $_.Name; $sh = $_.EnableSidHistory; Write-Output "$name|$sh" }`)
		lines := parseOutputLines(output)
		if len(lines) == 0 {
			v.addResult(w, "WARN", "SIDHistory", fmt.Sprintf("Could not enumerate trusts on %s", host), "")
			continue
		}
		for _, line := range lines {
			parts := strings.SplitN(line, "|", 2)
			if len(parts) < 2 {
				continue
			}
			trustName := strings.TrimSpace(parts[0])
			enabled := strings.TrimSpace(strings.ToLower(parts[1]))
			if enabled == "true" {
				v.addResult(w, "PASS", "SIDHistory", fmt.Sprintf("SID History enabled on trust to %s (cross-forest abuse possible)", trustName), "")
			} else {
				v.addResult(w, "INFO", "SIDHistory", fmt.Sprintf("SID History disabled on trust to %s", trustName), "")
			}
		}
	}
}

func (v *Validator) checkSMBShares(ctx context.Context, w io.Writer) {
	printHeader(w, "SMB Shares")

	shareHosts := v.lab.HostsWithVuln("openshares")
	if len(shareHosts) == 0 {
		v.addResult(w, "SKIP", "Shares", "No openshares vulns configured", "")
		return
	}

	for _, role := range shareHosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output, err := v.runPSErr(ctx, host,
			`Get-SmbShare | Where-Object { $_.Name -notmatch 'ADMIN\$|C\$|IPC\$' } | Select-Object -ExpandProperty Name`)
		if err != nil {
			v.addResult(w, "WARN", "Shares", fmt.Sprintf("Could not query shares on %s: %v", hostLabel, err), "")
			continue
		}
		shares := parseOutputLines(output)
		if len(shares) > 0 {
			v.addResult(w, "PASS", "Shares", fmt.Sprintf("Custom shares on %s: %s", hostLabel, strings.Join(shares, ", ")), "")
		} else {
			v.addResult(w, "FAIL", "Shares", fmt.Sprintf("No custom shares found on %s", hostLabel), "")
		}
	}
}

func (v *Validator) checkFirewallDisabled(ctx context.Context, w io.Writer) {
	printHeader(w, "Firewall")

	fwHosts := v.lab.HostsWithVuln("disable_firewall")
	if len(fwHosts) == 0 {
		v.addResult(w, "SKIP", "Firewall", "No disable_firewall vulns configured", "")
		return
	}

	for _, role := range fwHosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output, err := v.runPSErr(ctx, host,
			`Get-NetFirewallProfile | Where-Object { $_.Enabled -eq $true } | Select-Object -ExpandProperty Name`)
		if err != nil {
			v.addResult(w, "WARN", "Firewall", fmt.Sprintf("Could not query firewall on %s: %v", hostLabel, err), "")
			continue
		}
		enabledProfiles := parseOutputLines(output)
		if len(enabledProfiles) == 0 {
			v.addResult(w, "PASS", "Firewall", fmt.Sprintf("Firewall disabled on %s", hostLabel), "")
		} else {
			v.addResult(w, "FAIL", "Firewall", fmt.Sprintf("Firewall still enabled on %s (profiles: %s)", hostLabel, strings.Join(enabledProfiles, ", ")), "")
		}
	}
}

//go:embed scripts/password_policy.ps1
var scriptPasswordPolicy string

type passwordPolicyResult struct {
	Found            bool   `json:"found"`
	Complexity       bool   `json:"complexity"`
	MinLength        int    `json:"min_length"`
	LockoutThreshold int    `json:"lockout_threshold"`
	Error            string `json:"error"`
}

func (v *Validator) checkPasswordPolicy(ctx context.Context, w io.Writer) {
	printHeader(w, "Password Policy")

	for _, role := range v.lab.DCs() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		r, err := runScriptJSON[passwordPolicyResult](ctx, v, host, scriptPasswordPolicy, nil)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "PasswordPolicy",
				fmt.Sprintf("Could not read password policy on %s: %v", host, err), "")
			continue
		case r.Error != "":
			v.addResult(w, "WARN", "PasswordPolicy",
				fmt.Sprintf("Password policy query error on %s: %s", host, r.Error), "")
			continue
		case !r.Found:
			v.addResult(w, "WARN", "PasswordPolicy",
				fmt.Sprintf("Could not read password policy on %s", host), "")
			continue
		}
		domain := v.lab.DomainForHost(strings.ToLower(host))
		if domain == "" {
			domain = host
		}
		if !r.Complexity {
			v.addResult(w, "PASS", "PasswordPolicy", fmt.Sprintf("Password complexity disabled in %s (weak policy)", domain), "")
		} else {
			v.addResult(w, "INFO", "PasswordPolicy", fmt.Sprintf("Password complexity enabled in %s", domain), "")
		}
		if r.MinLength <= 3 {
			v.addResult(w, "PASS", "PasswordPolicy", fmt.Sprintf("Min password length is %d in %s (weak)", r.MinLength, domain), "")
		} else {
			v.addResult(w, "INFO", "PasswordPolicy", fmt.Sprintf("Min password length is %d in %s", r.MinLength, domain), "")
		}
		if r.LockoutThreshold == 0 {
			v.addResult(w, "PASS", "PasswordPolicy", fmt.Sprintf("No lockout threshold in %s (spray-friendly)", domain), "")
		} else {
			v.addResult(w, "INFO", "PasswordPolicy", fmt.Sprintf("Lockout threshold is %d in %s", r.LockoutThreshold, domain), "")
		}
	}
}

func (v *Validator) checkLDAPSigning(ctx context.Context, w io.Writer) {
	printHeader(w, "LDAP Signing & Channel Binding")

	probes := []struct {
		path        string
		name        string
		passMsg     string
		negotiated  string
		enforcedFmt string
	}{
		{
			path:        `HKLM:\SYSTEM\CurrentControlSet\Services\LDAP`,
			name:        "LDAPServerSigningRequirements",
			passMsg:     "LDAP signing not required in %s (relay possible)",
			negotiated:  "LDAP signing negotiated in %s",
			enforcedFmt: "LDAP signing required (%d) in %s",
		},
		{
			path:        `HKLM:\SYSTEM\CurrentControlSet\Services\NTDS\Parameters`,
			name:        "LDAPServerIntegrity",
			passMsg:     "LDAP server integrity disabled in %s",
			negotiated:  "LDAP server integrity = 1 in %s (SASL only)",
			enforcedFmt: "LDAP server integrity required (%d) in %s",
		},
		{
			path:        `HKLM:\SYSTEM\CurrentControlSet\Services\NTDS\Parameters`,
			name:        "LdapEnforceChannelBindings",
			passMsg:     "LDAP channel binding disabled in %s (relay possible)",
			negotiated:  "LDAP channel binding optional in %s",
			enforcedFmt: "LDAP channel binding enforced (%d) in %s",
		},
	}

	for _, role := range v.lab.DCs() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		domain := v.lab.DomainForHost(strings.ToLower(host))
		if domain == "" {
			domain = host
		}

		for _, p := range probes {
			r, err := runScriptJSON[registryDWORDResult](ctx, v, host, scriptRegistryDWORD,
				map[string]any{"Path": p.path, "Name": p.name})
			switch {
			case err != nil:
				v.addResult(w, "WARN", "LDAP",
					fmt.Sprintf("Could not query %s in %s: %v", p.name, domain, err), "")
			case r.Error != "":
				v.addResult(w, "WARN", "LDAP",
					fmt.Sprintf("%s query error in %s: %s", p.name, domain, r.Error), "")
			case !r.Present || r.Value == 0:
				v.addResult(w, "PASS", "LDAP", fmt.Sprintf(p.passMsg, domain), "")
			case r.Value == 1:
				v.addResult(w, "INFO", "LDAP", fmt.Sprintf(p.negotiated, domain), "")
			default:
				v.addResult(w, "INFO", "LDAP", fmt.Sprintf(p.enforcedFmt, r.Value, domain), "")
			}
		}
	}
}

func (v *Validator) checkRunAsPPL(ctx context.Context, w io.Writer) {
	printHeader(w, "LSASS Protection (RunAsPPL)")

	for _, role := range v.lab.WindowsHosts() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[registryDWORDResult](ctx, v, host, scriptRegistryDWORD,
			map[string]any{
				"Path": `HKLM:\SYSTEM\CurrentControlSet\Control\Lsa`,
				"Name": "RunAsPPL",
			})
		switch {
		case err != nil:
			v.addResult(w, "WARN", "LSAProtection",
				fmt.Sprintf("Could not query RunAsPPL on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "LSAProtection",
				fmt.Sprintf("RunAsPPL query error on %s: %s", hostLabel, r.Error), "")
		case !r.Present || r.Value == 0:
			v.addResult(w, "PASS", "LSAProtection",
				fmt.Sprintf("RunAsPPL disabled on %s (LSASS dumpable)", hostLabel), "")
		case r.Value == 1:
			v.addResult(w, "INFO", "LSAProtection",
				fmt.Sprintf("RunAsPPL enabled on %s (LSASS protected)", hostLabel), "")
		case r.Value == 2:
			v.addResult(w, "INFO", "LSAProtection",
				fmt.Sprintf("RunAsPPL locked on %s (LSASS protected, UEFI)", hostLabel), "")
		default:
			v.addResult(w, "INFO", "LSAProtection",
				fmt.Sprintf("RunAsPPL=%d on %s", r.Value, hostLabel), "")
		}
	}
}

func (v *Validator) checkCertEnrollShare(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS CertEnroll Share")

	adcsHosts := v.lab.ADCSHosts()
	if len(adcsHosts) == 0 {
		v.addResult(w, "SKIP", "CertEnroll", "No ADCS configured for this lab", "")
		return
	}

	for _, role := range adcsHosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output, err := v.runPSErr(ctx, host,
			`Get-SmbShare -Name CertEnroll -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Path`)
		if err != nil {
			v.addResult(w, "WARN", "CertEnroll", fmt.Sprintf("Could not query CertEnroll on %s: %v", hostLabel, err), "")
			continue
		}
		if strings.TrimSpace(output) != "" {
			v.addResult(w, "PASS", "CertEnroll", fmt.Sprintf("CertEnroll share exists on %s (%s)", hostLabel, strings.TrimSpace(output)), "")
		} else {
			v.addResult(w, "FAIL", "CertEnroll", fmt.Sprintf("CertEnroll share NOT found on %s", hostLabel), "")
		}
	}
}

// ---- Section 5: Credential Discovery ----

// checkUsernamePasswordEqual flags AD users whose password equals their
// username (e.g. hodor/hodor) — the canonical "trivial creds" pattern.
func (v *Validator) checkUsernamePasswordEqual(ctx context.Context, w io.Writer) {
	printHeader(w, "Username == Password Users")

	users := v.lab.UsersWithSamePasswordAsName()
	if len(users) == 0 {
		v.addResult(w, "SKIP", "Credentials", "No username==password users configured", "")
		return
	}

	for _, uf := range users {
		dcRole := strings.ToUpper(uf.DCRole)
		if !v.hasHost(dcRole) {
			continue
		}
		var output string
		var probeErr error
		outcome := v.retryMustExist(ctx, func() probeOutcome {
			output, probeErr = runScriptText(ctx, v, dcRole, scriptADUserExists,
				map[string]any{"Username": uf.Username})
			switch {
			case probeErr != nil:
				return probeIncomplete
			case strings.Contains(output, "USER_FOUND"):
				return probePositive
			case strings.Contains(output, "USER_NOT_FOUND"):
				return probeNegative
			default:
				return probeIncomplete
			}
		})
		switch outcome {
		case probePositive:
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("%s (password=%s) exists in %s", uf.Username, uf.User.Password, uf.Domain), "")
		case probeNegative:
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("%s does NOT exist in %s (expected weak-cred user)", uf.Username, uf.Domain), "")
		default:
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Could not verify %s in %s: %v", uf.Username, uf.Domain, probeErr), "")
		}
	}
}

// checkAutologonRegistry verifies AutoAdminLogon is enabled with stored
// credentials on hosts running the vulns_autologon role. On Windows
// 10/Server 2016+, ansible.windows.win_auto_logon stores DefaultPassword
// in the LSA secret store rather than the registry, so an empty registry
// password value with AAL=1 + DefaultUserName populated is the *correct*
// post-provisioning state, not a misconfiguration.
func (v *Validator) checkAutologonRegistry(ctx context.Context, w io.Writer) {
	printHeader(w, "Autologon Registry Credentials")

	hosts := v.lab.HostsWithVuln("autologon")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "Credentials", "No autologon vulns configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[autologonResult](ctx, v, host, scriptAutologonRegistry, nil)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Autologon registry unreadable on %s: %v", hostLabel, err), "")
			continue
		case r.Error != "":
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Autologon probe error on %s: %s", hostLabel, r.Error), "")
			continue
		}
		hasRegPw := r.PWLength > 0
		switch {
		case r.AAL == 1 && r.User != "" && hasRegPw:
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("Autologon enabled on %s (user=%s, pw in registry)", hostLabel, r.User), "")
		case r.AAL == 1 && r.User != "":
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("Autologon enabled on %s (user=%s, pw in LSA secret)", hostLabel, r.User), "")
		case r.AAL == 1:
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("AutoAdminLogon=1 on %s but DefaultUserName missing", hostLabel), "")
		default:
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("AutoAdminLogon NOT enabled on %s (AAL=%d)", hostLabel, r.AAL), "")
		}
	}
}

//go:embed scripts/autologon_registry.ps1
var scriptAutologonRegistry string

type autologonResult struct {
	AAL      int    `json:"aal"`
	User     string `json:"user"`
	PWLength int    `json:"pw_length"`
	Error    string `json:"error"`
}

// checkCmdkeyCredentials verifies stored Credential Manager entries
// populated by the vulns_credentials role. cmdkey writes to the *invoking
// user's* DPAPI vault, but SSM commands run as SYSTEM and only see the
// SYSTEM vault — so `cmdkey /list` is blind to per-user creds. Instead we
// enumerate vault files under each user profile (we cannot decrypt them
// without that user's DPAPI master key, but their existence proves cmdkey
// ran). The role stores under the configured `runas` user, so we cross-
// check the configured users against the profiles that actually contain
// vault files.
func (v *Validator) checkCmdkeyCredentials(ctx context.Context, w io.Writer) {
	printHeader(w, "Stored Credential Manager Entries")

	hosts := v.lab.HostsWithVuln("credentials")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "Credentials", "No credentials vulns configured", "")
		return
	}

	const vaultEnumQuery = `$paths = @('C:\Users\*\AppData\Local\Microsoft\Credentials\*','C:\Users\*\AppData\Roaming\Microsoft\Credentials\*'); ` +
		`$found = @{}; ` +
		`foreach ($p in $paths) { ` +
		`  foreach ($f in (Get-ChildItem -Path $p -ErrorAction SilentlyContinue -Force)) { ` +
		`    $u = ($f.FullName -split '\\')[2].ToLower(); ` +
		`    $found[$u] = $true ` +
		`  } ` +
		`}; ` +
		`if ($found.Count -eq 0) { 'NONE' } else { 'USERS=' + (($found.Keys | Sort-Object) -join ',') }`

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))

		output, err := v.runPSErr(ctx, host, vaultEnumQuery)
		output = strings.TrimSpace(output)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Could not enumerate credential vault on %s: %v", hostLabel, err), "")
			continue
		case output == "":
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Could not enumerate credential vault on %s", hostLabel), "")
		case strings.HasPrefix(output, "USERS="):
			users := strings.Split(strings.TrimPrefix(output, "USERS="), ",")
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("Credential vault populated on %s (users: %s)", hostLabel, strings.Join(users, ", ")), "")
		default: // "NONE"
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("No stored credentials on %s", hostLabel), "")
		}
	}
}

//go:embed scripts/sysvol_plaintext.ps1
var scriptSysvolPlaintext string

//go:embed scripts/shares_file_count.ps1
var scriptSharesFileCount string

//go:embed scripts/share_permissive_acls.ps1
var scriptSharePermissiveACLs string

//go:embed scripts/admin_profile_acl.ps1
var scriptAdminProfileACL string

type sysvolPlaintextResult struct {
	RootPresent bool     `json:"root_present"`
	FileCount   int      `json:"file_count"`
	Files       []string `json:"files"`
	Error       string   `json:"error"`
}

type sharesFileCountResult struct {
	RootPresent bool   `json:"root_present"`
	FileCount   int    `json:"file_count"`
	Error       string `json:"error"`
}

type sharePermissiveACLsResult struct {
	Entries []struct {
		Path     string `json:"path"`
		Identity string `json:"identity"`
		Rights   string `json:"rights"`
	} `json:"entries"`
	Error string `json:"error"`
}

type adminProfileACLResult struct {
	Found        bool   `json:"found"`
	NonAdminRead bool   `json:"non_admin_read"`
	Error        string `json:"error"`
}

// checkSysvolPlaintext scans SYSVOL for plaintext-credential markers on DCs
// where the vulns_directory or vulns_files role staged scripts.
func (v *Validator) checkSysvolPlaintext(ctx context.Context, w io.Writer) {
	printHeader(w, "SYSVOL Plaintext Credentials")

	candidate := make(map[string]bool)
	for _, role := range v.lab.HostsWithVuln("directory") {
		candidate[role] = true
	}
	for _, role := range v.lab.HostsWithVuln("files") {
		candidate[role] = true
	}

	var dcRoles []string
	for role := range candidate {
		hc, ok := v.lab.HostConfigs[role]
		if !ok || hc.Type != "dc" {
			continue
		}
		dcRoles = append(dcRoles, role)
	}
	if len(dcRoles) == 0 {
		v.addResult(w, "SKIP", "Credentials", "No DCs with directory/files vulns configured", "")
		return
	}

	for _, role := range dcRoles {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[sysvolPlaintextResult](ctx, v, host, scriptSysvolPlaintext, nil)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Could not enumerate SYSVOL on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("SYSVOL scan error on %s: %s", hostLabel, r.Error), "")
		case !r.RootPresent:
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("SYSVOL not present on %s", hostLabel), "")
		case r.FileCount == 0:
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("No plaintext credential markers in SYSVOL on %s", hostLabel), "")
		default:
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("SYSVOL plaintext markers on %s in %d file(s)", hostLabel, r.FileCount), "")
		}
	}
}

// checkShareFilePlaintext enumerates writable shares populated by the
// vulns_files role for plaintext-credential drops.
func (v *Validator) checkShareFilePlaintext(ctx context.Context, w io.Writer) {
	printHeader(w, "Share File Plaintext Credentials")

	hosts := v.lab.HostsWithVuln("files")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "Credentials", "No files vulns configured", "")
		return
	}

	for _, role := range hosts {
		hc, ok := v.lab.HostConfigs[role]
		if !ok || hc.Type == "dc" {
			// DCs are covered by checkSysvolPlaintext.
			continue
		}
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[sharesFileCountResult](ctx, v, host, scriptSharesFileCount, nil)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Could not enumerate shares on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Share enum error on %s: %s", hostLabel, r.Error), "")
		case !r.RootPresent:
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("C:\\shares missing on %s", hostLabel), "")
		case r.FileCount == 0:
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("No files in C:\\shares on %s", hostLabel), "")
		default:
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("%d file(s) staged under C:\\shares on %s", r.FileCount, hostLabel), "")
		}
	}
}

// checkSharePermissions verifies vulns_permissions ACL grants land on disk
// (e.g. IIS_IUSRS / Authenticated Users with Modify on share folders).
func (v *Validator) checkSharePermissions(ctx context.Context, w io.Writer) {
	printHeader(w, "Share Permission ACLs")

	hosts := v.lab.HostsWithVuln("permissions")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "Permissions", "No permissions vulns configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[sharePermissiveACLsResult](ctx, v, host, scriptSharePermissiveACLs, nil)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "Permissions",
				fmt.Sprintf("Could not read share ACLs on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "Permissions",
				fmt.Sprintf("Share ACL query error on %s: %s", hostLabel, r.Error), "")
		case len(r.Entries) == 0:
			v.addResult(w, "FAIL", "Permissions",
				fmt.Sprintf("No permissive share ACEs on %s", hostLabel), "")
		default:
			v.addResult(w, "PASS", "Permissions",
				fmt.Sprintf("Permissive ACEs on %s: %d entries", hostLabel, len(r.Entries)), "")
		}
	}
}

// checkAdministratorFolder verifies the C:\users\administrator folder exists
// and is readable by non-admin (the vulns_administrator_folder role disables
// inheritance and grants only admin rights, so listing without admin should
// still surface the directory's existence).
func (v *Validator) checkAdministratorFolder(ctx context.Context, w io.Writer) {
	printHeader(w, "Administrator Profile Folder")

	hosts := v.lab.HostsWithVuln("administrator_folder")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "Credentials", "No administrator_folder vulns configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[adminProfileACLResult](ctx, v, host, scriptAdminProfileACL, nil)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Could not verify administrator folder on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Administrator folder query error on %s: %s", hostLabel, r.Error), "")
		case !r.Found:
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("C:\\users\\administrator missing on %s", hostLabel), "")
		case r.NonAdminRead:
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("C:\\users\\administrator readable by non-admin on %s", hostLabel), "")
		default:
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("C:\\users\\administrator exists on %s (admin-only ACL)", hostLabel), "")
		}
	}
}

// ---- Section 6: Network Poisoning / Hardening ----

//go:embed scripts/smb1_feature.ps1
var scriptSMB1Feature string

type smb1FeatureResult struct {
	Found bool   `json:"found"`
	State string `json:"state"`
	Error string `json:"error"`
}

// checkSMBv1 verifies the legacy SMB1 protocol feature is enabled by the
// vulns_smbv1 role.
func (v *Validator) checkSMBv1(ctx context.Context, w io.Writer) {
	printHeader(w, "SMBv1 Protocol")

	hosts := v.lab.HostsWithVuln("smbv1")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "Network", "No smbv1 vulns configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[smb1FeatureResult](ctx, v, host, scriptSMB1Feature, nil)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "Network",
				fmt.Sprintf("Could not query SMB1 feature on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "Network",
				fmt.Sprintf("SMB1 query error on %s: %s", hostLabel, r.Error), "")
		case !r.Found:
			v.addResult(w, "WARN", "Network",
				fmt.Sprintf("SMBv1 feature unknown on %s", hostLabel), "")
		case r.State == "Enabled":
			v.addResult(w, "PASS", "Network",
				fmt.Sprintf("SMBv1 enabled on %s (legacy auth/relay possible)", hostLabel), "")
		case r.State == "Disabled":
			v.addResult(w, "FAIL", "Network",
				fmt.Sprintf("SMBv1 disabled on %s", hostLabel), "")
		default:
			v.addResult(w, "INFO", "Network",
				fmt.Sprintf("SMBv1 state %s on %s", r.State, hostLabel), "")
		}
	}
}

//go:embed scripts/credssp_server.ps1
var scriptCredSSPServer string

//go:embed scripts/credssp_client.ps1
var scriptCredSSPClient string

type credsspServerResult struct {
	Present bool   `json:"present"`
	Value   int    `json:"value"`
	Source  string `json:"source"`
	Error   string `json:"error"`
}

type credsspClientResult struct {
	PolicyPresent bool   `json:"policy_present"`
	AFCPresent    bool   `json:"afc_present"`
	AFC           int    `json:"afc"`
	Error         string `json:"error"`
}

// checkCredSSP verifies WSMAN CredSSP is enabled on server/client hosts via
// vulns_enable_credssp_server / vulns_enable_credssp_client.
func (v *Validator) checkCredSSP(ctx context.Context, w io.Writer) {
	printHeader(w, "CredSSP (WSMAN)")

	servers := v.lab.HostsWithVuln("enable_credssp_server")
	clients := v.lab.HostsWithVuln("enable_credssp_client")
	if len(servers) == 0 && len(clients) == 0 {
		v.addResult(w, "SKIP", "Network", "No CredSSP vulns configured", "")
		return
	}

	for _, role := range servers {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		v.checkCredSSPServerHost(ctx, w, host, strings.ToUpper(v.lab.Hostname(role)))
	}

	for _, role := range clients {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		v.checkCredSSPClientHost(ctx, w, host, strings.ToUpper(v.lab.Hostname(role)))
	}
}

func (v *Validator) checkCredSSPServerHost(ctx context.Context, w io.Writer, host, hostLabel string) {
	r, err := runScriptJSON[credsspServerResult](ctx, v, host, scriptCredSSPServer, nil)
	switch {
	case err != nil:
		v.addResult(w, "WARN", "Network",
			fmt.Sprintf("Could not query CredSSP server on %s: %v", hostLabel, err), "")
	case r.Error != "":
		v.addResult(w, "WARN", "Network",
			fmt.Sprintf("CredSSP server query error on %s: %s", hostLabel, r.Error), "")
	case !r.Present:
		v.addResult(w, "WARN", "Network",
			fmt.Sprintf("CredSSP server not configured on %s", hostLabel), "")
	case r.Value == 1:
		v.addResult(w, "PASS", "Network",
			fmt.Sprintf("CredSSP server enabled on %s (relay target)", hostLabel), "")
	case r.Value == 0:
		v.addResult(w, "FAIL", "Network",
			fmt.Sprintf("CredSSP server disabled on %s", hostLabel), "")
	default:
		v.addResult(w, "INFO", "Network",
			fmt.Sprintf("CredSSP server value=%d on %s", r.Value, hostLabel), "")
	}
}

func (v *Validator) checkCredSSPClientHost(ctx context.Context, w io.Writer, host, hostLabel string) {
	r, err := runScriptJSON[credsspClientResult](ctx, v, host, scriptCredSSPClient, nil)
	switch {
	case err != nil:
		v.addResult(w, "WARN", "Network",
			fmt.Sprintf("Could not read CredSSP client policy on %s: %v", hostLabel, err), "")
	case r.Error != "":
		v.addResult(w, "WARN", "Network",
			fmt.Sprintf("CredSSP client query error on %s: %s", hostLabel, r.Error), "")
	case !r.PolicyPresent:
		v.addResult(w, "FAIL", "Network",
			fmt.Sprintf("CredSSP client policy missing on %s", hostLabel), "")
	case !r.AFCPresent, r.AFC == 0:
		v.addResult(w, "FAIL", "Network",
			fmt.Sprintf("CredSSP client AllowFreshCredentials disabled on %s", hostLabel), "")
	case r.AFC == 1:
		v.addResult(w, "PASS", "Network",
			fmt.Sprintf("CredSSP client AllowFreshCredentials=1 on %s", hostLabel), "")
	default:
		v.addResult(w, "INFO", "Network",
			fmt.Sprintf("CredSSP client AllowFreshCredentials=%d on %s", r.AFC, hostLabel), "")
	}
}

//go:embed scripts/webdav_redirector.ps1
var scriptWebDAVRedirector string

type webDAVRedirectorResult struct {
	Found bool   `json:"found"`
	State string `json:"state"`
	Error string `json:"error"`
}

// checkWebDAVRedirector confirms the WebDAV-Redirector feature is installed
// on hosts running the webdav role (enables HTTP-auth coercion).
func (v *Validator) checkWebDAVRedirector(ctx context.Context, w io.Writer) {
	printHeader(w, "WebDAV-Redirector Feature")

	servers := v.lab.WindowsServers()
	if len(servers) == 0 {
		v.addResult(w, "SKIP", "Network", "No Windows servers configured", "")
		return
	}

	found := false
	for _, role := range servers {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[webDAVRedirectorResult](ctx, v, host, scriptWebDAVRedirector, nil)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "Network",
				fmt.Sprintf("Could not query WebDAV-Redirector on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "Network",
				fmt.Sprintf("WebDAV-Redirector query error on %s: %s", hostLabel, r.Error), "")
		case !r.Found:
			v.addResult(w, "INFO", "Network",
				fmt.Sprintf("WebDAV-Redirector feature not present on %s", hostLabel), "")
		case r.State == "Installed":
			found = true
			v.addResult(w, "PASS", "Network",
				fmt.Sprintf("WebDAV-Redirector installed on %s", hostLabel), "")
		case r.State == "Available", r.State == "Removed":
			v.addResult(w, "INFO", "Network",
				fmt.Sprintf("WebDAV-Redirector available but not installed on %s", hostLabel), "")
		default:
			v.addResult(w, "INFO", "Network",
				fmt.Sprintf("WebDAV-Redirector state %s on %s", r.State, hostLabel), "")
		}
	}
	if !found {
		v.addResult(w, "INFO", "Network", "WebDAV-Redirector not installed on any Windows server", "")
	}
}

// ---- Section 8: ADCS template flags ----

// adcsTemplateAttr queries a single attribute on a named cert template using
// the configuration naming context. Returns trimmed PowerShell output.
func (v *Validator) adcsTemplateAttr(ctx context.Context, dcRole, templateName, attr string) string {
	out, err := runScriptText(ctx, v, dcRole,
		`$t = Get-ADObject -Filter {cn -eq {{psq .Template}} -and objectClass -eq 'pKICertificateTemplate'} `+
			`-SearchBase ("CN=Certificate Templates,CN=Public Key Services,CN=Services," + (Get-ADRootDSE).configurationNamingContext) `+
			`-Properties {{psq .Attr}} -ErrorAction SilentlyContinue; `+
			`if (-not $t) { Write-Output 'TEMPLATE_NOT_FOUND'; exit }; `+
			`$val = $t.{{psq .Attr}}; `+
			`if ($val -is [System.Array]) { $val -join ',' } else { Write-Output $val }`,
		map[string]any{"Template": templateName, "Attr": attr})
	if err != nil {
		return ""
	}
	return out
}

// adcsTemplateDCs returns the DC roles that have at least one ADCS template
// queryable. We pick any DC associated with an ADCS host.
func (v *Validator) adcsTemplateDCs() []string {
	dcs := make(map[string]bool)
	for _, adcsRole := range v.lab.ADCSHosts() {
		if dcRole := v.lab.ADCSDCRole(adcsRole); dcRole != "" {
			dcs[dcRole] = true
		}
	}
	// Fall back to all DCs if mapping was empty.
	if len(dcs) == 0 {
		for _, role := range v.lab.DCs() {
			dcs[role] = true
		}
	}
	var out []string
	for r := range dcs {
		out = append(out, r)
	}
	return out
}

// hasADCSTemplate returns true if the variant's config.json declares the named
// template (e.g. "ESC1") in any host's vulns_vars.adcs_templates.
func (v *Validator) hasADCSTemplate(name string) bool {
	return v.lab.ADCSConfiguredTemplates()[strings.ToUpper(name)]
}

// checkADCSESC1 verifies the ESC1 template has CT_FLAG_ENROLLEE_SUPPLIES_SUBJECT
// (msPKI-Certificate-Name-Flag bit 0x1) set.
func (v *Validator) checkADCSESC1(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC1 - ENROLLEE_SUPPLIES_SUBJECT")

	dcs := v.adcsTemplateDCs()
	if len(dcs) == 0 || len(v.lab.ADCSHosts()) == 0 {
		v.addResult(w, "SKIP", "ADCS-ESC1", "No ADCS configured for this lab", "")
		return
	}
	if !v.hasADCSTemplate("ESC1") {
		v.addResult(w, "SKIP", "ADCS-ESC1", "ESC1 template not in variant config", "")
		return
	}

	for _, dcRole := range dcs {
		dc := strings.ToUpper(dcRole)
		if !v.hasHost(dc) {
			continue
		}
		output := v.adcsTemplateAttr(ctx, dc, "ESC1", "msPKI-Certificate-Name-Flag")
		switch {
		case strings.Contains(output, "TEMPLATE_NOT_FOUND"):
			v.addResult(w, "INFO", "ADCS-ESC1",
				fmt.Sprintf("ESC1 template not present on %s", dc), "")
		case strings.TrimSpace(output) == "":
			v.addResult(w, "WARN", "ADCS-ESC1",
				fmt.Sprintf("Could not read ESC1 template on %s", dc), "")
		default:
			val := strings.TrimSpace(output)
			// The ENROLLEE_SUPPLIES_SUBJECT flag is bit 0x1 (decimal 1). The
			// stored value can be a positive or two's-complement int.
			if strings.Contains(val, "1") && (val == "1" || hasBitOne(val)) {
				v.addResult(w, "PASS", "ADCS-ESC1",
					fmt.Sprintf("ESC1 has ENROLLEE_SUPPLIES_SUBJECT (flag=%s) on %s", val, dc), "")
			} else {
				v.addResult(w, "FAIL", "ADCS-ESC1",
					fmt.Sprintf("ESC1 missing ENROLLEE_SUPPLIES_SUBJECT (flag=%s) on %s", val, dc), "")
			}
		}
	}
}

// hasBitOne returns true if the decimal string represents an integer with
// bit 0 set (i.e. odd value).
func hasBitOne(decimal string) bool {
	s := strings.TrimSpace(decimal)
	if s == "" {
		return false
	}
	last := s[len(s)-1]
	return last == '1' || last == '3' || last == '5' || last == '7' || last == '9'
}

// checkADCSESC2 verifies the ESC2 template lists Any Purpose (2.5.29.37.0)
// in pKIExtendedKeyUsage.
func (v *Validator) checkADCSESC2(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC2 - Any Purpose EKU")

	dcs := v.adcsTemplateDCs()
	if len(dcs) == 0 || len(v.lab.ADCSHosts()) == 0 {
		v.addResult(w, "SKIP", "ADCS-ESC2", "No ADCS configured for this lab", "")
		return
	}
	if !v.hasADCSTemplate("ESC2") {
		v.addResult(w, "SKIP", "ADCS-ESC2", "ESC2 template not in variant config", "")
		return
	}

	for _, dcRole := range dcs {
		dc := strings.ToUpper(dcRole)
		if !v.hasHost(dc) {
			continue
		}
		output := v.adcsTemplateAttr(ctx, dc, "ESC2", "pKIExtendedKeyUsage")
		switch {
		case strings.Contains(output, "TEMPLATE_NOT_FOUND"):
			v.addResult(w, "INFO", "ADCS-ESC2",
				fmt.Sprintf("ESC2 template not present on %s", dc), "")
		case strings.Contains(output, "2.5.29.37.0"):
			v.addResult(w, "PASS", "ADCS-ESC2",
				fmt.Sprintf("ESC2 has Any Purpose EKU on %s", dc), "")
		case strings.TrimSpace(output) == "":
			v.addResult(w, "WARN", "ADCS-ESC2",
				fmt.Sprintf("Could not read ESC2 template EKU on %s", dc), "")
		default:
			v.addResult(w, "FAIL", "ADCS-ESC2",
				fmt.Sprintf("ESC2 missing Any Purpose EKU on %s (got %s)", dc, strings.TrimSpace(output)), "")
		}
	}
}

// checkADCSESC3 verifies the ESC3-CRA template lists Certificate Request Agent
// (1.3.6.1.4.1.311.20.2.1) in pKIExtendedKeyUsage.
func (v *Validator) checkADCSESC3(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC3 - Certificate Request Agent EKU")

	dcs := v.adcsTemplateDCs()
	if len(dcs) == 0 || len(v.lab.ADCSHosts()) == 0 {
		v.addResult(w, "SKIP", "ADCS-ESC3", "No ADCS configured for this lab", "")
		return
	}
	if !v.hasADCSTemplate("ESC3") && !v.hasADCSTemplate("ESC3-CRA") {
		v.addResult(w, "SKIP", "ADCS-ESC3", "ESC3 template not in variant config", "")
		return
	}

	for _, dcRole := range dcs {
		dc := strings.ToUpper(dcRole)
		if !v.hasHost(dc) {
			continue
		}
		// Try ESC3-CRA first; fall back to ESC3.
		out := v.adcsTemplateAttr(ctx, dc, "ESC3-CRA", "pKIExtendedKeyUsage")
		tmpl := "ESC3-CRA"
		if strings.Contains(out, "TEMPLATE_NOT_FOUND") {
			out = v.adcsTemplateAttr(ctx, dc, "ESC3", "pKIExtendedKeyUsage")
			tmpl = "ESC3"
		}
		switch {
		case strings.Contains(out, "TEMPLATE_NOT_FOUND"):
			v.addResult(w, "INFO", "ADCS-ESC3",
				fmt.Sprintf("ESC3/ESC3-CRA template not present on %s", dc), "")
		case strings.Contains(out, "1.3.6.1.4.1.311.20.2.1"):
			v.addResult(w, "PASS", "ADCS-ESC3",
				fmt.Sprintf("%s has Certificate Request Agent EKU on %s", tmpl, dc), "")
		case strings.TrimSpace(out) == "":
			v.addResult(w, "WARN", "ADCS-ESC3",
				fmt.Sprintf("Could not read %s template EKU on %s", tmpl, dc), "")
		default:
			v.addResult(w, "FAIL", "ADCS-ESC3",
				fmt.Sprintf("%s missing CRA EKU on %s (got %s)", tmpl, dc, strings.TrimSpace(out)), "")
		}
	}
}

// checkADCSESC4 verifies a non-default identity has GenericAll on the ESC4
// template (typically khal.drogo per config.json).
func (v *Validator) checkADCSESC4(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC4 - Template ACL Abuse")

	if len(v.lab.ADCSHosts()) == 0 {
		v.addResult(w, "SKIP", "ADCS-ESC4", "No ADCS configured for this lab", "")
		return
	}
	if !v.hasADCSTemplate("ESC4") {
		v.addResult(w, "SKIP", "ADCS-ESC4", "ESC4 template not in variant config", "")
		return
	}

	// Look for ACLs whose target is the ESC4 template DN.
	var grantees []labmapACL
	for _, af := range v.lab.AllACLs() {
		if strings.Contains(strings.ToUpper(af.ACL.To), "CN=ESC4,CN=CERTIFICATE TEMPLATES") {
			grantees = append(grantees, labmapACL{Domain: af.Domain, DCRole: af.DCRole, Source: af.ACL.For, Right: af.ACL.Right})
		}
	}

	dcs := v.adcsTemplateDCs()
	if len(grantees) == 0 {
		for _, dcRole := range dcs {
			dc := strings.ToUpper(dcRole)
			if !v.hasHost(dc) {
				continue
			}
			v.scanESC4ACL(ctx, w, dc)
		}
		return
	}

	for _, g := range grantees {
		dc := strings.ToUpper(g.DCRole)
		if !v.hasHost(dc) {
			continue
		}
		v.checkESC4Grantee(ctx, w, dc, g)
	}
}

func (v *Validator) scanESC4ACL(ctx context.Context, w io.Writer, dc string) {
	output, err := v.runPSErr(ctx, dc,
		`$t = Get-ADObject -Filter {cn -eq 'ESC4' -and objectClass -eq 'pKICertificateTemplate'} `+
			`-SearchBase ("CN=Certificate Templates,CN=Public Key Services,CN=Services," + (Get-ADRootDSE).configurationNamingContext) `+
			`-ErrorAction SilentlyContinue; `+
			`if (-not $t) { Write-Output 'TEMPLATE_NOT_FOUND'; exit }; `+
			`Import-Module ActiveDirectory; Set-Location AD:; `+
			`$acl = Get-Acl -Path $t.DistinguishedName; `+
			`$bad = $acl.Access | Where-Object { `+
			`$_.IdentityReference -notmatch 'Domain Admins|Enterprise Admins|SYSTEM|Authenticated Users|Domain Users|Administrators|Cert Publishers' -and `+
			`$_.ActiveDirectoryRights -match 'GenericAll|WriteDacl|WriteOwner' }; `+
			`if ($bad) { $bad | ForEach-Object { Write-Output ("$($_.IdentityReference)|$($_.ActiveDirectoryRights)") } } `+
			`else { Write-Output 'NO_ABUSE' }`)
	if err != nil {
		v.addResult(w, "WARN", "ADCS-ESC4",
			fmt.Sprintf("Could not read ESC4 template ACL on %s: %v", dc, err), "")
		return
	}
	switch {
	case strings.Contains(output, "TEMPLATE_NOT_FOUND"):
		v.addResult(w, "INFO", "ADCS-ESC4",
			fmt.Sprintf("ESC4 template not present on %s", dc), "")
	case strings.Contains(output, "NO_ABUSE"):
		v.addResult(w, "FAIL", "ADCS-ESC4",
			fmt.Sprintf("No abusable ACE on ESC4 template on %s", dc), "")
	case strings.TrimSpace(output) == "":
		v.addResult(w, "WARN", "ADCS-ESC4",
			fmt.Sprintf("Could not read ESC4 template ACL on %s", dc), "")
	default:
		lines := parseOutputLines(output)
		v.addResult(w, "PASS", "ADCS-ESC4",
			fmt.Sprintf("ESC4 abusable ACEs on %s: %s", dc, strings.Join(lines, "; ")), "")
	}
}

func (v *Validator) checkESC4Grantee(ctx context.Context, w io.Writer, dc string, g labmapACL) {
	source := v.lab.User(g.Source)
	output, err := runScriptText(ctx, v, dc,
		`$t = Get-ADObject -Filter {cn -eq 'ESC4' -and objectClass -eq 'pKICertificateTemplate'} `+
			`-SearchBase ("CN=Certificate Templates,CN=Public Key Services,CN=Services," + (Get-ADRootDSE).configurationNamingContext) `+
			`-ErrorAction SilentlyContinue; `+
			`if (-not $t) { Write-Output 'TEMPLATE_NOT_FOUND'; exit }; `+
			`Import-Module ActiveDirectory; Set-Location AD:; `+
			`$acl = Get-Acl -Path $t.DistinguishedName; `+
			`$idPattern = '*' + {{psq .Source}} + '*'; `+
			`$ace = $acl.Access | Where-Object { $_.IdentityReference -like $idPattern -and $_.ActiveDirectoryRights -match {{psq .Right}} }; `+
			`if ($ace) { Write-Output 'ACL_FOUND' } else { Write-Output 'ACL_NOT_FOUND' }`,
		map[string]any{"Source": source, "Right": g.Right})
	if err != nil {
		v.addResult(w, "WARN", "ADCS-ESC4",
			fmt.Sprintf("Could not verify ESC4 ACL for %s in %s: %v", source, g.Domain, err), "")
		return
	}
	switch {
	case strings.Contains(output, "TEMPLATE_NOT_FOUND"):
		v.addResult(w, "INFO", "ADCS-ESC4",
			fmt.Sprintf("ESC4 template not present on %s", dc), "")
	case strings.Contains(output, "ACL_FOUND"):
		v.addResult(w, "PASS", "ADCS-ESC4",
			fmt.Sprintf("%s has %s on ESC4 template (%s)", source, g.Right, g.Domain), "")
	case strings.Contains(output, "ACL_NOT_FOUND"):
		v.addResult(w, "FAIL", "ADCS-ESC4",
			fmt.Sprintf("%s does NOT have %s on ESC4 template (%s)", source, g.Right, g.Domain), "")
	default:
		v.addResult(w, "WARN", "ADCS-ESC4",
			fmt.Sprintf("Could not verify ESC4 ACL for %s in %s", source, g.Domain), "")
	}
}

// labmapACL is a flattened view of an ACL grant for a single template/object.
type labmapACL struct {
	Domain string
	DCRole string
	Source string
	Right  string
}

// checkADCSESC9 verifies pre-conditions for ESC9 abuse: a configured user
// (typically missandei) has DoesNotRequirePreAuth set, and the GenericAll
// ACL chain (missandei -> khal.drogo) exists. ACL checks already run in
// checkACLPermissions; here we focus on the user attribute.
func (v *Validator) checkADCSESC9(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC9 - Pre-auth + ACL Chain")

	if len(v.lab.ADCSHosts()) == 0 {
		v.addResult(w, "SKIP", "ADCS-ESC9", "No ADCS configured for this lab", "")
		return
	}
	if !v.hasADCSTemplate("ESC9") {
		v.addResult(w, "SKIP", "ADCS-ESC9", "ESC9 template not in variant config", "")
		return
	}

	// AS-REP roasting is opt-in per DC via an `asrep_roasting*` script in
	// the host's scripts list. Only DCs that opt in are expected to expose
	// a DONT_REQ_PREAUTH pivot user; the rest get an INFO note rather than
	// a spurious FAIL.
	asrepDCs := make(map[string]bool)
	for _, role := range v.lab.HostsWithScript("asrep_roasting") {
		asrepDCs[strings.ToUpper(role)] = true
	}

	found := false
	for _, role := range v.lab.DCs() {
		dc := strings.ToUpper(role)
		if !v.hasHost(dc) {
			continue
		}
		domain := v.lab.DomainForHost(strings.ToLower(dc))
		if domain == "" {
			domain = dc
		}
		if !asrepDCs[dc] {
			v.addResult(w, "INFO", "ADCS-ESC9",
				fmt.Sprintf("AS-REP roasting not configured in %s (no ESC9 pivot expected)", domain), "")
			continue
		}
		output, err := v.runPSErr(ctx, dc,
			`Get-ADUser -Filter {DoesNotRequirePreAuth -eq $true} -Properties DoesNotRequirePreAuth | `+
				`Select-Object -ExpandProperty SamAccountName`)
		if err != nil {
			v.addResult(w, "WARN", "ADCS-ESC9",
				fmt.Sprintf("Could not query DONT_REQ_PREAUTH users in %s: %v", domain, err), "")
			continue
		}
		users := parseOutputLines(output)
		if len(users) == 0 {
			v.addResult(w, "FAIL", "ADCS-ESC9",
				fmt.Sprintf("No DONT_REQ_PREAUTH users in %s (no ESC9 pivot)", domain), "")
			continue
		}
		found = true
		v.addResult(w, "PASS", "ADCS-ESC9",
			fmt.Sprintf("ESC9 pivot users in %s: %s", domain, strings.Join(users, ", ")), "")
	}
	if len(asrepDCs) > 0 && !found {
		v.addResult(w, "FAIL", "ADCS-ESC9", "No ESC9 pivot users found in any AS-REP-configured domain", "")
	}

	v.checkESC9Enforcement(ctx, w)
}

// ctFlagNoSecurityExtension is CT_FLAG_NO_SECURITY_EXTENSION in
// msPKI-Enrollment-Flag: the bit that makes a template an ESC9 template by
// omitting szOID_NTDS_CA_SECURITY_EXT from every certificate it issues.
const ctFlagNoSecurityExtension = 0x00080000

// checkESC9Enforcement verifies the two conditions the pivot user does not
// establish: that the ESC9 template really drops the SID security extension,
// and that the KDC which will see the resulting certificate still accepts a
// weak mapping.
//
// Both are checked per template DC, and the template is checked first so labs
// that publish no ESC9 template (GOAD-Light, GOAD-Mini, NHA) report INFO rather
// than a KDC verdict about a route they never shipped.
func (v *Validator) checkESC9Enforcement(ctx context.Context, w io.Writer) {
	for _, dcRole := range v.adcsTemplateDCs() {
		dc := strings.ToUpper(dcRole)
		if !v.hasHost(dc) {
			continue
		}
		output := v.adcsTemplateAttr(ctx, dc, "ESC9", "msPKI-Enrollment-Flag")
		val := strings.TrimSpace(output)
		flag, parseErr := strconv.ParseInt(val, 10, 64)
		switch {
		case strings.Contains(output, "TEMPLATE_NOT_FOUND"):
			v.addResult(w, "INFO", "ADCS-ESC9",
				fmt.Sprintf("ESC9 template not present on %s", dc), "")
		case val == "" || parseErr != nil:
			v.addResult(w, "WARN", "ADCS-ESC9",
				fmt.Sprintf("Could not read ESC9 template msPKI-Enrollment-Flag on %s", dc), "")
		case uint32(flag)&ctFlagNoSecurityExtension == 0:
			v.addResult(w, "FAIL", "ADCS-ESC9",
				fmt.Sprintf("ESC9 template on %s lacks CT_FLAG_NO_SECURITY_EXTENSION (msPKI-Enrollment-Flag=%s)", dc, val), "")
		default:
			v.checkESC9KDCBinding(ctx, w, dcRole)
		}
	}
}

// checkESC9KDCBinding decides whether an issued ESC9 certificate can convert to
// a TGT, which the template flag does not establish.
//
// CT_FLAG_NO_SECURITY_EXTENSION works by *removing* szOID_NTDS_CA_SECURITY_EXT
// from the issued certificate, leaving the KDC no SID to bind and forcing the
// weak UPN mapping the attack spoofs. StrongCertificateBindingEnforcement 0
// (Disabled) and 1 (Compatibility) both allow that fallback; 2 (Full
// Enforcement) refuses any certificate it cannot map strongly, so stripping the
// extension is itself disqualifying. The pass condition is !=2, unlike ESC6's
// ==0: ESC6 also loses at 1, because its certificate *has* a security extension
// and a present extension is validated strictly even in Compatibility mode.
//
// This gate is independent of the ACL chain, and deliberately so. Enrolling the
// ESC9 template on staging as an ordinary domain user, with no UPN spoof and no
// -sid, yielded a certificate with no object SID whose AS-REQ the KDC dropped
// with event 39 at Error level. Nothing about the UPN-write primitive was in
// play, so an ESC9 verdict that waits on the ACL chain waits on the wrong
// blocker.
func (v *Validator) checkESC9KDCBinding(ctx context.Context, w io.Writer, dcRole string) {
	b, err := v.readKDCBinding(ctx, dcRole)
	switch {
	case err != nil:
		v.addResult(w, "WARN", "ADCS-ESC9",
			fmt.Sprintf("Cannot confirm ESC9 is exploitable: %s", err), "")
	case !b.Present:
		v.addResult(w, "FAIL", "ADCS-ESC9",
			fmt.Sprintf("%s %s, which refuses a certificate carrying no SID (ESC9 NOT exploitable); pin it with the adcs_esc10_case1 vuln", b.DCLabel, unpinnedKDCNote), "")
	case b.Value >= 2:
		v.addResult(w, "FAIL", "ADCS-ESC9",
			fmt.Sprintf("StrongCertificateBindingEnforcement=%d on %s refuses a certificate with no SID security extension, which is exactly what CT_FLAG_NO_SECURITY_EXTENSION produces (ESC9 NOT exploitable)", b.Value, b.DCLabel), "")
	default:
		v.addResult(w, "PASS", "ADCS-ESC9",
			fmt.Sprintf("StrongCertificateBindingEnforcement=%d on %s allows the weak UPN mapping an ESC9 certificate needs (ESC9 exploitable)", b.Value, b.DCLabel), "")
	}
}

// esc13IssuanceName is the DisplayName esc13.ps1 gives the issuance policy OID
// object it creates under the forest OID container.
const esc13IssuanceName = "IssuancePolicyESC13"

// checkADCSESC13 verifies the ESC13 template's msPKI-Certificate-Policy is
// populated (the esc13.ps1 script writes the issuance policy OID into it) and
// that the issuance policy OID carries an msDS-OIDToGroupLink. The policy
// attribute alone is not enough: a template can reference an OID that links to
// no group, which leaves ESC13 unexploitable while still looking configured.
func (v *Validator) checkADCSESC13(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC13 - Issuance Policy Link")

	hosts := v.lab.HostsWithVuln("adcs_esc13")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "ADCS-ESC13", "No ESC13 vulns configured", "")
		return
	}

	for _, role := range hosts {
		// Templates live in AD — query a DC.
		queryHost := strings.ToUpper(role)
		if hc := v.lab.HostConfigs[role]; hc.Type != "dc" {
			if dcRole := v.lab.DCForDomain(hc.Domain); dcRole != "" {
				queryHost = strings.ToUpper(dcRole)
			}
		}
		if !v.hasHost(queryHost) {
			continue
		}
		output := v.adcsTemplateAttr(ctx, queryHost, "ESC13", "msPKI-Certificate-Policy")
		switch {
		case strings.Contains(output, "TEMPLATE_NOT_FOUND"):
			v.addResult(w, "FAIL", "ADCS-ESC13",
				fmt.Sprintf("ESC13 template missing on %s", queryHost), "")
		case strings.TrimSpace(output) == "":
			v.addResult(w, "FAIL", "ADCS-ESC13",
				fmt.Sprintf("ESC13 msPKI-Certificate-Policy unset on %s", queryHost), "")
		default:
			v.addResult(w, "PASS", "ADCS-ESC13",
				fmt.Sprintf("ESC13 issuance policy set on %s: %s", queryHost, strings.TrimSpace(output)), "")
		}
		v.checkESC13GroupLink(ctx, w, queryHost)
	}
}

// checkESC13GroupLink verifies the issuance policy OID object is linked to a
// group via msDS-OIDToGroupLink, and that exactly one such OID object exists.
func (v *Validator) checkESC13GroupLink(ctx context.Context, w io.Writer, dc string) {
	output, err := runScriptTextErr(ctx, v, dc,
		`$base = "CN=OID,CN=Public Key Services,CN=Services," + (Get-ADRootDSE).configurationNamingContext; `+
			`$oids = @(Get-ADObject -SearchBase $base -Filter {DisplayName -eq {{psq .Name}}} `+
			`-Properties DisplayName,'msDS-OIDToGroupLink' -ErrorAction SilentlyContinue); `+
			`if ($oids.Count -eq 0) { Write-Output 'NO_OID'; exit }; `+
			`$linked = @($oids | Where-Object { $_.'msDS-OIDToGroupLink' }); `+
			`Write-Output ("COUNT=" + $oids.Count + " LINKED=" + $linked.Count + " GROUP=" + $linked[0].'msDS-OIDToGroupLink')`,
		map[string]any{"Name": esc13IssuanceName})
	count, linked, parsed := parseESC13GroupLink(output)
	switch {
	case err != nil:
		v.addResult(w, "WARN", "ADCS-ESC13",
			fmt.Sprintf("Could not query %s OID on %s: %v", esc13IssuanceName, dc, err), "")
	case strings.Contains(output, "NO_OID"):
		v.addResult(w, "FAIL", "ADCS-ESC13",
			fmt.Sprintf("No %s OID object on %s (esc13.ps1 has not run)", esc13IssuanceName, dc), "")
	case !parsed:
		v.addResult(w, "WARN", "ADCS-ESC13",
			fmt.Sprintf("Unreadable %s OID probe output on %s: %q", esc13IssuanceName, dc, strings.TrimSpace(output)), "")
	case linked == 0:
		v.addResult(w, "FAIL", "ADCS-ESC13",
			fmt.Sprintf("%s OID on %s has no msDS-OIDToGroupLink (ESC13 is not exploitable)", esc13IssuanceName, dc), "")
	case count != 1:
		v.addResult(w, "WARN", "ADCS-ESC13",
			fmt.Sprintf("Duplicate %s OID objects on %s: %s", esc13IssuanceName, dc, strings.TrimSpace(output)), "")
	default:
		v.addResult(w, "PASS", "ADCS-ESC13",
			fmt.Sprintf("ESC13 OID linked on %s: %s", dc, strings.TrimSpace(output)), "")
	}
}

// parseESC13GroupLink pulls COUNT and LINKED out of the probe's key=value line.
// It splits fields rather than substring-matching because "COUNT=10" contains
// "COUNT=1": a lab that accumulated ten stale OID objects would otherwise read
// as the healthy single-object case. Trailing GROUP= is a DN that may itself
// contain spaces and "=", so only the two integer keys are trusted.
func parseESC13GroupLink(output string) (count, linked int, ok bool) {
	var haveCount, haveLinked bool
	for _, field := range strings.Fields(output) {
		key, val, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			continue
		}
		switch key {
		case "COUNT":
			count, haveCount = n, true
		case "LINKED":
			linked, haveLinked = n, true
		}
	}
	return count, linked, haveCount && haveLinked
}

// ---- Section 16: DNS / Audit ----

// checkDNSConditionalForwarder verifies a DNS forwarder zone exists for each
// peer/parent domain on every DC (configured by parent_child_dns or
// dc_dns_conditional_forwarder roles).
func (v *Validator) checkDNSConditionalForwarder(ctx context.Context, w io.Writer) {
	printHeader(w, "DNS Conditional Forwarders")

	domains := v.lab.Domains()
	if len(domains) < 2 {
		v.addResult(w, "SKIP", "DNS", "Single-domain lab — no conditional forwarders expected", "")
		return
	}

	for _, srcDomain := range domains {
		dcRole := v.lab.DCForDomain(srcDomain)
		if dcRole == "" {
			continue
		}
		dc := strings.ToUpper(dcRole)
		if !v.hasHost(dc) {
			continue
		}
		for _, peer := range domains {
			if peer == srcDomain {
				continue
			}
			// Skip parent/child relationships because those use delegation,
			// not forwarder zones — but check forwarders for sibling/peer.
			if strings.HasSuffix(peer, "."+srcDomain) || strings.HasSuffix(srcDomain, "."+peer) {
				continue
			}
			output, err := runScriptTextErr(ctx, v, dc,
				`$z = Get-DnsServerZone -Name {{psq .Peer}} -ErrorAction SilentlyContinue; `+
					`if ($z -and $z.ZoneType -eq 'Forwarder') { 'FORWARDER' } `+
					`elseif ($z) { 'WRONG_TYPE' } else { 'NOT_FOUND' }`,
				map[string]any{"Peer": peer})
			switch {
			case strings.Contains(output, "FORWARDER"):
				v.addResult(w, "PASS", "DNS",
					fmt.Sprintf("Forwarder for %s configured on %s", peer, dc), "")
			case strings.Contains(output, "WRONG_TYPE"):
				v.addResult(w, "WARN", "DNS",
					fmt.Sprintf("Zone for %s on %s is not a Forwarder", peer, dc), "")
			case strings.Contains(output, "NOT_FOUND"):
				v.addResult(w, "FAIL", "DNS",
					fmt.Sprintf("No forwarder for %s on %s", peer, dc), "")
			case err != nil:
				v.addResult(w, "WARN", "DNS",
					fmt.Sprintf("Could not query forwarder %s on %s: %v", peer, dc, err), "")
			default:
				// Script always emits one of the three keywords; if we
				// landed here with no transport error, something else
				// printed to stdout (PS warning, banner, locale-mangled
				// output). Surface it so the cause is diagnosable.
				v.addResult(w, "WARN", "DNS",
					fmt.Sprintf("Unexpected DNS probe output on %s for %s: %q", dc, peer, output), "")
			}
		}
	}
}

// checkDCSACLAudit verifies Directory Service Access auditing is enabled on
// DCs running the dc_audit_sacl role.
func (v *Validator) checkDCSACLAudit(ctx context.Context, w io.Writer) {
	printHeader(w, "DC SACL / Directory Service Auditing")

	dcs := v.dcsWithAuditRole()
	if len(dcs) == 0 {
		v.addResult(w, "SKIP", "Audit", "No DCs with audit_sacl/audit_policy configured", "")
		return
	}

	for _, role := range dcs {
		dc := strings.ToUpper(role)
		if !v.hasHost(dc) {
			continue
		}
		output, err := v.runPSErr(ctx, dc,
			`auditpol /get /category:"DS Access" /r 2>&1 | Out-String`)
		if err != nil {
			v.addResult(w, "WARN", "Audit",
				fmt.Sprintf("Could not query auditpol on %s: %v", dc, err), "")
			continue
		}
		v.reportDSAccessAudit(w, dc, output)
	}
}

// dcsWithAuditRole returns DC roles whose config hints at the audit_sacl /
// audit_policy role being applied (script or security tag).
func (v *Validator) dcsWithAuditRole() []string {
	var dcs []string
	for role, hc := range v.lab.HostConfigs {
		if hc.Type != "dc" {
			continue
		}
		if hostHasTag(hc.Scripts, "audit_sacl") || hostHasTag(hc.Security, "audit_policy") {
			dcs = append(dcs, role)
		}
	}
	return dcs
}

func (v *Validator) reportDSAccessAudit(w io.Writer, dc, output string) {
	lower := strings.ToLower(output)
	dsEnabled := strings.Contains(lower, "success and failure") ||
		(strings.Contains(lower, "success") && strings.Contains(lower, "directory service"))
	switch {
	case dsEnabled:
		v.addResult(w, "PASS", "Audit",
			fmt.Sprintf("DS Access auditing enabled on %s", dc), "")
	case strings.Contains(lower, "no auditing"):
		v.addResult(w, "FAIL", "Audit",
			fmt.Sprintf("DS Access auditing NOT enabled on %s", dc), "")
	case strings.TrimSpace(output) == "":
		v.addResult(w, "WARN", "Audit",
			fmt.Sprintf("Could not read auditpol on %s", dc), "")
	default:
		v.addResult(w, "INFO", "Audit",
			fmt.Sprintf("DS Access auditpol on %s: %s", dc, strings.TrimSpace(output)), "")
	}
}

func hostHasTag(values []string, needle string) bool {
	for _, s := range values {
		if strings.Contains(strings.ToLower(s), needle) {
			return true
		}
	}
	return false
}

// checkLDAPDiagnosticLogging verifies the NTDS field-engineering registry
// value is non-zero (set by the ldap_diagnostic_logging role for 1644 events).
func (v *Validator) checkLDAPDiagnosticLogging(ctx context.Context, w io.Writer) {
	printHeader(w, "LDAP Diagnostic Logging")

	dcs := v.dcRoles()
	if len(dcs) == 0 {
		v.addResult(w, "SKIP", "Audit", "No DCs configured", "")
		return
	}

	enabled := false
	for _, role := range dcs {
		dc := strings.ToUpper(role)
		if !v.hasHost(dc) {
			continue
		}
		r, err := runScriptJSON[registryDWORDResult](ctx, v, dc, scriptRegistryDWORD,
			map[string]any{
				"Path": `HKLM:\SYSTEM\CurrentControlSet\Services\NTDS\Diagnostics`,
				"Name": "15 Field Engineering",
			})
		switch {
		case err != nil:
			v.addResult(w, "WARN", "Audit",
				fmt.Sprintf("Could not query LDAP Field Engineering on %s: %v", dc, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "Audit",
				fmt.Sprintf("LDAP Field Engineering query error on %s: %s", dc, r.Error), "")
		case !r.Present:
			v.addResult(w, "FAIL", "Audit",
				fmt.Sprintf("LDAP Field Engineering not set on %s (1644 events disabled)", dc), "")
		case r.Value == 0:
			v.addResult(w, "FAIL", "Audit",
				fmt.Sprintf("LDAP Field Engineering=0 on %s (1644 events disabled)", dc), "")
		default:
			v.addResult(w, "PASS", "Audit",
				fmt.Sprintf("LDAP Field Engineering=%d on %s (1644 events enabled)", r.Value, dc), "")
			enabled = true
		}
	}
	if !enabled && len(dcs) > 0 {
		v.addResult(w, "INFO", "Audit", "No DC has LDAP diagnostic logging enabled", "")
	}
}

func (v *Validator) dcRoles() []string {
	var dcs []string
	for role, hc := range v.lab.HostConfigs {
		if hc.Type == "dc" {
			dcs = append(dcs, role)
		}
	}
	return dcs
}

//go:embed scripts/asr_rules.ps1
var scriptASRRules string

type asrRulesResult struct {
	RuleCount int    `json:"rule_count"`
	Error     string `json:"error"`
}

// checkASRRules verifies Defender ASR rules are configured on hosts running
// the security_asr role.
func (v *Validator) checkASRRules(ctx context.Context, w io.Writer) {
	printHeader(w, "Defender ASR Rules")

	var hosts []string
	for role, hc := range v.lab.HostConfigs {
		for _, s := range hc.Security {
			if strings.Contains(strings.ToLower(s), "asr") {
				hosts = append(hosts, role)
				break
			}
		}
	}
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "Defender", "No ASR security tags configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[asrRulesResult](ctx, v, host, scriptASRRules, nil)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "Defender",
				fmt.Sprintf("Could not read ASR rules on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "Defender",
				fmt.Sprintf("ASR rules query error on %s: %s", hostLabel, r.Error), "")
		case r.RuleCount == 0:
			v.addResult(w, "FAIL", "Defender",
				fmt.Sprintf("No ASR rules configured on %s", hostLabel), "")
		default:
			v.addResult(w, "PASS", "Defender",
				fmt.Sprintf("ASR rules configured on %s: %d rule(s)", hostLabel, r.RuleCount), "")
		}
	}
}

// ---- Section 10: IIS upload ----

//go:embed scripts/iis_upload_acl.ps1
var scriptIISUploadACL string

type iisUploadACLResult struct {
	DirPresent bool   `json:"dir_present"`
	ACEPresent bool   `json:"ace_present"`
	Rights     string `json:"rights"`
	Error      string `json:"error"`
}

// checkIISUploadPermissions verifies the IIS upload directory is writable by
// IIS_IUSRS (set by the vulns_permissions role on hosts running IIS).
func (v *Validator) checkIISUploadPermissions(ctx context.Context, w io.Writer) {
	printHeader(w, "IIS Upload Folder Permissions")

	hosts := v.lab.HostsWithVuln("permissions")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "IIS", "No permissions vulns configured", "")
		return
	}

	found := false
	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		r, err := runScriptJSON[iisUploadACLResult](ctx, v, host, scriptIISUploadACL, nil)
		switch {
		case err != nil:
			v.addResult(w, "WARN", "IIS",
				fmt.Sprintf("Could not read upload ACL on %s: %v", hostLabel, err), "")
		case r.Error != "":
			v.addResult(w, "WARN", "IIS",
				fmt.Sprintf("Upload ACL query error on %s: %s", hostLabel, r.Error), "")
		case !r.DirPresent:
			v.addResult(w, "FAIL", "IIS",
				fmt.Sprintf("No upload directory on %s (IIS not configured)", hostLabel), "")
		case !r.ACEPresent:
			v.addResult(w, "FAIL", "IIS",
				fmt.Sprintf("Upload dir on %s has no IIS_IUSRS write ACE", hostLabel), "")
		default:
			found = true
			v.addResult(w, "PASS", "IIS",
				fmt.Sprintf("IIS_IUSRS has %s on upload dir on %s", r.Rights, hostLabel), "")
		}
	}
	if !found {
		// Not a failure — IIS is optional in some labs.
		v.addResult(w, "INFO", "IIS", "No IIS_IUSRS upload permissions found", "")
	}
}

// ---- Section 2: Configured Users ----

// checkConfiguredUsers verifies each user defined in DomainConfigs.Users
// actually exists in AD on the user's domain DC, and that every configured
// group membership is present in MemberOf. Emits one PASS per user and one
// FAIL per missing user or unsatisfied group expectation.
func (v *Validator) checkConfiguredUsers(ctx context.Context, w io.Writer) {
	printHeader(w, "Configured AD Users")

	users := v.lab.AllConfiguredUsers()
	if len(users) == 0 {
		v.addResult(w, "SKIP", "Users", "No users configured", "")
		return
	}

	for _, uf := range users {
		dcRole := strings.ToUpper(uf.DCRole)
		if !v.hasHost(dcRole) {
			continue
		}
		var output string
		var probeErr error
		outcome := v.retryMustExist(ctx, func() probeOutcome {
			output, probeErr = runScriptText(ctx, v, dcRole, scriptADUserWithGroups,
				map[string]any{"Username": uf.Username})
			switch {
			case probeErr != nil:
				return probeIncomplete
			case strings.Contains(output, "USER_FOUND"):
				return probePositive
			case strings.Contains(output, "USER_NOT_FOUND"):
				return probeNegative
			default:
				return probeIncomplete
			}
		})
		switch outcome {
		case probeIncomplete:
			v.addResult(w, "WARN", "Users",
				fmt.Sprintf("Could not verify %s in %s: %v", uf.Username, uf.Domain, probeErr), "")
			continue
		case probeNegative:
			v.addResult(w, "FAIL", "Users",
				fmt.Sprintf("%s does NOT exist in %s", uf.Username, uf.Domain), "")
			continue
		}

		// Collect group names from MemberOf DN strings (CN=Group,...).
		memberOf := make(map[string]bool)
		for _, line := range parseOutputLines(output) {
			if !strings.HasPrefix(line, "GROUP=") {
				continue
			}
			dn := strings.TrimPrefix(line, "GROUP=")
			// Take CN= portion of first RDN.
			parts := strings.SplitN(dn, ",", 2)
			cn := strings.TrimPrefix(parts[0], "CN=")
			memberOf[strings.ToLower(cn)] = true
		}

		expected := uf.User.Groups
		matched := 0
		var missing []string
		for _, g := range expected {
			if memberOf[strings.ToLower(g)] {
				matched++
			} else {
				missing = append(missing, g)
			}
		}

		if matched == len(expected) {
			v.addResult(w, "PASS", "Users",
				fmt.Sprintf("%s exists with %d/%d expected groups", uf.Username, matched, len(expected)), "")
		} else {
			v.addResult(w, "FAIL", "Users",
				fmt.Sprintf("%s missing groups in %s: %s", uf.Username, uf.Domain, strings.Join(missing, ", ")), "")
		}
	}
}

// ---- Section 3: Configured Groups ----

// checkConfiguredGroups verifies that every group referenced by any user's
// Groups list actually exists in AD on the corresponding domain DC. The set
// is deduplicated per (domain, group).
func (v *Validator) checkConfiguredGroups(ctx context.Context, w io.Writer) {
	printHeader(w, "Configured AD Groups")

	groups := v.lab.AllConfiguredGroups()
	if len(groups) == 0 {
		v.addResult(w, "SKIP", "Groups", "No groups referenced by users", "")
		return
	}

	for _, gf := range groups {
		dcRole := strings.ToUpper(gf.DCRole)
		if !v.hasHost(dcRole) {
			continue
		}
		output, err := runScriptText(ctx, v, dcRole,
			`$g = Get-ADGroup -Identity {{psq .Group}} -ErrorAction SilentlyContinue; `+
				`if ($g) { 'GROUP_FOUND' } else { 'GROUP_NOT_FOUND' }`,
			map[string]any{"Group": gf.Group})
		switch {
		case err != nil:
			v.addResult(w, "WARN", "Groups",
				fmt.Sprintf("Could not verify group '%s' in %s: %v", gf.Group, gf.Domain, err), "")
		case strings.Contains(output, "GROUP_FOUND"):
			v.addResult(w, "PASS", "Groups",
				fmt.Sprintf("Group '%s' exists in %s", gf.Group, gf.Domain), "")
		case strings.Contains(output, "GROUP_NOT_FOUND"):
			v.addResult(w, "FAIL", "Groups",
				fmt.Sprintf("Group '%s' NOT found in %s", gf.Group, gf.Domain), "")
		default:
			v.addResult(w, "WARN", "Groups",
				fmt.Sprintf("Could not verify group '%s' in %s", gf.Group, gf.Domain), "")
		}
	}
}

type crossDomainMembershipProbeResult struct {
	okCount    int
	missing    []string
	unresolved []string
}

func parseCrossDomainMembershipOutput(output string) crossDomainMembershipProbeResult {
	var result crossDomainMembershipProbeResult
	for _, line := range parseOutputLines(output) {
		switch {
		case strings.HasPrefix(line, "MEMBER_OK="):
			result.okCount++
		case strings.HasPrefix(line, "MEMBER_MISSING="):
			result.missing = append(result.missing, strings.TrimPrefix(line, "MEMBER_MISSING="))
		case strings.HasPrefix(line, "MEMBER_LOOKUP_ERROR="):
			result.unresolved = append(result.unresolved, strings.TrimPrefix(line, "MEMBER_LOOKUP_ERROR="))
		case strings.HasPrefix(line, "MEMBER_INVALID="):
			result.unresolved = append(result.unresolved, strings.TrimPrefix(line, "MEMBER_INVALID="))
		}
	}
	return result
}

func (v *Validator) reportCrossDomainMembershipResult(
	w io.Writer,
	gf labmap.CrossDomainGroupFact,
	result crossDomainMembershipProbeResult,
) {
	switch {
	case len(result.missing) > 0:
		message := fmt.Sprintf("Group '%s' missing configured members in %s: %s", gf.Group, gf.Domain, strings.Join(result.missing, ", "))
		if len(result.unresolved) > 0 {
			message += fmt.Sprintf("; could not resolve: %s", strings.Join(result.unresolved, ", "))
		}
		v.addResult(w, "FAIL", "Groups",
			message, "")
	case len(result.unresolved) > 0:
		v.addResult(w, "WARN", "Groups",
			fmt.Sprintf("Could not resolve configured members of '%s' in %s: %s", gf.Group, gf.Domain, strings.Join(result.unresolved, ", ")), "")
	case result.okCount != len(gf.Members):
		v.addResult(w, "WARN", "Groups",
			fmt.Sprintf("Could not confirm all configured members of '%s' in %s: got %d/%d proofs", gf.Group, gf.Domain, result.okCount, len(gf.Members)), "")
	default:
		v.addResult(w, "PASS", "Groups",
			fmt.Sprintf("Group '%s' has %d/%d configured cross-domain members in %s", gf.Group, result.okCount, len(gf.Members), gf.Domain), "")
	}
}

// checkCrossDomainGroupMemberships verifies every configured
// multi_domain_groups_member relation. Foreign members are represented in the
// local domain as ForeignSecurityPrincipal objects, so the probe resolves each
// configured principal's SID in its source domain and accepts either the
// principal DN or the local FSP DN as proof.
func (v *Validator) checkCrossDomainGroupMemberships(ctx context.Context, w io.Writer) {
	printHeader(w, "Configured Cross-Domain Group Memberships")

	facts := v.lab.CrossDomainGroupMemberships()
	if len(facts) == 0 {
		v.addResult(w, "SKIP", "Groups", "No cross-domain group memberships configured", "")
		return
	}

	adminUser := v.lab.AdminUser
	if adminUser == "" {
		adminUser = "administrator"
	}

	for _, gf := range facts {
		dcRole := strings.ToUpper(gf.DCRole)
		if !v.hasHost(dcRole) {
			continue
		}
		domainConfig, ok := v.lab.DomainConfigs[gf.Domain]
		if !ok {
			v.addResult(w, "WARN", "Groups",
				fmt.Sprintf("Could not verify cross-domain members of '%s': domain %s is not configured", gf.Group, gf.Domain), "")
			continue
		}
		domainUsernamePrefix := domainConfig.NetBIOSName
		if domainUsernamePrefix == "" {
			domainUsernamePrefix = gf.Domain
		}

		output, err := runScriptText(ctx, v, dcRole,
			`$ErrorActionPreference = 'Stop'; `+
				`$localDomain = {{psq .Domain}}; `+
				`$domainUsername = {{psq .DomainUsername}}; `+
				`$password = ConvertTo-SecureString {{psq .DomainPassword}} -AsPlainText -Force; `+
				`$credential = New-Object System.Management.Automation.PSCredential($domainUsername, $password); `+
				`$group = Get-ADGroup -Identity {{psq .Group}} -Properties Members -Server $localDomain -Credential $credential -ErrorAction Stop; `+
				`$localBase = (Get-ADDomain -Server $localDomain -Credential $credential -ErrorAction Stop).DistinguishedName; `+
				`foreach ($member in {{psarr .Members}}) { `+
				`  $parts = $member -split '\\', 2; `+
				`  if ($parts.Count -ne 2) { Write-Output "MEMBER_INVALID=$member"; continue }; `+
				`  $memberDomain = $parts[0]; $memberName = $parts[1]; $adObject = $null; `+
				`  try { $adObject = Get-ADUser -Identity $memberName -Server $memberDomain -Credential $credential -ErrorAction Stop } catch { `+
				`    try { $adObject = Get-ADGroup -Identity $memberName -Server $memberDomain -Credential $credential -ErrorAction Stop } catch { `+
				`      Write-Output "MEMBER_LOOKUP_ERROR=$member"; continue `+
				`    } `+
				`  }; `+
				`  $expectedDNs = @($adObject.DistinguishedName, "CN=$($adObject.SID.Value),CN=ForeignSecurityPrincipals,$localBase"); `+
				`  if (@($group.Members | Where-Object { $expectedDNs -contains $_ }).Count -gt 0) { `+
				`    Write-Output "MEMBER_OK=$member" `+
				`  } else { `+
				`    Write-Output "MEMBER_MISSING=$member" `+
				`  } `+
				`}`,
			map[string]any{
				"Domain":         gf.Domain,
				"DomainUsername": domainUsernamePrefix + `\` + adminUser,
				"DomainPassword": domainConfig.DomainPassword,
				"Group":          gf.Group,
				"Members":        gf.Members,
			})
		if err != nil {
			v.addResult(w, "WARN", "Groups",
				fmt.Sprintf("Could not verify cross-domain members of '%s' in %s: %v", gf.Group, gf.Domain, err), "")
			continue
		}

		result := parseCrossDomainMembershipOutput(output)
		v.reportCrossDomainMembershipResult(w, gf, result)
	}
}

// ---- Section 11: Local Admin Access Map ----

// localAdminsQuery is `net localgroup Administrators` parsed down to its
// member rows. Unlike Get-LocalGroupMember, `net localgroup` works
// identically on member servers and DCs (where Administrators is the
// BUILTIN\Administrators domain-local group). Output rows are DOMAIN\user
// (or bare account name for local accounts), which matches the
// labmap-configured "domain\\user" format.
const localAdminsQuery = `$lines = (net localgroup Administrators 2>&1 | Out-String) -split "` + "`" + `r?` + "`" + `n"; ` +
	`$inMembers = $false; ` +
	`foreach ($l in $lines) { ` +
	`  if ($l -match '^-+$') { $inMembers = $true; continue } ` +
	`  if (-not $inMembers) { continue } ` +
	`  $t = $l.Trim(); ` +
	`  if ($t -eq '' -or $t -like 'The command completed*') { continue } ` +
	`  $t ` +
	`}`

// checkLocalAdmins verifies, for each Windows host, that the configured
// local_groups.Administrators set matches the actual Administrators group
// membership. Uses `net localgroup` rather than Get-LocalGroupMember
// because the latter silently returns nothing for BUILTIN\Administrators
// on domain controllers.
func (v *Validator) checkLocalAdmins(ctx context.Context, w io.Writer) {
	printHeader(w, "Local Admin Access Map")

	hosts := v.lab.WindowsHosts()
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "LocalAdmins", "No Windows hosts configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))

		output, err := v.runPSErr(ctx, host, localAdminsQuery)
		if err != nil {
			v.addResult(w, "WARN", "LocalAdmins",
				fmt.Sprintf("Could not enumerate local admins on %s: %v", hostLabel, err), "")
			continue
		}
		actual := parseOutputLines(output)

		expected := v.lab.LocalAdminsForHost(role)
		if len(expected) == 0 {
			if len(actual) > 0 {
				v.addResult(w, "INFO", "LocalAdmins",
					fmt.Sprintf("Local admins on %s: %s", hostLabel, strings.Join(actual, ", ")), "")
			} else {
				v.addResult(w, "WARN", "LocalAdmins",
					fmt.Sprintf("Could not enumerate local admins on %s", hostLabel), "")
			}
			continue
		}

		// Build a set of actual names. On member servers `net localgroup`
		// emits "DOMAIN\user"; on DCs same-domain accounts are listed
		// bare ("robert.baratheon"). Index both the full name and the
		// post-backslash leaf so a "domain\\user" expectation matches
		// either form.
		actualSet := make(map[string]bool, len(actual)*2)
		for _, a := range actual {
			t := strings.ToLower(strings.TrimSpace(a))
			actualSet[t] = true
			if i := strings.Index(t, `\`); i >= 0 {
				actualSet[t[i+1:]] = true
			}
		}

		var missing []string
		for _, exp := range expected {
			t := strings.ToLower(strings.TrimSpace(exp))
			leaf := t
			if i := strings.Index(t, `\`); i >= 0 {
				leaf = t[i+1:]
			}
			if !actualSet[t] && !actualSet[leaf] {
				missing = append(missing, exp)
			}
		}
		if len(missing) == 0 {
			v.addResult(w, "PASS", "LocalAdmins",
				fmt.Sprintf("%s has all %d configured local admins", hostLabel, len(expected)), "")
		} else {
			v.addResult(w, "FAIL", "LocalAdmins",
				fmt.Sprintf("%s missing local admins: %s", hostLabel, strings.Join(missing, ", ")), "")
		}
	}
}

// ---- Section 13: CVE Patch Status ----

// cvePatch maps a CVE identifier to its mitigating KB(s). A missing KB is a
// PASS (lab is intentionally vulnerable); an installed KB is INFO (patch
// applied, exploit may fail).
type cvePatch struct {
	CVE  string
	Name string
	KBs  []string
}

//go:embed scripts/cve_patch.ps1
var scriptCVEPatch string

type cvePatchResult struct {
	Installed string `json:"installed"`
	Error     string `json:"error"`
}

// checkCVEPatches reports patch status for each (Windows host, CVE) by
// querying Get-HotFix per KB. Lab hosts are intentionally unpatched, so a
// missing KB indicates the vulnerability is still exploitable.
func (v *Validator) checkCVEPatches(ctx context.Context, w io.Writer) {
	printHeader(w, "CVE Patch Status")

	hosts := v.lab.WindowsHosts()
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "CVE", "No Windows hosts configured", "")
		return
	}

	patches := []cvePatch{
		{CVE: "CVE-2020-1472", Name: "ZeroLogon", KBs: []string{"KB4565351"}},
		{CVE: "CVE-2021-34527", Name: "PrintNightmare", KBs: []string{"KB5005010", "KB5005033", "KB5005565"}},
		{CVE: "CVE-2021-42278", Name: "noPac", KBs: []string{"KB5008380"}},
		{CVE: "CVE-2022-26923", Name: "Certifried", KBs: []string{"KB5014754"}},
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))

		for _, p := range patches {
			r, err := runScriptJSON[cvePatchResult](ctx, v, host, scriptCVEPatch,
				map[string]any{"KBs": p.KBs})
			switch {
			case err != nil:
				v.addResult(w, "WARN", "CVE",
					fmt.Sprintf("Could not query %s status on %s: %v", p.Name, hostLabel, err), "")
			case r.Error != "":
				v.addResult(w, "WARN", "CVE",
					fmt.Sprintf("CVE check error on %s: %s", hostLabel, r.Error), "")
			case r.Installed == "":
				v.addResult(w, "PASS", "CVE",
					fmt.Sprintf("%s on %s: unpatched (%s)", p.Name, hostLabel, p.CVE), "")
			default:
				v.addResult(w, "INFO", "CVE",
					fmt.Sprintf("%s on %s: patched (%s, %s)", p.Name, hostLabel, r.Installed, p.CVE), "")
			}
		}
	}
}

// ---- Section 14: Admin Shares ----

// checkAdminShares verifies the default ADMIN$ and C$ shares are present on
// each Windows host. Both shares are required for admin-creds lateral
// movement (psexec, smbexec, wmiexec).
func (v *Validator) checkAdminShares(ctx context.Context, w io.Writer) {
	printHeader(w, "Default Admin Shares")

	hosts := v.lab.WindowsHosts()
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "AdminShares", "No Windows hosts configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))

		var missing []string
		outcome := v.retryMustExist(ctx, func() probeOutcome {
			output, err := runScriptText(ctx, v, host, scriptAdminShares, nil)
			if err != nil {
				return probeIncomplete
			}
			found := make(map[string]bool)
			for _, s := range parseOutputLines(output) {
				found[strings.ToUpper(strings.TrimSpace(s))] = true
			}
			missing = nil
			for _, want := range []string{"ADMIN$", "C$"} {
				if !found[want] {
					missing = append(missing, want)
				}
			}
			if len(missing) == 0 {
				return probePositive
			}
			return probeNegative
		})
		switch outcome {
		case probePositive:
			v.addResult(w, "PASS", "AdminShares",
				fmt.Sprintf("%s exposes ADMIN$ and C$", hostLabel), "")
		case probeNegative:
			v.addResult(w, "FAIL", "AdminShares",
				fmt.Sprintf("%s missing default shares: %s", hostLabel, strings.Join(missing, ", ")), "")
		default:
			v.addResult(w, "WARN", "AdminShares",
				fmt.Sprintf("Could not enumerate shares on %s (host settling?)", hostLabel), "")
		}
	}
}

// parseOutputLines splits PowerShell command output into non-empty trimmed
// lines, discarding blank lines and leading/trailing whitespace.
func parseOutputLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
