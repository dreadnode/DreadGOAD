// Package validate provides vulnerability validation checks for GOAD lab
// instances. It runs PowerShell commands against Windows hosts via AWS SSM
// and records pass/fail/warn results in a structured [Report].
package validate

import (
	"context"
	"fmt"
	"io"
	"strings"
)

func printHeader(w io.Writer, header string) {
	_, _ = fmt.Fprintf(w, "\n== %s ==\n", header)
}

func (v *Validator) checkCredentialDiscovery(ctx context.Context, w io.Writer) {
	printHeader(w, "Credential Discovery Vulnerabilities")

	users := v.lab.UsersWithPasswordInDescription()
	if len(users) == 0 {
		v.addResult(w, "SKIP", "Credentials", "No users with password-in-description configured", "")
		return
	}

	for _, uf := range users {
		dcRole := strings.ToUpper(uf.DCRole)
		output := v.runPS(ctx, dcRole, fmt.Sprintf(
			`Get-ADUser -Identity '%s' -Properties Description | Select-Object -ExpandProperty Description`,
			uf.Username))
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
		output := v.runPS(ctx, dcRole,
			`Get-ADUser -Filter {DoesNotRequirePreAuth -eq $true} -Properties DoesNotRequirePreAuth | Select-Object -ExpandProperty SamAccountName`)
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
		output := v.runPS(ctx, dcRole, fmt.Sprintf(
			`Get-ADUser -Identity '%s' -Properties ServicePrincipalName | Select-Object -ExpandProperty ServicePrincipalName`,
			uf.Username))
		if strings.TrimSpace(output) != "" {
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
		output := v.runPS(ctx, host,
			`Get-SmbServerConfiguration | Select-Object RequireSecuritySignature,EnableSecuritySignature | Format-Table -AutoSize | Out-String`)
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

	for _, role := range v.lab.DCs() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))

		output := v.runPS(ctx, host,
			`Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Lsa' -Name RestrictAnonymous -ErrorAction SilentlyContinue | Select-Object -ExpandProperty RestrictAnonymous`)
		val := strings.TrimSpace(output)
		if val == "0" {
			v.addResult(w, "PASS", "SMB", fmt.Sprintf("RestrictAnonymous is 0 on %s (NULL sessions enabled)", hostLabel), "")
		} else {
			v.addResult(w, "INFO", "SMB", fmt.Sprintf("RestrictAnonymous is %s on %s", val, hostLabel), "")
		}

		output = v.runPS(ctx, host,
			`Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Lsa' -Name RestrictAnonymousSAM -ErrorAction SilentlyContinue | Select-Object -ExpandProperty RestrictAnonymousSAM`)
		val = strings.TrimSpace(output)
		if val == "0" {
			v.addResult(w, "PASS", "SMB", fmt.Sprintf("RestrictAnonymousSAM is 0 on %s (SAM enum enabled)", hostLabel), "")
		} else {
			v.addResult(w, "INFO", "SMB", fmt.Sprintf("RestrictAnonymousSAM is %s on %s", val, hostLabel), "")
		}
	}

	for _, role := range v.lab.WindowsServers() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output := v.runPS(ctx, host,
			`Get-LocalUser -Name Guest | Select-Object Name,Enabled | Format-Table -AutoSize | Out-String`)
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
		output := v.runPS(ctx, host,
			`$v = Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Lsa' -Name LmCompatibilityLevel -ErrorAction SilentlyContinue; if ($v) { $v.LmCompatibilityLevel } else { 'NOT_SET' }`)
		val := strings.TrimSpace(output)
		switch val {
		case "0", "1", "2":
			v.addResult(w, "PASS", "SMB", fmt.Sprintf("LmCompatibilityLevel is %s on %s (NTLM downgrade vulnerable)", val, hostLabel), "")
		case "", "NOT_SET":
			v.addResult(w, "WARN", "SMB", fmt.Sprintf("LmCompatibilityLevel not configured on %s (registry key missing)", hostLabel), "")
		default:
			v.addResult(w, "FAIL", "SMB", fmt.Sprintf("LmCompatibilityLevel is %s on %s (expected 0-2)", val, hostLabel), "")
		}
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
		output := v.runPS(ctx, host,
			`Get-ADObject -Identity ((Get-ADDomain).distinguishedname) -Properties ms-DS-MachineAccountQuota | Select-Object -ExpandProperty ms-DS-MachineAccountQuota`)
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

		output := v.runPS(ctx, host,
			`Get-Service 'MSSQL$SQLEXPRESS','MSSQLSERVER' -ErrorAction SilentlyContinue | Where-Object {$_.Status -eq 'Running'} | Select-Object -ExpandProperty Name`)
		if strings.TrimSpace(output) == "" {
			v.addResult(w, "FAIL", "MSSQL", fmt.Sprintf("MSSQL NOT running on %s", hostLabel), "")
			continue
		}
		v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("MSSQL running on %s", hostLabel), "")

		sqlQuery := func(query string) string {
			return v.runPS(ctx, host, fmt.Sprintf(
				`$c = New-Object System.Data.SqlClient.SqlConnection 'Server=localhost;Integrated Security=True;TrustServerCertificate=True'; `+
					`$c.Open(); $cmd = $c.CreateCommand(); $cmd.CommandText = '%s'; `+
					`$r = $cmd.ExecuteReader(); while ($r.Read()) { Write-Output $r[0].ToString() }; $r.Close(); $c.Close()`,
				query))
		}

		for _, admin := range mf.MSSQL.SysAdmins {
			output = sqlQuery(fmt.Sprintf(
				"SELECT m.name FROM sys.server_role_members srm JOIN sys.server_principals r ON srm.role_principal_id = r.principal_id JOIN sys.server_principals m ON srm.member_principal_id = m.principal_id WHERE r.name = ''sysadmin'' AND m.name = ''%s''",
				admin))
			if strings.TrimSpace(output) != "" {
				v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("%s is sysadmin on %s", admin, hostLabel), "")
			} else {
				v.addResult(w, "FAIL", "MSSQL", fmt.Sprintf("%s is NOT sysadmin on %s", admin, hostLabel), "")
			}
		}

		for grantee, target := range mf.MSSQL.ExecuteAsLogin {
			output = sqlQuery(fmt.Sprintf(
				"SELECT pr.name FROM sys.server_permissions sp JOIN sys.server_principals pr ON sp.grantee_principal_id = pr.principal_id JOIN sys.server_principals pr2 ON sp.major_id = pr2.principal_id WHERE sp.permission_name = ''IMPERSONATE'' AND pr.name = ''%s'' AND pr2.name = ''%s''",
				grantee, target))
			if strings.TrimSpace(output) != "" {
				v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("%s can impersonate %s on %s", grantee, target, hostLabel), "")
			} else {
				v.addResult(w, "FAIL", "MSSQL", fmt.Sprintf("%s CANNOT impersonate %s on %s", grantee, target, hostLabel), "")
			}
		}

		for name, ls := range mf.MSSQL.LinkedServers {
			output = sqlQuery(fmt.Sprintf(
				"SELECT name FROM sys.servers WHERE is_linked = 1 AND name = ''%s''", name))
			if strings.TrimSpace(output) != "" {
				v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("Linked server %s -> %s on %s", name, ls.DataSrc, hostLabel), "")
			} else {
				v.addResult(w, "FAIL", "MSSQL", fmt.Sprintf("Linked server %s NOT found on %s", name, hostLabel), "")
			}
		}

		v.checkMSSQLExtendedFeatures(w, sqlQuery, hostLabel)
	}
}

