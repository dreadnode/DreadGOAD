# SMBv1 feature probe — reports whether the SMB1Protocol Windows
# optional feature is enabled. Used by the smbv1 vuln check to confirm
# Ansible left the legacy protocol in place.
#
# Output:
#   { "found": bool, "state": string, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{ found = $false; state = ''; error = $null }

try {
    $f = Get-WindowsOptionalFeature -Online -FeatureName SMB1Protocol -ErrorAction SilentlyContinue
    if ($f) {
        $result.found = $true
        $result.state = "$($f.State)"
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
