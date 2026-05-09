# CredSSP client probe — reports the CredentialsDelegation policy that
# governs whether the client may delegate fresh credentials over CredSSP.
# The Ansible role lays down the policy key with AllowFreshCredentials=1
# so the host can act as a CredSSP relay client.
#
# Output:
#   { "policy_present": bool, "afc_present": bool, "afc": int,
#     "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{
    policy_present = $false
    afc_present    = $false
    afc            = 0
    error          = $null
}

try {
    $base = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\CredentialsDelegation'
    if (Test-Path $base) {
        $result.policy_present = $true
        $afc = (Get-ItemProperty -Path $base -Name AllowFreshCredentials -ErrorAction SilentlyContinue).AllowFreshCredentials
        if ($null -ne $afc) {
            $result.afc_present = $true
            $result.afc         = [int]$afc
        }
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
