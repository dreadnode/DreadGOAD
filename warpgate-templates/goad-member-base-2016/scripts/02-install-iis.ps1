$ProgressPreference = 'SilentlyContinue'
$ErrorActionPreference = 'Stop'

Write-Host "Installing IIS..."
Install-WindowsFeature -Name Web-Server -IncludeManagementTools -IncludeAllSubFeature

Write-Host "Installing WebDAV..."
Install-WindowsFeature -Name Web-DAV-Publishing

Write-Host "IIS installation complete"
