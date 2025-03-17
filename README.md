<div align="center">
  <h1><img alt="GOAD (Game Of Active Directory)" src="./docs/mkdocs/docs/img/logo_GOAD3.png"></h1>
  <br>
</div>

**GOAD (v3)**

:bookmark: Documentation : [https://orange-cyberdefense.github.io/GOAD/](https://orange-cyberdefense.github.io/GOAD/)

## Description

GOAD is a pentest active directory LAB project.
The purpose of this lab is to give pentesters a vulnerable Active directory environment ready to use to practice usual attack techniques.

> [!CAUTION]
> This lab is extremely vulnerable, do not reuse recipe to build your environment and do not deploy this environment on internet without isolation (this is a recommendation, use it as your own risk).<br>
> This repository was build for pentest practice.

![goad_screenshot](./docs/img/goad_screenshot.png)

## Licenses

This lab use free Windows VM only (180 days). After that delay enter a license on each server or rebuild all the lab (may be it's time for an update ;))

## Available labs

- GOAD Lab family and extensions overview
<div align="center">
<img alt="GOAD" width="800" src="./docs/img/diagram-GOADv3-full.png">
</div>

- [GOAD](https://orange-cyberdefense.github.io/GOAD/labs/GOAD/) : 5 vms, 2 forests, 3 domains (full goad lab)
<div align="center">
<img alt="GOAD" width="800" src="./docs/img/GOAD_schema.png">
</div>

- [GOAD-Light](https://orange-cyberdefense.github.io/GOAD/labs/GOAD-Light/) : 3 vms, 1 forest, 2 domains (smaller goad lab for those with a smaller pc)
<div align="center">
<img alt="GOAD Light" width="600" src="./docs/img/GOAD-Light_schema.png">
</div>

- [MINILAB](https://orange-cyberdefense.github.io/GOAD/labs/MINILAB/): 2 vms, 1 forest, 1 domain (basic lab with one DC (windows server 2019) and one Workstation (windows 10))

- [SCCM](https://orange-cyberdefense.github.io/GOAD/labs/SCCM/) : 4 vms, 1 forest, 1 domain, with microsoft configuration manager installed
<div align="center">
<img alt="SCCM" width="600" src="./docs/img/SCCMLAB_overview.png">
</div>

- [NHA](https://orange-cyberdefense.github.io/GOAD/labs/NHA/) : A challenge with 5 vms and 2 domains. no schema provided, you will have to find out how break it.

## Fetch upstream changes

```bash
# Fetch all the branches of the upstream repository
git fetch upstream

# Make sure you're on your main branch
git checkout main

# Rebase your main branch with the upstream main branch
git rebase upstream/main

# Push the changes to your private repository
git push origin main --force
```

## Uninstall SQLServer Express

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

Verify uninstallation:

```powershell
Get-WmiObject -Query "SELECT * FROM Win32_Product WHERE Name LIKE '%SQL Server%'"
```
