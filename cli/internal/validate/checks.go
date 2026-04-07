package validate

import (
	"context"
	"fmt"
	"strings"
)

func (v *Validator) checkCredentialDiscovery(ctx context.Context) {
	fmt.Println("\n== Credential Discovery Vulnerabilities ==")

	users := v.lab.UsersWithPasswordInDescription()
	if len(users) == 0 {
		v.addResult("SKIP", "Credentials", "No users with password-in-description configured", "")
		return
	}

	for _, uf := range users {
		dcRole := strings.ToUpper(uf.DCRole)
		output := v.runPS(ctx, dcRole, fmt.Sprintf(
			`Get-ADUser -Identity '%s' -Properties Description | Select-Object -ExpandProperty Description`,
			uf.Username))
		if strings.Contains(strings.ToLower(output), strings.ToLower(uf.User.Password)) {
			v.addResult("PASS", "Credentials", fmt.Sprintf("%s has password in description", uf.Username), "")
		} else {
			v.addResult("FAIL", "Credentials", fmt.Sprintf("%s does NOT have password in description", uf.Username), "")
		}
	}
}

func (v *Validator) checkKerberosAttacks(ctx context.Context) {
	fmt.Println("\n== Kerberos Attack Vectors ==")

	v.checkASREPRoasting(ctx)
	v.checkKerberoasting(ctx)
}

func (v *Validator) checkASREPRoasting(ctx context.Context) {
	// Find DCs that run AS-REP roasting scripts
	asrepHosts := v.lab.HostsWithScript("asrep_roasting")
	if len(asrepHosts) == 0 {
		v.addResult("SKIP", "Kerberos", "No AS-REP roasting scripts configured", "")
		return
	}

	for _, role := range asrepHosts {
		dcRole := strings.ToUpper(role)
		output := v.runPS(ctx, dcRole,
			`Get-ADUser -Filter {DoesNotRequirePreAuth -eq $true} -Properties DoesNotRequirePreAuth | Select-Object -ExpandProperty SamAccountName`)
		users := parseOutputLines(output)
		if len(users) > 0 {
			v.addResult("PASS", "Kerberos",
				fmt.Sprintf("AS-REP roastable users on %s: %s", dcRole, strings.Join(users, ", ")), "")
		} else {
			v.addResult("FAIL", "Kerberos",
				fmt.Sprintf("No AS-REP roastable users found on %s", dcRole), "")
		}
	}
}

func (v *Validator) checkKerberoasting(ctx context.Context) {
	spnUsers := v.lab.UsersWithSPNs()
	if len(spnUsers) == 0 {
		v.addResult("SKIP", "Kerberos", "No users with SPNs configured", "")
		return
	}

	for _, uf := range spnUsers {
		dcRole := strings.ToUpper(uf.DCRole)
		output := v.runPS(ctx, dcRole, fmt.Sprintf(
			`Get-ADUser -Identity '%s' -Properties ServicePrincipalName | Select-Object -ExpandProperty ServicePrincipalName`,
			uf.Username))
		if strings.TrimSpace(output) != "" {
			v.addResult("PASS", "Kerberos",
				fmt.Sprintf("%s has SPNs configured (Kerberoastable)", uf.Username), "")
		} else {
			v.addResult("FAIL", "Kerberos",
				fmt.Sprintf("%s does NOT have SPNs configured", uf.Username), "")
		}
	}
}

func (v *Validator) checkNetworkMisconfigs(ctx context.Context) {
	fmt.Println("\n== Network-Level Misconfigurations ==")

	// Check SMB signing on all Windows servers
	servers := v.lab.WindowsServers()
	if len(servers) == 0 {
		v.addResult("SKIP", "Network", "No Windows servers configured", "")
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
			v.addResult("PASS", "Network", fmt.Sprintf("%s has SMB signing disabled", hostLabel), "")
		case strings.Contains(lower, "false"):
			v.addResult("WARN", "Network", fmt.Sprintf("%s has SMB signing enabled but not required", hostLabel), "")
		default:
			v.addResult("FAIL", "Network", fmt.Sprintf("%s has SMB signing enforced", hostLabel), "")
		}
	}
}

