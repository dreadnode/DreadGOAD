package validate

import (
	"context"
	"fmt"
	"strings"
)

func (v *Validator) checkCredentialDiscovery(ctx context.Context) {
	fmt.Println("\n== 1. Credential Discovery Vulnerabilities ==")

	output := v.runPS(ctx, "DC02", `Get-ADUser -Filter * -Properties Description | Where-Object {$_.Description -match 'password|heartsbane'} | Select-Object SamAccountName,Description | Format-Table -AutoSize | Out-String -Width 200`)
	if strings.Contains(strings.ToLower(output), "samwell.tarly") {
		v.addResult("PASS", "Credentials", "samwell.tarly has password in description", "")
	} else {
		v.addResult("FAIL", "Credentials", "samwell.tarly does NOT have password in description", "")
	}
}

func (v *Validator) checkKerberosAttacks(ctx context.Context) {
	fmt.Println("\n== 2. Kerberos Attack Vectors ==")

	// AS-REP Roasting
	output := v.runPS(ctx, "DC02", `Get-ADUser brandon.stark -Properties DoesNotRequirePreAuth | Select-Object SamAccountName,DoesNotRequirePreAuth | Format-Table -AutoSize | Out-String`)
	if strings.Contains(strings.ToLower(output), "true") {
		v.addResult("PASS", "Kerberos", "brandon.stark has DoesNotRequirePreAuth (AS-REP roastable)", "")
	} else {
		v.addResult("FAIL", "Kerberos", "brandon.stark does NOT have PreAuth disabled", "")
	}

	output = v.runPS(ctx, "DC03", `Get-ADUser missandei -Properties DoesNotRequirePreAuth | Select-Object SamAccountName,DoesNotRequirePreAuth | Format-Table -AutoSize | Out-String`)
	if strings.Contains(strings.ToLower(output), "true") {
		v.addResult("PASS", "Kerberos", "missandei has DoesNotRequirePreAuth (AS-REP roastable)", "")
	} else {
		v.addResult("FAIL", "Kerberos", "missandei does NOT have PreAuth disabled", "")
	}

	// Kerberoasting
	output = v.runPS(ctx, "DC02", `Get-ADUser jon.snow -Properties ServicePrincipalName | Select-Object SamAccountName,ServicePrincipalName | Format-List | Out-String`)
	if strings.Contains(strings.ToLower(output), "serviceprincipalname") {
		v.addResult("PASS", "Kerberos", "jon.snow has SPNs configured (Kerberoastable)", "")
	} else {
		v.addResult("FAIL", "Kerberos", "jon.snow does NOT have SPNs configured", "")
	}

	output = v.runPS(ctx, "DC02", `Get-ADUser sql_svc -Properties ServicePrincipalName | Select-Object SamAccountName,ServicePrincipalName | Format-List | Out-String`)
	if strings.Contains(strings.ToLower(output), "serviceprincipalname") {
		v.addResult("PASS", "Kerberos", "sql_svc has SPNs configured (Kerberoastable)", "")
	} else {
		v.addResult("FAIL", "Kerberos", "sql_svc does NOT have SPNs configured", "")
	}
}

func (v *Validator) checkNetworkMisconfigs(ctx context.Context) {
	fmt.Println("\n== 3. Network-Level Misconfigurations ==")

	for _, host := range []string{"SRV02", "SRV03"} {
		if !v.hasHost(host) {
			continue
		}
		output := v.runPS(ctx, host, `Get-SmbServerConfiguration | Select-Object RequireSecuritySignature,EnableSecuritySignature | Format-Table -AutoSize | Out-String`)
		lower := strings.ToLower(output)
		hostLabel := map[string]string{"SRV02": "CASTELBLACK", "SRV03": "BRAAVOS"}[host]

		if strings.Contains(lower, "false") && strings.Count(lower, "false") >= 2 {
			v.addResult("PASS", "Network", fmt.Sprintf("%s has SMB signing disabled", hostLabel), "")
		} else if strings.Contains(lower, "false") {
			v.addResult("WARN", "Network", fmt.Sprintf("%s has SMB signing enabled but not required", hostLabel), "")
		} else {
			v.addResult("FAIL", "Network", fmt.Sprintf("%s has SMB signing enforced", hostLabel), "")
		}
	}
}