func (v *Validator) checkMSSQLExtendedFeatures(w io.Writer, sqlQuery func(string) string, hostLabel string) {
	output := sqlQuery("SELECT CONVERT(INT, ISNULL(value, value_in_use)) FROM sys.configurations WHERE name = ''xp_cmdshell''")
	xpEnabled := strings.TrimSpace(output) == "1"
	if xpEnabled {
		v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("xp_cmdshell enabled on %s", hostLabel), "")
	} else {
		v.addResult(w, "FAIL", "MSSQL", fmt.Sprintf("xp_cmdshell NOT enabled on %s", hostLabel), "")
	}

	if xpEnabled {
		privOut := sqlQuery("EXEC xp_cmdshell ''whoami /priv''")
		if strings.Contains(privOut, "SeImpersonatePrivilege") {
			v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("MSSQL service has SeImpersonatePrivilege on %s (potato escalation possible)", hostLabel), "")
		} else if strings.TrimSpace(privOut) != "" {
			v.addResult(w, "INFO", "MSSQL", fmt.Sprintf("SeImpersonatePrivilege NOT found on MSSQL service on %s", hostLabel), "")
		}
	}

	trustworthy := sqlQuery("SELECT name FROM sys.databases WHERE is_trustworthy_on = 1 AND name NOT IN (''master'',''tempdb'')")
	dbs := parseOutputLines(trustworthy)
	if len(dbs) > 0 {
		v.addResult(w, "PASS", "MSSQL", fmt.Sprintf("TRUSTWORTHY databases on %s: %s", hostLabel, strings.Join(dbs, ", ")), "")
	} else {
		v.addResult(w, "INFO", "MSSQL", fmt.Sprintf("No TRUSTWORTHY databases on %s", hostLabel), "")
	}
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

		output := v.runPS(ctx, host,
			`Get-WindowsFeature ADCS-Cert-Authority | Select-Object Name,InstallState | Format-Table -AutoSize | Out-String`)
		if strings.Contains(strings.ToLower(output), "installed") {
			v.addResult(w, "PASS", "ADCS", fmt.Sprintf("ADCS installed on %s", hostLabel), "")
		} else {
			v.addResult(w, "FAIL", "ADCS", fmt.Sprintf("ADCS NOT installed on %s", hostLabel), "")
		}

		if v.lab.CAWebEnrollment() {
			output = v.runPS(ctx, host,
				`Get-WindowsFeature ADCS-Web-Enrollment | Select-Object Name,InstallState | Format-Table -AutoSize | Out-String`)
			if strings.Contains(strings.ToLower(output), "installed") {
				v.addResult(w, "PASS", "ADCS", "ADCS Web Enrollment installed (ESC8 possible)", "")
			} else {
				v.addResult(w, "WARN", "ADCS", "ADCS Web Enrollment not installed", "")
			}
		}

		// Query published templates from the domain's DC (not the ADCS member
		// server) because ADWS is only reliable on domain controllers.
		templateQueryHost := host
		if dcRole := v.lab.ADCSDCRole(role); dcRole != "" {
			templateQueryHost = strings.ToUpper(dcRole)
		}
		output = v.runPS(ctx, templateQueryHost,
			`Get-ADObject -Filter {objectClass -eq 'pKIEnrollmentService'} -SearchBase ("CN=Enrollment Services,CN=Public Key Services,CN=Services," + (Get-ADRootDSE).configurationNamingContext) -Properties certificateTemplates | Select-Object -ExpandProperty certificateTemplates`)
		if strings.TrimSpace(output) == "" {
			continue
		}
		publishedTemplates := parseOutputLines(output)
		escTemplates := []string{"ESC1", "ESC2", "ESC3", "ESC3-CRA", "ESC4", "ESC13"}
		for _, tmpl := range escTemplates {
			found := false
			for _, pub := range publishedTemplates {
				if strings.EqualFold(strings.TrimSpace(pub), tmpl) {
					found = true
					break
				}
			}
			if found {
				v.addResult(w, "PASS", "ADCS", fmt.Sprintf("Template %s published on %s CA", tmpl, hostLabel), "")
			} else {
				v.addResult(w, "FAIL", "ADCS", fmt.Sprintf("Template %s NOT published on %s CA", tmpl, hostLabel), "")
			}
		}
	}
}

