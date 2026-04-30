# Azure first-boot bootstrap for GOAD lab hosts. Custom Script Extension runs
# this via `powershell -EncodedCommand`, whose cmdline cap (8192 chars) limits
# the script size — keep this minimal. Server 2022 Datacenter Azure Edition has
# TLS 1.2 enabled by default, so no SCHANNEL fixups needed here.
$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

# Reassert local Administrator + provision the ansible service account.
net user Administrator '${admin_password}' /expires:never /y
net user ansible '${admin_password}' /add /expires:never /y
net localgroup administrators ansible /add

# WinRM for the in-VNet Ansible controller. Default firewall rule scopes 5985
# to LocalSubnet only; controller (10.8.3.0/28) is in a different subnet from
# GOAD (10.8.1.0/24) so widen RemoteAddress to Any. Network ACL is enforced
# by the private NSG (10.8.0.0/16).
Set-Service -Name WinRM -StartupType Automatic
Start-Service -Name WinRM
& winrm quickconfig -quiet -force | Out-Null
& winrm set winrm/config/service '@{AllowUnencrypted="true"}' | Out-Null
& winrm set winrm/config/service/auth '@{Basic="true"}' | Out-Null
& winrm set winrm/config/service/auth '@{Negotiate="true"}' | Out-Null

$rule = 'WinRM-HTTP-Any'
Get-NetFirewallRule -Name $rule -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue
New-NetFirewallRule -Name $rule -DisplayName 'WinRM HTTP from any (NSG-gated)' `
    -Direction Inbound -Action Allow -Protocol TCP -LocalPort 5985 `
    -Profile Any -RemoteAddress Any | Out-Null

Write-Output "DreadGOAD Azure bootstrap complete on $env:COMPUTERNAME"