func (v *Validator) checkAnonymousSMB(ctx context.Context) {
	fmt.Println("\n== 4. Anonymous/Guest SMB Enumeration ==")

	// RestrictAnonymous on DC02
	output := v.runPS(ctx, "DC02", `Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Lsa' -Name RestrictAnonymous -ErrorAction SilentlyContinue | Select-Object -ExpandProperty RestrictAnonymous`)
	val := strings.TrimSpace(output)
	if val == "0" {
		v.addResult("PASS", "SMB", "RestrictAnonymous is 0 on WINTERFELL (NULL sessions enabled)", "")
	} else {
		v.addResult("FAIL", "SMB", fmt.Sprintf("RestrictAnonymous is %s on WINTERFELL (expected 0)", val), "")
	}

	// RestrictAnonymousSAM
	output = v.runPS(ctx, "DC02", `Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Lsa' -Name RestrictAnonymousSAM -ErrorAction SilentlyContinue | Select-Object -ExpandProperty RestrictAnonymousSAM`)
	val = strings.TrimSpace(output)
	if val == "0" {
		v.addResult("PASS", "SMB", "RestrictAnonymousSAM is 0 on WINTERFELL (SAM enum enabled)", "")
	} else {
		v.addResult("FAIL", "SMB", fmt.Sprintf("RestrictAnonymousSAM is %s on WINTERFELL (expected 0)", val), "")
	}

	// Guest accounts on member servers
	for _, host := range []string{"SRV02", "SRV03"} {
		if !v.hasHost(host) {
			continue
		}
		hostLabel := map[string]string{"SRV02": "CASTELBLACK", "SRV03": "BRAAVOS"}[host]
		output = v.runPS(ctx, host, `Get-LocalUser -Name Guest | Select-Object Name,Enabled | Format-Table -AutoSize | Out-String`)
		if strings.Contains(strings.ToLower(output), "true") {
			v.addResult("PASS", "SMB", fmt.Sprintf("Guest account enabled on %s", hostLabel), "")
		} else {
			v.addResult("FAIL", "SMB", fmt.Sprintf("Guest account NOT enabled on %s", hostLabel), "")
		}
	}

	// LmCompatibilityLevel on DC03
	output = v.runPS(ctx, "DC03", `Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Lsa' -Name LmCompatibilityLevel -ErrorAction SilentlyContinue | Select-Object -ExpandProperty LmCompatibilityLevel`)
	val = strings.TrimSpace(output)
	if val == "0" || val == "1" || val == "2" {
		v.addResult("PASS", "SMB", fmt.Sprintf("LmCompatibilityLevel is %s on MEEREEN (NTLM downgrade vulnerable)", val), "")
	} else {
		v.addResult("FAIL", "SMB", fmt.Sprintf("LmCompatibilityLevel is %s on MEEREEN (expected 0-2)", val), "")
	}
}

func (v *Validator) checkDelegation(ctx context.Context) {
	fmt.Println("\n== 5. Delegation Configurations ==")

	output := v.runPS(ctx, "DC02", `Get-ADUser sansa.stark -Properties TrustedForDelegation | Select-Object SamAccountName,TrustedForDelegation | Format-Table -AutoSize | Out-String`)
	if strings.Contains(strings.ToLower(output), "true") {
		v.addResult("PASS", "Delegation", "sansa.stark has unconstrained delegation", "")
	} else {
		v.addResult("FAIL", "Delegation", "sansa.stark does NOT have unconstrained delegation", "")
	}

	output = v.runPS(ctx, "DC02", `Get-ADUser jon.snow -Properties msDS-AllowedToDelegateTo | Select-Object SamAccountName,msDS-AllowedToDelegateTo | Format-List | Out-String`)
	if strings.Contains(strings.ToLower(output), "msds-allowedtodelegateto") {
		v.addResult("PASS", "Delegation", "jon.snow has constrained delegation configured", "")
	} else {
		v.addResult("FAIL", "Delegation", "jon.snow does NOT have constrained delegation", "")
	}
}

func (v *Validator) checkMachineAccountQuota(ctx context.Context) {
	fmt.Println("\n== 6. Machine Account Quota ==")

	output := v.runPS(ctx, "DC01", `Get-ADObject -Identity ((Get-ADDomain).distinguishedname) -Properties ms-DS-MachineAccountQuota | Select-Object -ExpandProperty ms-DS-MachineAccountQuota`)
	val := strings.TrimSpace(output)
	if val == "10" {
		v.addResult("PASS", "MachineQuota", "Machine Account Quota is 10 (allows RBCD)", "")
	} else {
		v.addResult("WARN", "MachineQuota", fmt.Sprintf("Machine Account Quota is %s (expected 10)", val), "")
	}
}

func (v *Validator) checkMSSQL(ctx context.Context) {
	fmt.Println("\n== 7. MSSQL Configurations ==")

	for _, host := range []string{"SRV02", "SRV03"} {
		if !v.hasHost(host) {
			continue
		}
		hostLabel := map[string]string{"SRV02": "CASTELBLACK", "SRV03": "BRAAVOS"}[host]
		output := v.runPS(ctx, host, `Get-Service 'MSSQL$SQLEXPRESS' -ErrorAction SilentlyContinue | Select-Object Name,Status,StartType | Format-Table -AutoSize | Out-String`)
		if strings.Contains(strings.ToLower(output), "running") {
			v.addResult("PASS", "MSSQL", fmt.Sprintf("MSSQL running on %s", hostLabel), "")
		} else {
			v.addResult("FAIL", "MSSQL", fmt.Sprintf("MSSQL NOT running on %s", hostLabel), "")
		}
	}
}