func (v *Validator) checkAnonymousSMB(ctx context.Context) {
	fmt.Println("\n== Anonymous/Guest SMB Enumeration ==")

	// Check RestrictAnonymous on each DC
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
			v.addResult("PASS", "SMB", fmt.Sprintf("RestrictAnonymous is 0 on %s (NULL sessions enabled)", hostLabel), "")
		} else {
			v.addResult("INFO", "SMB", fmt.Sprintf("RestrictAnonymous is %s on %s", val, hostLabel), "")
		}

		output = v.runPS(ctx, host,
			`Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Lsa' -Name RestrictAnonymousSAM -ErrorAction SilentlyContinue | Select-Object -ExpandProperty RestrictAnonymousSAM`)
		val = strings.TrimSpace(output)
		if val == "0" {
			v.addResult("PASS", "SMB", fmt.Sprintf("RestrictAnonymousSAM is 0 on %s (SAM enum enabled)", hostLabel), "")
		} else {
			v.addResult("INFO", "SMB", fmt.Sprintf("RestrictAnonymousSAM is %s on %s", val, hostLabel), "")
		}
	}

	// Check Guest accounts on servers
	for _, role := range v.lab.WindowsServers() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output := v.runPS(ctx, host,
			`Get-LocalUser -Name Guest | Select-Object Name,Enabled | Format-Table -AutoSize | Out-String`)
		if strings.Contains(strings.ToLower(output), "true") {
			v.addResult("PASS", "SMB", fmt.Sprintf("Guest account enabled on %s", hostLabel), "")
		} else {
			v.addResult("FAIL", "SMB", fmt.Sprintf("Guest account NOT enabled on %s", hostLabel), "")
		}
	}

	// Check NTLM downgrade on hosts with that vuln
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
			v.addResult("PASS", "SMB", fmt.Sprintf("LmCompatibilityLevel is %s on %s (NTLM downgrade vulnerable)", val, hostLabel), "")
		} else {
			v.addResult("FAIL", "SMB", fmt.Sprintf("LmCompatibilityLevel is %s on %s (expected 0-2)", val, hostLabel), "")
		}
	}
}

func (v *Validator) checkDelegation(ctx context.Context) {
	fmt.Println("\n== Delegation Configurations ==")

	// Find DCs with delegation scripts
	allHosts := v.lab.HostsWithScript("constrained_delegation")
	allHosts = append(allHosts, v.lab.HostsWithScript("unconstrained_delegation")...)
	if len(allHosts) == 0 {
		// Fall back to checking all DCs
		allHosts = v.lab.DCs()
	}
	if len(allHosts) == 0 {
		v.addResult("SKIP", "Delegation", "No domain controllers configured", "")
		return
	}

	checked := make(map[string]bool)
	for _, role := range allHosts {
		host := strings.ToUpper(role)
		if checked[host] || !v.hasHost(host) {
			continue
		}
		checked[host] = true

		// Unconstrained delegation
		output := v.runPS(ctx, host,
			`Get-ADUser -Filter {TrustedForDelegation -eq $true} -Properties TrustedForDelegation | Select-Object -ExpandProperty SamAccountName`)
		users := parseOutputLines(output)
		if len(users) > 0 {
			v.addResult("PASS", "Delegation",
				fmt.Sprintf("Unconstrained delegation users on %s: %s", host, strings.Join(users, ", ")), "")
		}

		// Constrained delegation
		output = v.runPS(ctx, host,
			`Get-ADUser -Filter 'msDS-AllowedToDelegateTo -like "*"' -Properties msDS-AllowedToDelegateTo | Select-Object -ExpandProperty SamAccountName`)
		users = parseOutputLines(output)
		if len(users) > 0 {
			v.addResult("PASS", "Delegation",
				fmt.Sprintf("Constrained delegation users on %s: %s", host, strings.Join(users, ", ")), "")
		}
	}
}

