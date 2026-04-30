# Azure first-boot bootstrap for GOAD lab hosts.
# Executed by Custom Script Extension via Run Command.
# Computer name is set declaratively by the azurerm_windows_virtual_machine
# resource, so no rename/restart is performed here.

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

# Enable TLS 1.2 system-wide (still required on Server 2019/2022 marketplace images).
New-Item 'HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\TLS 1.2\Server' -Force | Out-Null
New-Item 'HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\TLS 1.2\Client' -Force | Out-Null
New-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\TLS 1.2\Server' -Name 'Enabled'           -Value 1 -PropertyType 'DWord' -Force | Out-Null
New-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\TLS 1.2\Server' -Name 'DisabledByDefault' -Value 0 -PropertyType 'DWord' -Force | Out-Null
New-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\TLS 1.2\Client' -Name 'Enabled'           -Value 1 -PropertyType 'DWord' -Force | Out-Null
New-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\TLS 1.2\Client' -Name 'DisabledByDefault' -Value 0 -PropertyType 'DWord' -Force | Out-Null

Set-ItemProperty -Path 'HKLM:\SOFTWARE\Wow6432Node\Microsoft\.NetFramework\v4.0.30319' -Name 'SchUseStrongCrypto' -Value 1 -Type DWord -Force
Set-ItemProperty -Path 'HKLM:\SOFTWARE\Microsoft\.NetFramework\v4.0.30319'             -Name 'SchUseStrongCrypto' -Value 1 -Type DWord -Force

[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$allUsersAllHosts = "$env:windir\System32\WindowsPowerShell\v1.0\profile.ps1"
New-Item -Path $allUsersAllHosts -ItemType File -Force | Out-Null
Set-Content -Path $allUsersAllHosts -Value "[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12" -Force

# Reassert local Administrator password (single source-of-truth from lab config).
net user Administrator '${admin_password}' /expires:never /y

# Provision the ansible service account used by the provisioner.
net user ansible '${admin_password}' /add /expires:never /y
net localgroup administrators ansible /add

# Enable WinRM for the in-VNet Ansible controller. The default Windows Firewall
# rule scopes 5985 to LocalSubnet only; the controller (10.8.3.0/28) lives in a
# different subnet from the GOAD hosts (10.8.1.0/24) so we widen to RemoteAddress=Any
# and rely on the private NSG (10.8.0.0/16 only) for network ACL.
Set-Service -Name WinRM -StartupType Automatic
Start-Service -Name WinRM
& winrm quickconfig -quiet -force | Out-Null

# pywinrm/pypsrp default to NTLM transport which still encrypts payload at the
# SPNEGO layer; AllowUnencrypted just removes the listener-level reject.
& winrm set winrm/config/service '@{AllowUnencrypted="true"}' | Out-Null
& winrm set winrm/config/service/auth '@{Basic="true"}' | Out-Null
& winrm set winrm/config/service/auth '@{Negotiate="true"}' | Out-Null

$ruleName = 'WinRM-HTTP-Any'
Get-NetFirewallRule -Name $ruleName -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue
New-NetFirewallRule `
    -Name $ruleName `
    -DisplayName 'WinRM HTTP from any (NSG-gated)' `
    -Direction Inbound -Action Allow `
    -Protocol TCP -LocalPort 5985 `
    -Profile Any -RemoteAddress Any | Out-Null

Write-Output "DreadGOAD Azure bootstrap complete on $env:COMPUTERNAME"