func (v *Validator) checkADCSESC7(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC7 - ManageCA ACL")

	facts := v.lab.HostsWithESC7()
	if len(facts) == 0 {
		v.addResult(w, "SKIP", "ADCS-ESC7", "No ESC7 (ManageCA) vulns configured", "")
		return
	}

	for _, f := range facts {
		// The ManageCA ACL is on the CA, so query the ADCS host.
		host := strings.ToUpper(f.HostRole)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(f.Hostname)

		// Use PSPKI to check whether the ca_manager identity has ManageCa rights.
		script := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
if (-not (Get-Module -ListAvailable -Name PSPKI)) {
  Write-Output 'PSPKI_NOT_INSTALLED'
  exit
}
Import-Module -Name PSPKI
try {
  $ca = Get-CertificationAuthority
  $acl = Get-CertificationAuthority $ca.ComputerName | Get-CertificationAuthorityAcl
  $match = $acl.Access | Where-Object { $_.IdentityReference -like '*%s*' -and $_.Rights -match 'ManageCa' }
  if ($match) { Write-Output 'MANAGECA_FOUND' } else { Write-Output 'MANAGECA_NOT_FOUND' }
} catch {
  Write-Output "CHECK_ERROR: $_"
}`, f.CAManager)

		output := v.runPS(ctx, host, script)

		switch {
		case strings.Contains(output, "MANAGECA_FOUND"):
			v.addResult(w, "PASS", "ADCS-ESC7", fmt.Sprintf("%s has ManageCA on %s CA (ESC7 exploitable)", f.CAManager, hostLabel), "")
		case strings.Contains(output, "MANAGECA_NOT_FOUND"):
			v.addResult(w, "FAIL", "ADCS-ESC7", fmt.Sprintf("%s does NOT have ManageCA on %s CA", f.CAManager, hostLabel), "")
		case strings.Contains(output, "PSPKI_NOT_INSTALLED"):
			v.addResult(w, "FAIL", "ADCS-ESC7", fmt.Sprintf("PSPKI module not installed on %s", hostLabel), "")
		default:
			v.addResult(w, "WARN", "ADCS-ESC7", fmt.Sprintf("Could not verify ManageCA for %s on %s: %s", f.CAManager, hostLabel, strings.TrimSpace(output)), "")
		}
	}
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
		output := v.runPS(ctx, host,
			`certutil -getreg policy\EditFlags 2>&1`)
		if strings.Contains(output, "EDITF_ATTRIBUTESUBJECTALTNAME2") {
			v.addResult(w, "PASS", "ADCS-ESC6", fmt.Sprintf("EDITF_ATTRIBUTESUBJECTALTNAME2 set on %s (ESC6 exploitable)", hostLabel), "")
		} else {
			v.addResult(w, "FAIL", "ADCS-ESC6", fmt.Sprintf("EDITF_ATTRIBUTESUBJECTALTNAME2 NOT set on %s", hostLabel), "")
		}
	}
}

func (v *Validator) checkADCSESC10(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC10 - Weak Certificate Mapping")

	case1Hosts := v.lab.HostsWithVuln("adcs_esc10_case1")
	for _, role := range case1Hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output := v.runPS(ctx, host,
			`$v = Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\Kdc' -Name StrongCertificateBindingEnforcement -ErrorAction SilentlyContinue; if ($v) { $v.StrongCertificateBindingEnforcement } else { 'NOT_SET' }`)
		val := strings.TrimSpace(output)
		switch val {
		case "0":
			v.addResult(w, "PASS", "ADCS-ESC10", fmt.Sprintf("StrongCertificateBindingEnforcement=0 on %s (ESC10 case 1 exploitable)", hostLabel), "")
		case "", "NOT_SET":
			v.addResult(w, "WARN", "ADCS-ESC10", fmt.Sprintf("StrongCertificateBindingEnforcement not configured on %s (registry key missing)", hostLabel), "")
		default:
			v.addResult(w, "FAIL", "ADCS-ESC10", fmt.Sprintf("StrongCertificateBindingEnforcement=%s on %s (expected 0)", val, hostLabel), "")
		}
	}

	case2Hosts := v.lab.HostsWithVuln("adcs_esc10_case2")
	for _, role := range case2Hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output := v.runPS(ctx, host,
			`$v = Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\SecurityProviders\Schannel' -Name CertificateMappingMethods -ErrorAction SilentlyContinue; if ($v) { $v.CertificateMappingMethods } else { 'NOT_SET' }`)
		val := strings.TrimSpace(output)
		switch val {
		case "4":
			v.addResult(w, "PASS", "ADCS-ESC10", fmt.Sprintf("CertificateMappingMethods=0x4 on %s (ESC10 case 2 exploitable)", hostLabel), "")
		case "", "NOT_SET":
			v.addResult(w, "WARN", "ADCS-ESC10", fmt.Sprintf("CertificateMappingMethods not configured on %s (registry key missing)", hostLabel), "")
		default:
			v.addResult(w, "FAIL", "ADCS-ESC10", fmt.Sprintf("CertificateMappingMethods=%s on %s (expected 4)", val, hostLabel), "")
		}
	}

	if len(case1Hosts) == 0 && len(case2Hosts) == 0 {
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
		output := v.runPS(ctx, host,
			`certutil -getreg CA\InterfaceFlags 2>&1`)
		// When the flag is removed, IF_ENFORCEENCRYPTICERTREQUEST should NOT appear
		if !strings.Contains(output, "IF_ENFORCEENCRYPTICERTREQUEST") {
			v.addResult(w, "PASS", "ADCS-ESC11", fmt.Sprintf("IF_ENFORCEENCRYPTICERTREQUEST disabled on %s (ESC11 exploitable)", hostLabel), "")
		} else {
			v.addResult(w, "FAIL", "ADCS-ESC11", fmt.Sprintf("IF_ENFORCEENCRYPTICERTREQUEST still set on %s", hostLabel), "")
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
		output := v.runPS(ctx, templateQueryHost,
			`$t = Get-ADObject -Filter {displayName -eq 'Web Server' -and objectClass -eq 'pKICertificateTemplate'} -SearchBase ("CN=Certificate Templates,CN=Public Key Services,CN=Services," + (Get-ADRootDSE).configurationNamingContext) -Properties nTSecurityDescriptor; `+
				`if (-not $t) { Write-Output 'TEMPLATE_NOT_FOUND'; exit }; `+
				`Import-Module ActiveDirectory; Set-Location AD:; `+
				`$acl = Get-Acl -Path $t.DistinguishedName; `+
				`$match = $acl.Access | Where-Object { $_.IdentityReference -like '*Domain Users*' -and $_.ActiveDirectoryRights -match 'ExtendedRight' }; `+
				`if ($match) { Write-Output 'ENROLL_FOUND' } else { Write-Output 'ENROLL_NOT_FOUND' }`)
		switch {
		case strings.Contains(output, "ENROLL_FOUND"):
			v.addResult(w, "PASS", "ADCS-ESC15", fmt.Sprintf("Domain Users can enroll Web Server template on %s (ESC15 exploitable)", hostLabel), "")
		case strings.Contains(output, "ENROLL_NOT_FOUND"):
			v.addResult(w, "FAIL", "ADCS-ESC15", fmt.Sprintf("Domain Users CANNOT enroll Web Server template on %s", hostLabel), "")
		case strings.Contains(output, "TEMPLATE_NOT_FOUND"):
			v.addResult(w, "FAIL", "ADCS-ESC15", fmt.Sprintf("Web Server template NOT found on %s", hostLabel), "")
		default:
			v.addResult(w, "WARN", "ADCS-ESC15", fmt.Sprintf("Could not verify Web Server template ACL on %s", hostLabel), "")
		}
	}
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
		script := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
Import-Module ActiveDirectory
Set-Location AD:
$target = '%s'
$sourceSam = '%s'
$sourceMatch = '*%s*'
try {
  if ($target -match '=') {
    $objDN = $target
    $objAcl = Get-Acl -Path $objDN -ErrorAction Stop
  } else {
    $obj = Get-ADObject -Filter "SamAccountName -eq '$target'" -ErrorAction Stop
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
}`, target, sourceSam, sourceSam)

		output := v.runPS(ctx, dcRole, script)

		switch {
		case strings.Contains(output, "ACL_FOUND"):
			v.addResult(w, "PASS", "ACL", fmt.Sprintf("%s has %s on %s", source, af.ACL.Right, target), "")
		case strings.Contains(output, "ACL_NOT_FOUND"):
			v.addResult(w, "FAIL", "ACL", fmt.Sprintf("%s does NOT have %s on %s", source, af.ACL.Right, target), "")
		default:
			v.addResult(w, "WARN", "ACL", fmt.Sprintf("Could not verify ACL: %s -> %s (%s)", source, target, af.ACL.Right), "")
		}
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
				output := v.runPS(ctx, srcHost,
					`Get-ADTrust -Filter * | Select-Object Name,Direction,TrustType | Format-Table -AutoSize | Out-String`)
				if strings.Contains(strings.ToLower(output), strings.ToLower(tf.TargetDomain)) {
					v.addResult(w, "PASS", "Trusts",
						fmt.Sprintf("Trust configured: %s -> %s", tf.SourceDomain, tf.TargetDomain), "")
				} else {
					v.addResult(w, "FAIL", "Trusts",
						fmt.Sprintf("Trust NOT found: %s -> %s", tf.SourceDomain, tf.TargetDomain), "")
				}
			}
		}

		if tf.TargetDCRole != "" {
			tgtHost := strings.ToUpper(tf.TargetDCRole)
			if v.hasHost(tgtHost) {
				output := v.runPS(ctx, tgtHost,
					`Get-ADTrust -Filter * | Select-Object Name,Direction,TrustType | Format-Table -AutoSize | Out-String`)
				if strings.Contains(strings.ToLower(output), strings.ToLower(tf.SourceDomain)) {
					v.addResult(w, "PASS", "Trusts",
						fmt.Sprintf("Trust configured: %s -> %s", tf.TargetDomain, tf.SourceDomain), "")
				} else {
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
		output := v.runPS(ctx, host,
			`Get-Service Spooler | Select-Object Status | Format-Table -AutoSize | Out-String`)
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
		output := v.runPS(ctx, host,
			`Get-Service W3SVC -ErrorAction SilentlyContinue | Select-Object Name,Status | Format-Table -AutoSize | Out-String`)
		if strings.Contains(strings.ToLower(output), "running") {
			v.addResult(w, "PASS", "Services", fmt.Sprintf("IIS running on %s", hostLabel), "")
		} else if strings.TrimSpace(output) != "" {
			v.addResult(w, "WARN", "Services", fmt.Sprintf("IIS not running on %s", hostLabel), "")
		}

		output = v.runPS(ctx, host,
			`Get-Service WebClient -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Status`)
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
			output := v.runPS(ctx, host, fmt.Sprintf(
				`$t = Get-ScheduledTask -TaskName '%s' -ErrorAction SilentlyContinue; if ($t) { $t.State } else { '___NOTFOUND___' }`, taskName))
			state := strings.TrimSpace(output)
			switch state {
			case "___NOTFOUND___":
				v.addResult(w, "FAIL", "ScheduledTasks", fmt.Sprintf("%s NOT found on %s", taskName, host), "")
			case "":
				// Empty output means WinRM returned nothing (transient error);
				// the task likely exists but we couldn't read its state.
				v.addResult(w, "WARN", "ScheduledTasks", fmt.Sprintf("%s state unknown on %s (WinRM returned empty)", taskName, host), "")
			default:
				// Task exists; any state (Ready, Running, Disabled, etc.) is a PASS
				// since we're validating that provisioning created the task.
				v.addResult(w, "PASS", "ScheduledTasks", fmt.Sprintf("%s is %s on %s", taskName, state, host), "")
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
		output := v.runPS(ctx, host,
			`$v = Get-ItemProperty -Path 'HKLM:\Software\policies\Microsoft\Windows NT\DNSClient' -Name EnableMulticast -ErrorAction SilentlyContinue; if ($v) { $v.EnableMulticast } else { 'NOT_SET' }`)
		val := strings.TrimSpace(output)
		switch val {
		case "1", "NOT_SET":
			v.addResult(w, "PASS", "LLMNR", fmt.Sprintf("LLMNR enabled on %s", hostLabel), "")
		case "":
			v.addResult(w, "WARN", "LLMNR", fmt.Sprintf("LLMNR status unknown on %s (command returned empty)", hostLabel), "")
		default:
			v.addResult(w, "FAIL", "LLMNR", fmt.Sprintf("LLMNR disabled on %s (value=%s)", hostLabel, val), "")
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
		output := v.runPS(ctx, host,
			`Get-GPO -All | Where-Object { $_.DisplayName -notmatch 'Default Domain' } | Select-Object -ExpandProperty DisplayName`)
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
		output := v.runPS(ctx, host, fmt.Sprintf(
			`Get-ADServiceAccount -Identity '%s' -Properties Enabled | Select-Object -ExpandProperty Enabled`, gf.GMSA.Name))
		if strings.Contains(strings.ToLower(output), "true") {
			v.addResult(w, "PASS", "gMSA", fmt.Sprintf("gMSA %s exists and enabled in %s", gf.GMSA.Name, gf.Domain), "")
		} else {
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
		output := v.runPS(ctx, dc, fmt.Sprintf(
			`Get-ADComputer -Identity '%s' -Properties ms-Mcs-AdmPwd | Select-Object -ExpandProperty ms-Mcs-AdmPwd`, hostname))
		if strings.TrimSpace(output) != "" {
			v.addResult(w, "PASS", "LAPS", fmt.Sprintf("LAPS password set for %s", hostname), "")
		} else {
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
			output := v.runPS(ctx, dc, fmt.Sprintf(
				`$computers = Get-ADComputer -Filter {ms-Mcs-AdmPwd -like '*'} -Properties ms-Mcs-AdmPwd -SearchBase (Get-ADDomain).DistinguishedName -ErrorAction SilentlyContinue; `+
					`Import-Module ActiveDirectory; Set-Location AD:; `+
					`$found = $false; foreach ($c in $computers) { `+
					`$acl = Get-Acl -Path $c.DistinguishedName; `+
					`$match = $acl.Access | Where-Object { $_.IdentityReference -like '*%s*' }; `+
					`if ($match) { $found = $true; break } }; `+
					`if ($found) { Write-Output 'READER_OK' } else { Write-Output 'READER_NOT_FOUND' }`,
				reader))
			switch {
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
		output := v.runPS(ctx, host, fmt.Sprintf(
			`netdom trust %s /d:%s /quarantine 2>&1`, tf.SourceDomain, tf.TargetDomain))
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
		output := v.runPS(ctx, host,
			`Get-SmbShare | Where-Object { $_.Name -notmatch 'ADMIN\$|C\$|IPC\$' } | Select-Object -ExpandProperty Name`)
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
		output := v.runPS(ctx, host,
			`Get-NetFirewallProfile | Where-Object { $_.Enabled -eq $true } | Select-Object -ExpandProperty Name`)
		enabledProfiles := parseOutputLines(output)
		if len(enabledProfiles) == 0 {
			v.addResult(w, "PASS", "Firewall", fmt.Sprintf("Firewall disabled on %s", hostLabel), "")
		} else {
			v.addResult(w, "FAIL", "Firewall", fmt.Sprintf("Firewall still enabled on %s (profiles: %s)", hostLabel, strings.Join(enabledProfiles, ", ")), "")
		}
	}
}

func (v *Validator) checkPasswordPolicy(ctx context.Context, w io.Writer) {
	printHeader(w, "Password Policy")

	for _, role := range v.lab.DCs() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		output := v.runPS(ctx, host,
			`try { $p = Get-ADDefaultDomainPasswordPolicy -ErrorAction Stop; Write-Output "$($p.ComplexityEnabled)|$($p.MinPasswordLength)|$($p.LockoutThreshold)" } catch { Write-Output "ERROR: $_" }`)
		parts := strings.Split(strings.TrimSpace(output), "|")
		if len(parts) < 3 {
			v.addResult(w, "WARN", "PasswordPolicy", fmt.Sprintf("Could not read password policy on %s", host), "")
			continue
		}
		domain := v.lab.DomainForHost(strings.ToLower(host))
		if domain == "" {
			domain = host
		}
		complexity := parts[0]
		minLen := parts[1]
		lockout := parts[2]
		if strings.EqualFold(complexity, "false") {
			v.addResult(w, "PASS", "PasswordPolicy", fmt.Sprintf("Password complexity disabled in %s (weak policy)", domain), "")
		} else {
			v.addResult(w, "INFO", "PasswordPolicy", fmt.Sprintf("Password complexity enabled in %s", domain), "")
		}
		if minLen == "0" || minLen == "1" || minLen == "2" || minLen == "3" {
			v.addResult(w, "PASS", "PasswordPolicy", fmt.Sprintf("Min password length is %s in %s (weak)", minLen, domain), "")
		} else {
			v.addResult(w, "INFO", "PasswordPolicy", fmt.Sprintf("Min password length is %s in %s", minLen, domain), "")
		}
		if lockout == "0" {
			v.addResult(w, "PASS", "PasswordPolicy", fmt.Sprintf("No lockout threshold in %s (spray-friendly)", domain), "")
		} else {
			v.addResult(w, "INFO", "PasswordPolicy", fmt.Sprintf("Lockout threshold is %s in %s", lockout, domain), "")
		}
	}
}

func (v *Validator) checkLDAPSigning(ctx context.Context, w io.Writer) {
	printHeader(w, "LDAP Signing & Channel Binding")

	for _, role := range v.lab.DCs() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		domain := v.lab.DomainForHost(strings.ToLower(host))
		if domain == "" {
			domain = host
		}

		// LDAP client signing requirements
		output := v.runPS(ctx, host,
			`Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\LDAP' -Name LDAPServerSigningRequirements -ErrorAction SilentlyContinue | Select-Object -ExpandProperty LDAPServerSigningRequirements`)
		val := strings.TrimSpace(output)
		switch val {
		case "0", "":
			v.addResult(w, "PASS", "LDAP", fmt.Sprintf("LDAP signing not required in %s (relay possible)", domain), "")
		case "1":
			v.addResult(w, "INFO", "LDAP", fmt.Sprintf("LDAP signing negotiated in %s", domain), "")
		default:
			v.addResult(w, "INFO", "LDAP", fmt.Sprintf("LDAP signing required (%s) in %s", val, domain), "")
		}

		// LDAP server integrity
		output = v.runPS(ctx, host,
			`Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\NTDS\Parameters' -Name LDAPServerIntegrity -ErrorAction SilentlyContinue | Select-Object -ExpandProperty LDAPServerIntegrity`)
		val = strings.TrimSpace(output)
		switch val {
		case "0", "":
			v.addResult(w, "PASS", "LDAP", fmt.Sprintf("LDAP server integrity disabled in %s", domain), "")
		case "1":
			v.addResult(w, "INFO", "LDAP", fmt.Sprintf("LDAP server integrity = %s in %s (SASL only)", val, domain), "")
		default:
			v.addResult(w, "INFO", "LDAP", fmt.Sprintf("LDAP server integrity required (%s) in %s", val, domain), "")
		}

		// LDAP channel binding
		output = v.runPS(ctx, host,
			`Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\NTDS\Parameters' -Name LdapEnforceChannelBindings -ErrorAction SilentlyContinue | Select-Object -ExpandProperty LdapEnforceChannelBindings`)
		val = strings.TrimSpace(output)
		switch val {
		case "0", "":
			v.addResult(w, "PASS", "LDAP", fmt.Sprintf("LDAP channel binding disabled in %s (relay possible)", domain), "")
		case "1":
			v.addResult(w, "INFO", "LDAP", fmt.Sprintf("LDAP channel binding optional in %s", domain), "")
		default:
			v.addResult(w, "INFO", "LDAP", fmt.Sprintf("LDAP channel binding enforced (%s) in %s", val, domain), "")
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
		output := v.runPS(ctx, host,
			`Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\Lsa' -Name RunAsPPL -ErrorAction SilentlyContinue | Select-Object -ExpandProperty RunAsPPL`)
		val := strings.TrimSpace(output)
		switch val {
		case "0", "":
			v.addResult(w, "PASS", "LSAProtection", fmt.Sprintf("RunAsPPL disabled on %s (LSASS dumpable)", hostLabel), "")
		case "1":
			v.addResult(w, "INFO", "LSAProtection", fmt.Sprintf("RunAsPPL enabled on %s (LSASS protected)", hostLabel), "")
		case "2":
			v.addResult(w, "INFO", "LSAProtection", fmt.Sprintf("RunAsPPL locked on %s (LSASS protected, UEFI)", hostLabel), "")
		default:
			v.addResult(w, "INFO", "LSAProtection", fmt.Sprintf("RunAsPPL=%s on %s", val, hostLabel), "")
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
		output := v.runPS(ctx, host,
			`Get-SmbShare -Name CertEnroll -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Path`)
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
		output := v.runPS(ctx, dcRole, fmt.Sprintf(
			`$u = Get-ADUser -Identity '%s' -ErrorAction SilentlyContinue; if ($u) { 'USER_FOUND' } else { 'USER_NOT_FOUND' }`,
			uf.Username))
		switch {
		case strings.Contains(output, "USER_FOUND"):
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("%s (password=%s) exists in %s", uf.Username, uf.User.Password, uf.Domain), "")
		case strings.Contains(output, "USER_NOT_FOUND"):
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("%s does NOT exist in %s (expected weak-cred user)", uf.Username, uf.Domain), "")
		default:
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Could not verify %s in %s", uf.Username, uf.Domain), "")
		}
	}
}