func (v *Validator) checkADCS(ctx context.Context) {
	fmt.Println("\n== 8. ADCS Configuration ==")

	if !v.hasHost("SRV03") {
		return
	}

	output := v.runPS(ctx, "SRV03", `Get-WindowsFeature ADCS-Cert-Authority | Select-Object Name,InstallState | Format-Table -AutoSize | Out-String`)
	if strings.Contains(strings.ToLower(output), "installed") {
		v.addResult("PASS", "ADCS", "ADCS installed on BRAAVOS", "")
	} else {
		v.addResult("FAIL", "ADCS", "ADCS NOT installed on BRAAVOS", "")
	}

	output = v.runPS(ctx, "SRV03", `Get-WindowsFeature ADCS-Web-Enrollment | Select-Object Name,InstallState | Format-Table -AutoSize | Out-String`)
	if strings.Contains(strings.ToLower(output), "installed") {
		v.addResult("PASS", "ADCS", "ADCS Web Enrollment installed (ESC8 possible)", "")
	} else {
		v.addResult("WARN", "ADCS", "ADCS Web Enrollment not installed", "")
	}
}

func (v *Validator) checkACLPermissions(ctx context.Context) {
	fmt.Println("\n== 9. ACL Permissions ==")

	output := v.runPS(ctx, "DC01", `$user = Get-ADUser jaime.lannister -Properties nTSecurityDescriptor; $acl = $user.nTSecurityDescriptor.Access | Where-Object { $_.IdentityReference -like '*tywin*' }; if ($acl) { Write-Output 'ACL_FOUND' } else { Write-Output 'ACL_NOT_FOUND' }`)
	if strings.Contains(output, "ACL_FOUND") {
		v.addResult("PASS", "ACL", "tywin.lannister has ACL rights on jaime.lannister", "")
	} else if strings.Contains(output, "ACL_NOT_FOUND") {
		v.addResult("FAIL", "ACL", "tywin.lannister does NOT have ACL rights on jaime.lannister", "")
	} else {
		v.addResult("WARN", "ACL", "Could not verify ACL: tywin -> jaime", "")
	}
}

func (v *Validator) checkDomainTrusts(ctx context.Context) {
	fmt.Println("\n== 10. Domain Trusts ==")

	output := v.runPS(ctx, "DC02", `Get-ADTrust -Filter * | Select-Object Name,Direction,TrustType | Format-Table -AutoSize | Out-String`)
	if strings.Contains(strings.ToLower(output), "sevenkingdoms") {
		v.addResult("PASS", "Trusts", "Parent-child trust configured (north -> sevenkingdoms)", "")
	} else {
		v.addResult("FAIL", "Trusts", "Parent-child trust NOT found", "")
	}

	output = v.runPS(ctx, "DC01", `Get-ADTrust -Filter * | Select-Object Name,Direction,TrustType | Format-Table -AutoSize | Out-String`)
	if strings.Contains(strings.ToLower(output), "essos") {
		v.addResult("PASS", "Trusts", "Forest trust configured (sevenkingdoms <-> essos)", "")
	} else {
		v.addResult("FAIL", "Trusts", "Forest trust NOT found", "")
	}
}

func (v *Validator) checkServices(ctx context.Context) {
	fmt.Println("\n== 11. Additional Services ==")

	// Print Spooler on all DCs
	for _, host := range []string{"DC01", "DC02", "DC03"} {
		output := v.runPS(ctx, host, `Get-Service Spooler | Select-Object Status | Format-Table -AutoSize | Out-String`)
		if strings.Contains(strings.ToLower(output), "running") {
			v.addResult("PASS", "Services", fmt.Sprintf("Print Spooler running on %s (coercion possible)", host), "")
		} else {
			v.addResult("WARN", "Services", fmt.Sprintf("Print Spooler not running on %s", host), "")
		}
	}

	// IIS on SRV02
	if v.hasHost("SRV02") {
		output := v.runPS(ctx, "SRV02", `Get-Service W3SVC -ErrorAction SilentlyContinue | Select-Object Name,Status | Format-Table -AutoSize | Out-String`)
		if strings.Contains(strings.ToLower(output), "running") {
			v.addResult("PASS", "Services", "IIS running on CASTELBLACK", "")
		} else {
			v.addResult("FAIL", "Services", "IIS NOT running on CASTELBLACK", "")
		}
	}
}
