# Autologon registry probe — reports the three Winlogon registry values
# that govern unattended sign-in. On Windows 10/Server 2016+,
# `ansible.windows.win_auto_logon` stores DefaultPassword in the LSA
# secret store rather than the registry, so an empty registry password
# with AAL=1 + DefaultUserName populated is the *correct* post-Ansible
# state, not a misconfiguration. The caller distinguishes those cases.
#
# Output:
#   { "aal": int, "user": string, "pw_length": int, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$path = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon'

$result = [ordered]@{
    aal       = 0
    user      = ''
    pw_length = 0
    error     = $null
}

try {
    $a = (Get-ItemProperty -Path $path -Name AutoAdminLogon   -ErrorAction SilentlyContinue).AutoAdminLogon
    $u = (Get-ItemProperty -Path $path -Name DefaultUserName  -ErrorAction SilentlyContinue).DefaultUserName
    $p = (Get-ItemProperty -Path $path -Name DefaultPassword  -ErrorAction SilentlyContinue).DefaultPassword
    if ($null -ne $a) { $result.aal       = [int]$a }
    if ($null -ne $u) { $result.user      = "$u" }
    if ($null -ne $p) { $result.pw_length = ($p | Measure-Object -Character).Characters }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
