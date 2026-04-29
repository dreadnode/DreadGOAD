# CredSSP server probe — reports whether WSMAN service has CredSSP auth
# enabled. The setting lives at one of two registry locations depending
# on Windows build, so we check both. The Ansible role flips this on so
# the host accepts CredSSP auth (relay target).
#
# Output:
#   { "present": bool, "value": int, "source": string, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{
    present = $false
    value   = 0
    source  = ''
    error   = $null
}

try {
    $p1 = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WSMAN\Service'
    $v  = (Get-ItemProperty -Path $p1 -Name auth_credssp -ErrorAction SilentlyContinue).auth_credssp
    if ($null -ne $v) {
        $result.present = $true
        $result.value   = [int]$v
        $result.source  = 'auth_credssp'
    } else {
        $p2 = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WSMAN\Service\Auth'
        $v2 = (Get-ItemProperty -Path $p2 -Name CredSSP -ErrorAction SilentlyContinue).CredSSP
        if ($null -ne $v2) {
            $result.present = $true
            $result.value   = [int]$v2
            $result.source  = 'Auth\CredSSP'
        }
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