// checkAutologonRegistry verifies AutoAdminLogon registry values are populated
// with plaintext credentials on hosts running the vulns_autologon role.
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
		output := v.runPS(ctx, host,
			`$k='HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon'; `+
				`$a=(Get-ItemProperty -Path $k -Name AutoAdminLogon -ErrorAction SilentlyContinue).AutoAdminLogon; `+
				`$u=(Get-ItemProperty -Path $k -Name DefaultUserName -ErrorAction SilentlyContinue).DefaultUserName; `+
				`$p=(Get-ItemProperty -Path $k -Name DefaultPassword -ErrorAction SilentlyContinue).DefaultPassword; `+
				`Write-Output ("AAL=$a|USER=$u|PWLEN=" + ($p | Measure-Object -Character).Characters)`)
		line := strings.TrimSpace(output)
		if line == "" {
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Autologon registry unreadable on %s", hostLabel), "")
			continue
		}
		// Parse AAL=, USER=, PWLEN= fields.
		fields := map[string]string{}
		for _, p := range strings.Split(line, "|") {
			if kv := strings.SplitN(p, "=", 2); len(kv) == 2 {
				fields[kv[0]] = kv[1]
			}
		}
		aal := fields["AAL"]
		user := fields["USER"]
		pwlen := fields["PWLEN"]
		switch {
		case aal == "1" && user != "" && pwlen != "" && pwlen != "0":
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("Autologon enabled on %s (user=%s, pw stored)", hostLabel, user), "")
		case aal == "1" && (user == "" || pwlen == "" || pwlen == "0"):
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("AutoAdminLogon=1 on %s but credentials missing", hostLabel), "")
		default:
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("AutoAdminLogon NOT enabled on %s (AAL=%s)", hostLabel, aal), "")
		}
	}
}

