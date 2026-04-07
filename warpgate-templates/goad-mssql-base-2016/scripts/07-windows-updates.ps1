$ProgressPreference = 'SilentlyContinue'
$ErrorActionPreference = 'Stop'

Write-Host "Starting Windows Update service..."
Set-Service -Name wuauserv -StartupType Automatic
Start-Service -Name wuauserv

Write-Host "Installing PSWindowsUpdate module..."
Install-Module -Name PSWindowsUpdate -Force -Confirm:$false

Write-Host "Checking for Windows Updates..."
Import-Module PSWindowsUpdate

Write-Host "Installing Windows Updates (this may take 15-30 minutes)..."
$updates = Get-WindowsUpdate -AcceptAll -Install -AutoReboot:$false -IgnoreReboot

if ($updates) {
    Write-Host "Installed $($updates.Count) updates"
} else {
    Write-Host "No updates available"
}

Write-Host "Windows Updates complete"
