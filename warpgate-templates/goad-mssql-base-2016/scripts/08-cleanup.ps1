Write-Host "Cleaning up for AMI creation..."

# Clear Windows Update download cache
Stop-Service -Name wuauserv -Force -ErrorAction SilentlyContinue
Remove-Item -Path "C:\Windows\SoftwareDistribution\Download\*" -Recurse -Force -ErrorAction SilentlyContinue
Start-Service -Name wuauserv

# Keep SQL installer files (in case reinstall needed)
# Remove-Item -Path "C:\setup" -Recurse -Force -ErrorAction SilentlyContinue

# Clear temp files
Remove-Item -Path "$env:TEMP\*" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "C:\Windows\Temp\*" -Recurse -Force -ErrorAction SilentlyContinue

# Clear event logs
wevtutil cl Application
wevtutil cl Security
wevtutil cl System

Write-Host "Cleanup complete"