// checkCmdkeyCredentials verifies stored Credential Manager entries
// (typically TERMSRV/* targets) populated by the vulns_credentials role.
func (v *Validator) checkCmdkeyCredentials(ctx context.Context, w io.Writer) {
	printHeader(w, "Stored Credential Manager Entries")

	hosts := v.lab.HostsWithVuln("credentials")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "Credentials", "No credentials vulns configured", "")
		return
	}

	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		// cmdkey reports per-user creds; query under the configured runas user
		// is impractical via SSM. Falling back to enumerating Credential
		// Manager via vaultcmd-style reg query covers the most reliable
		// surface — TERMSRV credentials are stored as Generic credentials.
		output := v.runPS(ctx, host,
			`cmdkey /list 2>&1 | Out-String`)
		lower := strings.ToLower(output)
		switch {
		case strings.Contains(lower, "termsrv/"):
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("TERMSRV credential found on %s", hostLabel), "")
		case strings.Contains(lower, "target:"):
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Credentials stored on %s but no TERMSRV target", hostLabel), "")
		default:
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("No stored credentials on %s", hostLabel), "")
		}
	}
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
		output := v.runPS(ctx, host,
			`$root='C:\Windows\SYSVOL'; if (-not (Test-Path $root)) { Write-Output 'NO_SYSVOL'; exit }; `+
				`$pat='password|pwd|secret|cpassword'; `+
				`$hits = Get-ChildItem -Path $root -Recurse -File -ErrorAction SilentlyContinue | `+
				`Select-String -Pattern $pat -SimpleMatch:$false -ErrorAction SilentlyContinue; `+
				`if (-not $hits) { Write-Output 'NO_HITS'; exit }; `+
				`$hits | Group-Object Path | ForEach-Object { Write-Output $_.Name }`)
		switch {
		case strings.Contains(output, "NO_SYSVOL"):
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("SYSVOL not present on %s", hostLabel), "")
		case strings.Contains(output, "NO_HITS"):
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("No plaintext credential markers in SYSVOL on %s", hostLabel), "")
		default:
			files := parseOutputLines(output)
			if len(files) > 0 {
				v.addResult(w, "PASS", "Credentials",
					fmt.Sprintf("SYSVOL plaintext markers on %s in %d file(s)", hostLabel, len(files)), "")
			} else {
				v.addResult(w, "WARN", "Credentials",
					fmt.Sprintf("Could not enumerate SYSVOL on %s", hostLabel), "")
			}
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
		output := v.runPS(ctx, host,
			`$root='C:\shares'; if (-not (Test-Path $root)) { Write-Output 'NO_SHARES'; exit }; `+
				`$files = Get-ChildItem -Path $root -Recurse -File -ErrorAction SilentlyContinue; `+
				`Write-Output ("FILES=" + ($files | Measure-Object).Count)`)
		switch {
		case strings.Contains(output, "NO_SHARES"):
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("C:\\shares missing on %s", hostLabel), "")
		case strings.HasPrefix(strings.TrimSpace(output), "FILES="):
			cnt := strings.TrimPrefix(strings.TrimSpace(output), "FILES=")
			if cnt == "0" {
				v.addResult(w, "FAIL", "Credentials",
					fmt.Sprintf("No files in C:\\shares on %s", hostLabel), "")
			} else {
				v.addResult(w, "PASS", "Credentials",
					fmt.Sprintf("%s file(s) staged under C:\\shares on %s", cnt, hostLabel), "")
			}
		default:
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Could not enumerate shares on %s", hostLabel), "")
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
		output := v.runPS(ctx, host,
			`$paths = @('C:\shares','C:\inetpub\wwwroot\upload','C:\thewall'); `+
				`$any = $false; `+
				`foreach ($p in $paths) { `+
				`if (-not (Test-Path $p)) { continue }; `+
				`$acl = Get-Acl -Path $p -ErrorAction SilentlyContinue; `+
				`foreach ($ace in $acl.Access) { `+
				`if ($ace.IdentityReference -match 'Everyone|Authenticated Users|IIS_IUSRS|Users' -and `+
				`($ace.FileSystemRights -match 'FullControl|Modify|Write')) { `+
				`Write-Output ("$p|$($ace.IdentityReference)|$($ace.FileSystemRights)"); $any = $true } } }; `+
				`if (-not $any) { Write-Output 'NO_PERMISSIVE_ACE' }`)
		switch {
		case strings.Contains(output, "NO_PERMISSIVE_ACE"):
			v.addResult(w, "FAIL", "Permissions",
				fmt.Sprintf("No permissive share ACEs on %s", hostLabel), "")
		case strings.TrimSpace(output) == "":
			v.addResult(w, "WARN", "Permissions",
				fmt.Sprintf("Could not read share ACLs on %s", hostLabel), "")
		default:
			lines := parseOutputLines(output)
			v.addResult(w, "PASS", "Permissions",
				fmt.Sprintf("Permissive ACEs on %s: %d entries", hostLabel, len(lines)), "")
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
		output := v.runPS(ctx, host,
			`$p='C:\users\administrator'; `+
				`if (-not (Test-Path $p)) { Write-Output 'MISSING'; exit }; `+
				`$acl = Get-Acl -Path $p -ErrorAction SilentlyContinue; `+
				`$nonAdmin = $acl.Access | Where-Object { `+
				`$_.IdentityReference -notmatch 'Administrators|SYSTEM|TrustedInstaller|CREATOR OWNER' -and `+
				`$_.FileSystemRights -match 'Read|List|Modify|FullControl' }; `+
				`if ($nonAdmin) { Write-Output 'NON_ADMIN_READ' } else { Write-Output 'ADMIN_ONLY' }`)
		switch {
		case strings.Contains(output, "MISSING"):
			v.addResult(w, "FAIL", "Credentials",
				fmt.Sprintf("C:\\users\\administrator missing on %s", hostLabel), "")
		case strings.Contains(output, "NON_ADMIN_READ"):
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("C:\\users\\administrator readable by non-admin on %s", hostLabel), "")
		case strings.Contains(output, "ADMIN_ONLY"):
			v.addResult(w, "PASS", "Credentials",
				fmt.Sprintf("C:\\users\\administrator exists on %s (admin-only ACL)", hostLabel), "")
		default:
			v.addResult(w, "WARN", "Credentials",
				fmt.Sprintf("Could not verify administrator folder on %s", hostLabel), "")
		}
	}
}