func (v *Validator) checkMachineAccountQuota(ctx context.Context) {
	fmt.Println("\n== Machine Account Quota ==")

	for _, role := range v.lab.DCs() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		output := v.runPS(ctx, host,
			`Get-ADObject -Identity ((Get-ADDomain).distinguishedname) -Properties ms-DS-MachineAccountQuota | Select-Object -ExpandProperty ms-DS-MachineAccountQuota`)
		val := strings.TrimSpace(output)
		if val == "10" {
			v.addResult("PASS", "MachineQuota", "Machine Account Quota is 10 (allows RBCD)", "")
		} else {
			v.addResult("WARN", "MachineQuota", fmt.Sprintf("Machine Account Quota is %s (default is 10)", val), "")
		}
		return // Only check first available DC
	}
}

func (v *Validator) checkMSSQL(ctx context.Context) {
	fmt.Println("\n== MSSQL Configurations ==")

	mssqlHosts := v.lab.HostsWithMSSQL()
	if len(mssqlHosts) == 0 {
		v.addResult("SKIP", "MSSQL", "No MSSQL configured for this lab", "")
		return
	}

	for _, role := range mssqlHosts {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output := v.runPS(ctx, host,
			`Get-Service 'MSSQL$SQLEXPRESS','MSSQLSERVER' -ErrorAction SilentlyContinue | Where-Object {$_.Status -eq 'Running'} | Select-Object -ExpandProperty Name`)
		if strings.TrimSpace(output) != "" {
			v.addResult("PASS", "MSSQL", fmt.Sprintf("MSSQL running on %s", hostLabel), "")
		} else {
			v.addResult("FAIL", "MSSQL", fmt.Sprintf("MSSQL NOT running on %s", hostLabel), "")
		}
	}
}

func (v *Validator) checkADCS(ctx context.Context) {
	fmt.Println("\n== ADCS Configuration ==")

	adcsHosts := v.lab.ADCSHosts()
	if len(adcsHosts) == 0 {
		v.addResult("SKIP", "ADCS", "No ADCS configured for this lab", "")
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
			v.addResult("PASS", "ADCS", fmt.Sprintf("ADCS installed on %s", hostLabel), "")
		} else {
			v.addResult("FAIL", "ADCS", fmt.Sprintf("ADCS NOT installed on %s", hostLabel), "")
		}

		if v.lab.CAWebEnrollment() {
			output = v.runPS(ctx, host,
				`Get-WindowsFeature ADCS-Web-Enrollment | Select-Object Name,InstallState | Format-Table -AutoSize | Out-String`)
			if strings.Contains(strings.ToLower(output), "installed") {
				v.addResult("PASS", "ADCS", "ADCS Web Enrollment installed (ESC8 possible)", "")
			} else {
				v.addResult("WARN", "ADCS", "ADCS Web Enrollment not installed", "")
			}
		}
	}
}

