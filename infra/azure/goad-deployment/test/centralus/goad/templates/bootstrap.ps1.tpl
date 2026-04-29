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

Write-Output "DreadGOAD Azure bootstrap complete on $env:COMPUTERNAME"