// ---- Section 6: Network Poisoning / Hardening ----

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
		output := v.runPS(ctx, host,
			`$f = Get-WindowsOptionalFeature -Online -FeatureName SMB1Protocol -ErrorAction SilentlyContinue; `+
				`if ($f) { $f.State } else { 'NOT_FOUND' }`)
		state := strings.TrimSpace(output)
		switch state {
		case "Enabled":
			v.addResult(w, "PASS", "Network",
				fmt.Sprintf("SMBv1 enabled on %s (legacy auth/relay possible)", hostLabel), "")
		case "Disabled":
			v.addResult(w, "FAIL", "Network",
				fmt.Sprintf("SMBv1 disabled on %s", hostLabel), "")
		case "NOT_FOUND", "":
			v.addResult(w, "WARN", "Network",
				fmt.Sprintf("SMBv1 feature unknown on %s", hostLabel), "")
		default:
			v.addResult(w, "INFO", "Network",
				fmt.Sprintf("SMBv1 state %s on %s", state, hostLabel), "")
		}
	}
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
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output := v.runPS(ctx, host,
			`$v = Get-ItemProperty -Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WSMAN\Service' -Name auth_credssp -ErrorAction SilentlyContinue; `+
				`if ($v) { $v.auth_credssp } else { `+
				`$v2 = Get-ItemProperty -Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WSMAN\Service\Auth' -Name CredSSP -ErrorAction SilentlyContinue; `+
				`if ($v2) { $v2.CredSSP } else { 'NOT_SET' } }`)
		val := strings.TrimSpace(output)
		switch val {
		case "1":
			v.addResult(w, "PASS", "Network",
				fmt.Sprintf("CredSSP server enabled on %s (relay target)", hostLabel), "")
		case "0":
			v.addResult(w, "FAIL", "Network",
				fmt.Sprintf("CredSSP server disabled on %s", hostLabel), "")
		case "", "NOT_SET":
			v.addResult(w, "WARN", "Network",
				fmt.Sprintf("CredSSP server not configured on %s", hostLabel), "")
		default:
			v.addResult(w, "INFO", "Network",
				fmt.Sprintf("CredSSP server value=%s on %s", val, hostLabel), "")
		}
	}

	for _, role := range clients {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output := v.runPS(ctx, host,
			`$base='HKLM:\SOFTWARE\Policies\Microsoft\Windows\CredentialsDelegation'; `+
				`if (-not (Test-Path $base)) { Write-Output 'NO_KEY'; exit }; `+
				`$afc = (Get-ItemProperty -Path $base -Name AllowFreshCredentials -ErrorAction SilentlyContinue).AllowFreshCredentials; `+
				`Write-Output ("AFC=" + $afc)`)
		line := strings.TrimSpace(output)
		switch {
		case strings.Contains(line, "NO_KEY"):
			v.addResult(w, "FAIL", "Network",
				fmt.Sprintf("CredSSP client policy missing on %s", hostLabel), "")
		case strings.HasPrefix(line, "AFC=1"):
			v.addResult(w, "PASS", "Network",
				fmt.Sprintf("CredSSP client AllowFreshCredentials=1 on %s", hostLabel), "")
		case strings.HasPrefix(line, "AFC="):
			val := strings.TrimPrefix(line, "AFC=")
			if val == "" || val == "0" {
				v.addResult(w, "FAIL", "Network",
					fmt.Sprintf("CredSSP client AllowFreshCredentials disabled on %s", hostLabel), "")
			} else {
				v.addResult(w, "INFO", "Network",
					fmt.Sprintf("CredSSP client AllowFreshCredentials=%s on %s", val, hostLabel), "")
			}
		default:
			v.addResult(w, "WARN", "Network",
				fmt.Sprintf("Could not read CredSSP client policy on %s", hostLabel), "")
		}
	}
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

	any := false
	for _, role := range servers {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output := v.runPS(ctx, host,
			`$f = Get-WindowsFeature -Name WebDAV-Redirector -ErrorAction SilentlyContinue; `+
				`if ($f) { $f.InstallState } else { 'NOT_FOUND' }`)
		state := strings.TrimSpace(output)
		switch state {
		case "Installed":
			any = true
			v.addResult(w, "PASS", "Network",
				fmt.Sprintf("WebDAV-Redirector installed on %s", hostLabel), "")
		case "Available", "Removed":
			v.addResult(w, "INFO", "Network",
				fmt.Sprintf("WebDAV-Redirector available but not installed on %s", hostLabel), "")
		case "NOT_FOUND", "":
			v.addResult(w, "INFO", "Network",
				fmt.Sprintf("WebDAV-Redirector feature not present on %s", hostLabel), "")
		default:
			v.addResult(w, "INFO", "Network",
				fmt.Sprintf("WebDAV-Redirector state %s on %s", state, hostLabel), "")
		}
	}
	if !any {
		v.addResult(w, "INFO", "Network", "WebDAV-Redirector not installed on any Windows server", "")
	}
}