func (v *Validator) checkACLPermissions(ctx context.Context) {
	fmt.Println("\n== ACL Permissions ==")

	acls := v.lab.AllACLs()
	if len(acls) == 0 {
		v.addResult("SKIP", "ACL", "No ACLs configured for this lab", "")
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
$sourceMatch = '*%s*'
try {
  if ($target -match '=') {
    $objDN = $target
    $objAcl = Get-Acl -Path $objDN -ErrorAction Stop
  } else {
    $obj = Get-ADObject -Filter "SamAccountName -eq '$target'" -Properties nTSecurityDescriptor -ErrorAction Stop
    if (-not $obj) { Write-Output 'TARGET_NOT_FOUND'; exit }
    $objAcl = $obj.nTSecurityDescriptor
  }
  $ace = $objAcl.Access | Where-Object { $_.IdentityReference -like $sourceMatch }
  if ($ace) { Write-Output 'ACL_FOUND' } else { Write-Output 'ACL_NOT_FOUND' }
} catch {
  Write-Output "CHECK_ERROR: $_"
}`, target, sourceFirst)

		output := v.runPS(ctx, dcRole, script)

		switch {
		case strings.Contains(output, "ACL_FOUND"):
			v.addResult("PASS", "ACL", fmt.Sprintf("%s has %s on %s", source, af.ACL.Right, target), "")
		case strings.Contains(output, "ACL_NOT_FOUND"):
			v.addResult("FAIL", "ACL", fmt.Sprintf("%s does NOT have %s on %s", source, af.ACL.Right, target), "")
		default:
			v.addResult("WARN", "ACL", fmt.Sprintf("Could not verify ACL: %s -> %s (%s)", source, target, af.ACL.Right), "")
		}
	}
}

func (v *Validator) checkDomainTrusts(ctx context.Context) {
	fmt.Println("\n== Domain Trusts ==")

	trusts := v.lab.DomainTrusts()
	if len(trusts) == 0 {
		v.addResult("SKIP", "Trusts", "No domain trusts configured for this lab", "")
		return
	}

	for _, tf := range trusts {
		if tf.SourceDCRole != "" {
			srcHost := strings.ToUpper(tf.SourceDCRole)
			if v.hasHost(srcHost) {
				output := v.runPS(ctx, srcHost,
					`Get-ADTrust -Filter * | Select-Object Name,Direction,TrustType | Format-Table -AutoSize | Out-String`)
				if strings.Contains(strings.ToLower(output), strings.ToLower(tf.TargetDomain)) {
					v.addResult("PASS", "Trusts",
						fmt.Sprintf("Trust configured: %s -> %s", tf.SourceDomain, tf.TargetDomain), "")
				} else {
					v.addResult("FAIL", "Trusts",
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
					v.addResult("PASS", "Trusts",
						fmt.Sprintf("Trust configured: %s -> %s", tf.TargetDomain, tf.SourceDomain), "")
				} else {
					v.addResult("FAIL", "Trusts",
						fmt.Sprintf("Trust NOT found: %s -> %s", tf.TargetDomain, tf.SourceDomain), "")
				}
			}
		}
	}
}

func (v *Validator) checkServices(ctx context.Context) {
	fmt.Println("\n== Additional Services ==")

	// Print Spooler on all DCs
	for _, role := range v.lab.DCs() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		output := v.runPS(ctx, host,
			`Get-Service Spooler | Select-Object Status | Format-Table -AutoSize | Out-String`)
		if strings.Contains(strings.ToLower(output), "running") {
			v.addResult("PASS", "Services", fmt.Sprintf("Print Spooler running on %s (coercion possible)", host), "")
		} else {
			v.addResult("WARN", "Services", fmt.Sprintf("Print Spooler not running on %s", host), "")
		}
	}

	// IIS on Windows servers (only report if found or expected)
	for _, role := range v.lab.WindowsServers() {
		host := strings.ToUpper(role)
		if !v.hasHost(host) {
			continue
		}
		hostLabel := strings.ToUpper(v.lab.Hostname(role))
		output := v.runPS(ctx, host,
			`Get-Service W3SVC -ErrorAction SilentlyContinue | Select-Object Name,Status | Format-Table -AutoSize | Out-String`)
		if strings.Contains(strings.ToLower(output), "running") {
			v.addResult("PASS", "Services", fmt.Sprintf("IIS running on %s", hostLabel), "")
		} else if strings.TrimSpace(output) != "" {
			v.addResult("WARN", "Services", fmt.Sprintf("IIS not running on %s", hostLabel), "")
		}
	}
}

// parseOutputLines splits PowerShell output into non-empty trimmed lines.
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
