# Troubleshooting

## Uninstalling SQL Server Express

If you're encountering issues related to SQL Server Express components
(particularly during `servers.yml` playbook execution), follow these steps to
completely uninstall SQL Server components from your lab environment.

### Complete Uninstallation Steps

Run the following commands in a PowerShell session on the relevant Windows
hosts to fully remove SQL Server Express:

```powershell
# Get all installed SQL Server components
$SQLComponents = Get-WmiObject -Query "SELECT * FROM Win32_Product WHERE Name LIKE '%SQL Server%'"
foreach ($Component in $SQLComponents) {
    Write-Host "Uninstalling: $($Component.Name) - $($Component.IdentifyingNumber)"
    Start-Process -FilePath "msiexec.exe" -ArgumentList "/x $($Component.IdentifyingNumber) /quiet /norestart" -Wait
}

# Stop SQL services before cleanup
Stop-Service -Name MSSQLSERVER, SQLSERVERAGENT, SQLWriter, SQLBrowser -Force -ErrorAction SilentlyContinue

# Take ownership and set permissions for cleanup
takeown /F "C:\Program Files\Microsoft SQL Server\*" /R /A /D Y
icacls "C:\Program Files\Microsoft SQL Server\*" /grant administrators:F /T

# Remove directories and registry keys
Remove-Item -Path "C:\setup" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "C:\ProgramData\Microsoft\SQL Server" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "C:\Program Files\Microsoft SQL Server" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server" -Recurse -Force -ErrorAction SilentlyContinue
Write-Host "SQL Server components uninstallation complete."
```

### Verification

Confirm successful uninstallation by running:

```powershell
Get-WmiObject -Query "SELECT * FROM Win32_Product WHERE Name LIKE '%SQL Server%'"
```

The above query should return no results if SQL Server has been fully removed.