// ---- Section 8: ADCS template flags ----

// adcsTemplateAttr queries a single attribute on a named cert template using
// the configuration naming context. Returns trimmed PowerShell output.
func (v *Validator) adcsTemplateAttr(ctx context.Context, dcRole, templateName, attr string) string {
	return v.runPS(ctx, dcRole, fmt.Sprintf(
		`$t = Get-ADObject -Filter {cn -eq '%s' -and objectClass -eq 'pKICertificateTemplate'} `+
			`-SearchBase ("CN=Certificate Templates,CN=Public Key Services,CN=Services," + (Get-ADRootDSE).configurationNamingContext) `+
			`-Properties %s -ErrorAction SilentlyContinue; `+
			`if (-not $t) { Write-Output 'TEMPLATE_NOT_FOUND'; exit }; `+
			`$v = $t.'%s'; `+
			`if ($v -is [System.Array]) { $v -join ',' } else { Write-Output $v }`,
		templateName, attr, attr))
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

// checkADCSESC1 verifies the ESC1 template has CT_FLAG_ENROLLEE_SUPPLIES_SUBJECT
// (msPKI-Certificate-Name-Flag bit 0x1) set.
func (v *Validator) checkADCSESC1(ctx context.Context, w io.Writer) {
	printHeader(w, "ADCS ESC1 - ENROLLEE_SUPPLIES_SUBJECT")

	dcs := v.adcsTemplateDCs()
	if len(dcs) == 0 || len(v.lab.ADCSHosts()) == 0 {
		v.addResult(w, "SKIP", "ADCS-ESC1", "No ADCS configured for this lab", "")
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

	// Look for ACLs whose target is the ESC4 template DN.
	var grantees []labmapACL
	for _, af := range v.lab.AllACLs() {
		if strings.Contains(strings.ToUpper(af.ACL.To), "CN=ESC4,CN=CERTIFICATE TEMPLATES") {
			grantees = append(grantees, labmapACL{Domain: af.Domain, DCRole: af.DCRole, Source: af.ACL.For, Right: af.ACL.Right})
		}
	}

	dcs := v.adcsTemplateDCs()
	if len(grantees) == 0 {
		// Fall back to scanning ACLs for any non-default identity.
		for _, dcRole := range dcs {
			dc := strings.ToUpper(dcRole)
			if !v.hasHost(dc) {
				continue
			}
			output := v.runPS(ctx, dc,
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
		return
	}

	// Specific grantee path — verify each labmap-configured identity has the
	// expected right.
	for _, g := range grantees {
		dc := strings.ToUpper(g.DCRole)
		if !v.hasHost(dc) {
			continue
		}
		source := v.lab.User(g.Source)
		output := v.runPS(ctx, dc, fmt.Sprintf(
			`$t = Get-ADObject -Filter {cn -eq 'ESC4' -and objectClass -eq 'pKICertificateTemplate'} `+
				`-SearchBase ("CN=Certificate Templates,CN=Public Key Services,CN=Services," + (Get-ADRootDSE).configurationNamingContext) `+
				`-ErrorAction SilentlyContinue; `+
				`if (-not $t) { Write-Output 'TEMPLATE_NOT_FOUND'; exit }; `+
				`Import-Module ActiveDirectory; Set-Location AD:; `+
				`$acl = Get-Acl -Path $t.DistinguishedName; `+
				`$ace = $acl.Access | Where-Object { $_.IdentityReference -like '*%s*' -and $_.ActiveDirectoryRights -match '%s' }; `+
				`if ($ace) { Write-Output 'ACL_FOUND' } else { Write-Output 'ACL_NOT_FOUND' }`,
			source, g.Right))
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

	// Pull AS-REP-roastable users domain by domain (these are the candidate
	// pivot identities for ESC9).
	any := false
	for _, role := range v.lab.DCs() {
		dc := strings.ToUpper(role)
		if !v.hasHost(dc) {
			continue
		}
		output := v.runPS(ctx, dc,
			`Get-ADUser -Filter {DoesNotRequirePreAuth -eq $true} -Properties DoesNotRequirePreAuth | `+
				`Select-Object -ExpandProperty SamAccountName`)
		users := parseOutputLines(output)
		domain := v.lab.DomainForHost(strings.ToLower(dc))
		if domain == "" {
			domain = dc
		}
		if len(users) == 0 {
			v.addResult(w, "FAIL", "ADCS-ESC9",
				fmt.Sprintf("No DONT_REQ_PREAUTH users in %s (no ESC9 pivot)", domain), "")
			continue
		}
		any = true
		v.addResult(w, "PASS", "ADCS-ESC9",
			fmt.Sprintf("ESC9 pivot users in %s: %s", domain, strings.Join(users, ", ")), "")
	}
	if !any {
		v.addResult(w, "FAIL", "ADCS-ESC9", "No ESC9 pivot users found in any domain", "")
	}
}

// checkADCSESC13 verifies the ESC13 template's msPKI-Certificate-Policy is
// populated (the esc13.ps1 script writes the issuance policy OID into it).
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
	}
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
			output := v.runPS(ctx, dc, fmt.Sprintf(
				`$z = Get-DnsServerZone -Name '%s' -ErrorAction SilentlyContinue; `+
					`if ($z -and $z.ZoneType -eq 'Forwarder') { 'FORWARDER' } `+
					`elseif ($z) { 'WRONG_TYPE' } else { 'NOT_FOUND' }`,
				peer))
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
			default:
				v.addResult(w, "WARN", "DNS",
					fmt.Sprintf("Could not read DNS zones on %s for %s", dc, peer), "")
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
		output := v.runPS(ctx, dc,
			`auditpol /get /category:"DS Access" /r 2>&1 | Out-String`)
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

	any := false
	for _, role := range dcs {
		dc := strings.ToUpper(role)
		if !v.hasHost(dc) {
			continue
		}
		output := v.runPS(ctx, dc,
			`$v = Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\NTDS\Diagnostics' `+
				`-Name '15 Field Engineering' -ErrorAction SilentlyContinue; `+
				`if ($v) { $v.'15 Field Engineering' } else { 'NOT_SET' }`)
		if v.reportLDAPDiagnostic(w, dc, strings.TrimSpace(output)) {
			any = true
		}
	}
	if !any && len(dcs) > 0 {
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

// reportLDAPDiagnostic emits the result for one DC and returns true when the
// 1644 events are enabled.
func (v *Validator) reportLDAPDiagnostic(w io.Writer, dc, val string) bool {
	switch val {
	case "0", "", "NOT_SET":
		v.addResult(w, "FAIL", "Audit",
			fmt.Sprintf("LDAP Field Engineering=%s on %s (1644 events disabled)", val, dc), "")
		return false
	default:
		v.addResult(w, "PASS", "Audit",
			fmt.Sprintf("LDAP Field Engineering=%s on %s (1644 events enabled)", val, dc), "")
		return true
	}
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
		output := v.runPS(ctx, host,
			`$ids = (Get-MpPreference -ErrorAction SilentlyContinue).AttackSurfaceReductionRules_Ids; `+
				`if (-not $ids -or $ids.Count -eq 0) { Write-Output 'NO_RULES'; exit }; `+
				`Write-Output ("COUNT=" + $ids.Count)`)
		line := strings.TrimSpace(output)
		switch {
		case strings.Contains(line, "NO_RULES"):
			v.addResult(w, "FAIL", "Defender",
				fmt.Sprintf("No ASR rules configured on %s", hostLabel), "")
		case strings.HasPrefix(line, "COUNT="):
			cnt := strings.TrimPrefix(line, "COUNT=")
			v.addResult(w, "PASS", "Defender",
				fmt.Sprintf("ASR rules configured on %s: %s rule(s)", hostLabel, cnt), "")
		default:
			v.addResult(w, "WARN", "Defender",
				fmt.Sprintf("Could not read ASR rules on %s", hostLabel), "")
		}
	}
}

// ---- Section 10: IIS upload ----

// checkIISUploadPermissions verifies the IIS upload directory is writable by
// IIS_IUSRS (set by the vulns_permissions role on hosts running IIS).
func (v *Validator) checkIISUploadPermissions(ctx context.Context, w io.Writer) {
	printHeader(w, "IIS Upload Folder Permissions")

	hosts := v.lab.HostsWithVuln("permissions")
	if len(hosts) == 0 {
		v.addResult(w, "SKIP", "IIS", "No permissions vulns configured", "")
		return
	}

	any := false
	for _, role := range hosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output := v.runPS(ctx, host,
			`$p='C:\inetpub\wwwroot\upload'; `+
				`if (-not (Test-Path $p)) { Write-Output 'NO_UPLOAD_DIR'; exit }; `+
				`$acl = Get-Acl -Path $p -ErrorAction SilentlyContinue; `+
				`$ace = $acl.Access | Where-Object { `+
				`$_.IdentityReference -match 'IIS_IUSRS' -and $_.FileSystemRights -match 'FullControl|Modify|Write' }; `+
				`if ($ace) { Write-Output ($ace.FileSystemRights -join ',') } else { Write-Output 'NO_IIS_ACE' }`)
		line := strings.TrimSpace(output)
		switch {
		case strings.Contains(line, "NO_UPLOAD_DIR"):
			v.addResult(w, "INFO", "IIS",
				fmt.Sprintf("No upload directory on %s (IIS not configured)", hostLabel), "")
		case strings.Contains(line, "NO_IIS_ACE"):
			v.addResult(w, "FAIL", "IIS",
				fmt.Sprintf("Upload dir on %s has no IIS_IUSRS write ACE", hostLabel), "")
		case line == "":
			v.addResult(w, "WARN", "IIS",
				fmt.Sprintf("Could not read upload ACL on %s", hostLabel), "")
		default:
			any = true
			v.addResult(w, "PASS", "IIS",
				fmt.Sprintf("IIS_IUSRS has %s on upload dir on %s", line, hostLabel), "")
		}
	}
	if !any {
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
		output := v.runPS(ctx, dcRole, fmt.Sprintf(
			`$u = Get-ADUser -Identity '%s' -Properties MemberOf -ErrorAction SilentlyContinue; `+
				`if (-not $u) { Write-Output 'USER_NOT_FOUND'; exit }; `+
				`Write-Output 'USER_FOUND'; `+
				`foreach ($g in $u.MemberOf) { Write-Output "GROUP=$g" }`,
			uf.Username))
		if !strings.Contains(output, "USER_FOUND") {
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
		output := v.runPS(ctx, dcRole, fmt.Sprintf(
			`$g = Get-ADGroup -Identity '%s' -ErrorAction SilentlyContinue; `+
				`if ($g) { 'GROUP_FOUND' } else { 'GROUP_NOT_FOUND' }`,
			gf.Group))
		switch {
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

// ---- Section 11: Local Admin Access Map ----

// checkLocalAdmins verifies, for each Windows host, that the configured
// local_groups.Administrators set matches the actual Administrators group
// membership reported by Get-LocalGroupMember. If the host has no
// configured local admins, it falls back to INFO with the live members so
// operators can compare manually.
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

		output := v.runPS(ctx, host,
			`Get-LocalGroupMember -Group Administrators -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name`)
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

		// Normalize actual entries (e.g. "SEVENKINGDOMS\\robert.baratheon")
		// for case-insensitive matching against expected ("sevenkingdoms\\robert.baratheon").
		actualSet := make(map[string]bool, len(actual))
		for _, a := range actual {
			actualSet[strings.ToLower(strings.TrimSpace(a))] = true
		}

		var missing []string
		for _, exp := range expected {
			if !actualSet[strings.ToLower(strings.TrimSpace(exp))] {
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
			// Build a single PS command that checks all KBs for this CVE
			// and emits the first installed KB id, or INSTALLED_NONE.
			ids := make([]string, len(p.KBs))
			for i, kb := range p.KBs {
				ids[i] = "'" + kb + "'"
			}
			cmd := fmt.Sprintf(
				`$kbs = @(%s); `+
					`$found = $null; `+
					`foreach ($kb in $kbs) { `+
					`  $h = Get-HotFix -Id $kb -ErrorAction SilentlyContinue; `+
					`  if ($h) { $found = $kb; break } `+
					`}; `+
					`if ($found) { Write-Output "INSTALLED=$found" } else { Write-Output 'INSTALLED_NONE' }`,
				strings.Join(ids, ","))
			output := v.runPS(ctx, host, cmd)
			line := strings.TrimSpace(output)

			switch {
			case strings.Contains(line, "INSTALLED_NONE"):
				v.addResult(w, "PASS", "CVE",
					fmt.Sprintf("%s on %s: unpatched (%s)", p.Name, hostLabel, p.CVE), "")
			case strings.HasPrefix(line, "INSTALLED="):
				kb := strings.TrimPrefix(line, "INSTALLED=")
				v.addResult(w, "INFO", "CVE",
					fmt.Sprintf("%s on %s: patched (%s, %s)", p.Name, hostLabel, kb, p.CVE), "")
			default:
				v.addResult(w, "WARN", "CVE",
					fmt.Sprintf("Could not query %s status on %s", p.Name, hostLabel), "")
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

		output := v.runPS(ctx, host,
			`Get-SmbShare -Name ADMIN$,C$ -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name`)
		shares := parseOutputLines(output)
		found := make(map[string]bool, len(shares))
		for _, s := range shares {
			found[strings.ToUpper(strings.TrimSpace(s))] = true
		}

		var missing []string
		for _, want := range []string{"ADMIN$", "C$"} {
			if !found[want] {
				missing = append(missing, want)
			}
		}
		if len(missing) == 0 {
			v.addResult(w, "PASS", "AdminShares",
				fmt.Sprintf("%s exposes ADMIN$ and C$", hostLabel), "")
		} else {
			v.addResult(w, "FAIL", "AdminShares",
				fmt.Sprintf("%s missing default shares: %s", hostLabel, strings.Join(missing, ", ")), "")
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
