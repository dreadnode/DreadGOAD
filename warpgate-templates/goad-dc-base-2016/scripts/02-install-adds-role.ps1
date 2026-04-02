$ProgressPreference = 'SilentlyContinue'
$ErrorActionPreference = 'Stop'

Write-Host "Installing AD Domain Services role..."
Install-WindowsFeature -Name AD-Domain-Services -IncludeManagementTools

Write-Host "Installing DNS Server role..."
Install-WindowsFeature -Name DNS -IncludeManagementTools

Write-Host "Installing RSAT tools..."
Install-WindowsFeature -Name RSAT-AD-Tools, RSAT-DNS-Server, RSAT-ADDS

Write-Host "Installing Group Policy Management..."
Install-WindowsFeature -Name GPMC

Write-Host "AD DS role installation complete"
