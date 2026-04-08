# Add registry keys to enable TLS 1.2 at the OS level
New-Item 'HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\TLS 1.2\Server' -Force | Out-Null
New-Item 'HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\TLS 1.2\Client' -Force | Out-Null
New-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\TLS 1.2\Server' -name 'Enabled' -value '1' -PropertyType 'DWord' -Force | Out-Null
New-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\TLS 1.2\Server' -name 'DisabledByDefault' -value 0 -PropertyType 'DWord' -Force | Out-Null
New-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\TLS 1.2\Client' -name 'Enabled' -value 1 -PropertyType 'DWord' -Force | Out-Null
New-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\SCHANNEL\Protocols\TLS 1.2\Client' -name 'DisabledByDefault' -value 0 -PropertyType 'DWord' -Force | Out-Null

# Enable strong cryptography on .NET Framework
Set-ItemProperty -Path 'HKLM:\SOFTWARE\Wow6432Node\Microsoft\.NetFramework\v4.0.30319' -Name 'SchUseStrongCrypto' -Value 1 -Type DWord -Force
Set-ItemProperty -Path 'HKLM:\SOFTWARE\Microsoft\.NetFramework\v4.0.30319' -Name 'SchUseStrongCrypto' -Value 1 -Type DWord -Force

# Force TLS 1.2 in current PowerShell session and create system-wide PowerShell profile
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

# Create AllSigned profile for all users
$allUsersAllHosts = "$env:windir\System32\WindowsPowerShell\v1.0\profile.ps1"
New-Item -Path $allUsersAllHosts -ItemType File -Force | Out-Null
Set-Content -Path $allUsersAllHosts -Value "[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12" -Force

# Set the local Administrator password
net user Administrator ${admin_password} /expires:never /y

# Create ansible user, add to administrators group, and set password
net user ansible ${admin_password} /add /expires:never /y
net localgroup administrators ansible /add

# Setup SSM Agent
$progressPreference = 'silentlyContinue'

# Use WebClient instead of Invoke-WebRequest for SSM agent download too
$ssmUrl = "https://amazon-ssm-${aws_region}.s3.amazonaws.com/latest/windows_amd64/AmazonSSMAgentSetup.exe"
$ssmOutput = "$env:TEMP\SSMAgent_latest.exe"
$webClient = New-Object System.Net.WebClient
$webClient.DownloadFile($ssmUrl, $ssmOutput)

# Install SSM agent
Start-Process -FilePath $env:TEMP\SSMAgent_latest.exe -ArgumentList "/S" -Wait
Remove-Item -Force $env:TEMP\SSMAgent_latest.exe
Restart-Service AmazonSSMAgent

# Rename computer and restart
Rename-Computer -NewName "${hostname}" -Force
Restart-Computer -Force
