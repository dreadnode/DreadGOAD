# ADCS features probe — reports install state of the two Windows features
# the ADCS Ansible role lays down on the CA host.
#
# Output:
#   { "cert_authority": bool, "web_enrollment": bool, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{
    cert_authority = $false
    web_enrollment = $false
    error          = $null
}

try {
    $f1 = Get-WindowsFeature ADCS-Cert-Authority -ErrorAction SilentlyContinue
    if ($f1) { $result.cert_authority = ($f1.InstallState -eq 'Installed') }

    $f2 = Get-WindowsFeature ADCS-Web-Enrollment -ErrorAction SilentlyContinue
    if ($f2) { $result.web_enrollment = ($f2.InstallState -eq 'Installed') }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
