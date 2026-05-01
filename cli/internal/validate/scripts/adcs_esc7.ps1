# ADCS ESC7 — ManageCA ACL probe.
#
# Confirms the lab's configured CA Manager identity has the ManageCa right on
# the CA. Designed to run on the DC where upstream's vulns_adcs_esc7 role
# installs PSPKI; PSPKI's Get-CertificationAuthority hits the AD configuration
# partition, so the script elevates to a domain admin via Invoke-Command --
# the same `become: runas` trick the role uses.
#
# Inputs (rendered by validate.runScriptJSON via text/template):
#   Identity       (sAMAccountName-style identity, e.g. "essos\viserys.targaryen")
#   DomainNetBIOS  (NETBIOS domain name for the credential, e.g. "ESSOS")
#   DomainPassword (domain admin password)
#   AdminUser      (inventory admin_user; "administrator" on Ludus, "goadmin" on AWS/Azure)
#
# Output: a single JSON object between BEGIN_JSON/END_JSON markers.
#   { "pspki": bool, "found": bool, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$Identity       = {{psq .Identity}}
$DomainNetBIOS  = {{psq .DomainNetBIOS}}
$DomainPassword = {{psq .DomainPassword}}
$AdminUser      = {{psq .AdminUser}}

$result = [ordered]@{
    pspki = $true
    found = $false
    error = $null
}

try {
    if (-not (Get-Module -ListAvailable -Name PSPKI)) {
        $result.pspki = $false
    } else {
        $secPass = ConvertTo-SecureString $DomainPassword -AsPlainText -Force
        $cred    = New-Object System.Management.Automation.PSCredential(
                       "$DomainNetBIOS\$AdminUser", $secPass)

        $probe = {
            param($Identity)
            $ErrorActionPreference = 'Stop'
            Import-Module -Name PSPKI
            $ca  = Get-CertificationAuthority
            $acl = Get-CertificationAuthority $ca.ComputerName | Get-CertificationAuthorityAcl
            $match = $acl.Access | Where-Object {
                $_.IdentityReference -like "*$Identity*" -and $_.Rights -match 'ManageCa'
            }
            [bool]$match
        }

        $result.found = [bool](Invoke-Command -ComputerName localhost `
            -Credential $cred -Authentication Negotiate `
            -ScriptBlock $probe -ArgumentList $Identity)
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
