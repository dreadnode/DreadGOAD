# ADCS ESC7 — ManageCA ACL probe.
#
# Confirms the lab's configured CA Manager identity has the ManageCa right on
# the local CA, which is the post-condition of the adcs_esc7 Ansible role.
#
# Inputs (rendered by validate.runScriptJSON via text/template):
#   Identity — sAMAccountName-style identity, e.g. "essos\viserys.targaryen"
#
# Output: a single JSON object between BEGIN_JSON/END_JSON markers.
#   { "pspki": bool, "found": bool, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$Identity = {{psq .Identity}}

$result = [ordered]@{
    pspki = $true
    found = $false
    error = $null
}

try {
    if (-not (Get-Module -ListAvailable -Name PSPKI)) {
        $result.pspki = $false
    } else {
        Import-Module -Name PSPKI
        $ca  = Get-CertificationAuthority
        $acl = Get-CertificationAuthority $ca.ComputerName | Get-CertificationAuthorityAcl
        $match = $acl.Access | Where-Object {
            $_.IdentityReference -like "*$Identity*" -and $_.Rights -match 'ManageCa'
        }
        $result.found = [bool]$match
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
