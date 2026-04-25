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
			`Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Lsa' -Name LmCompatibilityLevel -ErrorAction SilentlyContinue | Select-Object -ExpandProperty LmCompatibilityLevel`)
		val := strings.TrimSpace(output)
		if val == "0" || val == "1" || val == "2" {
			v.addResult(w, "PASS", "SMB", fmt.Sprintf("LmCompatibilityLevel is %s on %s (NTLM downgrade vulnerable)", val, hostLabel), "")
		} else {
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
  $match = $acl | Where-Object { $_.Identity -like '*%s*' -and $_.Rights -match 'ManageCa' }
  if ($match) { Write-Output 'MANAGECA_FOUND' } else { Write-Output 'MANAGECA_NOT_FOUND' }
} catch {
  Write-Output "CHECK_ERROR: $_"
}`, strings.ReplaceAll(f.CAManager, `\`, `\\`))

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
			`Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\Kdc' -Name StrongCertificateBindingEnforcement -ErrorAction SilentlyContinue | Select-Object -ExpandProperty StrongCertificateBindingEnforcement`)
		val := strings.TrimSpace(output)
		if val == "0" {
			v.addResult(w, "PASS", "ADCS-ESC10", fmt.Sprintf("StrongCertificateBindingEnforcement=0 on %s (ESC10 case 1 exploitable)", hostLabel), "")
		} else {
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
			`Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\SecurityProviders\Schannel' -Name CertificateMappingMethods -ErrorAction SilentlyContinue | Select-Object -ExpandProperty CertificateMappingMethods`)
		val := strings.TrimSpace(output)
		if val == "4" {
			v.addResult(w, "PASS", "ADCS-ESC10", fmt.Sprintf("CertificateMappingMethods=0x4 on %s (ESC10 case 2 exploitable)", hostLabel), "")
		} else {
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

		sourceFirst := strings.SplitN(source, ".", 2)[0]
		// Strip trailing $ for gMSA accounts to match the identity reference
		sourceFirst = strings.TrimSuffix(sourceFirst, "$")

		// Build the PowerShell lookup for the target object.
		// DN paths (containing = signs) are resolved directly via Get-Acl;
		// SamAccountNames are looked up with Get-ADObject which finds
		// users, groups, and service accounts alike.
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
  # Try name-based match first
  $ace = $objAcl.Access | Where-Object { $_.IdentityReference -like $sourceMatch }
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
}`, target, sourceFirst, sourceFirst)

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
				`Get-ScheduledTask -TaskName '%s' -ErrorAction SilentlyContinue | Select-Object -ExpandProperty State`, taskName))
			state := strings.TrimSpace(output)
			switch {
			case strings.EqualFold(state, "Running") || strings.EqualFold(state, "Ready"):
				v.addResult(w, "PASS", "ScheduledTasks", fmt.Sprintf("%s is %s on %s", taskName, state, host), "")
			case state != "":
				v.addResult(w, "WARN", "ScheduledTasks", fmt.Sprintf("%s state is %s on %s", taskName, state, host), "")
			default:
				v.addResult(w, "FAIL", "ScheduledTasks", fmt.Sprintf("%s NOT found on %s", taskName, host), "")
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
		if val == "1" || val == "NOT_SET" {
			v.addResult(w, "PASS", "LLMNR", fmt.Sprintf("LLMNR enabled on %s", hostLabel), "")
		} else {
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
			`$p = Get-ADDefaultDomainPasswordPolicy; Write-Output "$($p.ComplexityEnabled)|$($p.MinPasswordLength)|$($p.LockoutThreshold)"`)
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
