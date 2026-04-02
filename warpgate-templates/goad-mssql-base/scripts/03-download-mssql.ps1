$ProgressPreference = 'SilentlyContinue'
$ErrorActionPreference = 'Stop'

$downloadUrl = "https://download.microsoft.com/download/7/f/8/7f8a9c43-8c8a-4f7c-9f92-83c18d96b681/SQL2019-SSEI-Expr.exe"

Write-Host "Creating installation directories..."
New-Item -Path "C:\setup\mssql\media" -ItemType Directory -Force | Out-Null
New-Item -Path "C:\setup\mssql\extraction" -ItemType Directory -Force | Out-Null

Write-Host "Downloading SQL Server Express 2019 installer..."
Invoke-WebRequest -Uri $downloadUrl -OutFile "C:\setup\mssql\sql_installer.exe" -UseBasicParsing

Write-Host "Downloading SQL Server installation media (this may take 5-10 minutes)..."
Start-Process -FilePath "C:\setup\mssql\sql_installer.exe" -ArgumentList "/ACTION=Download", "/MEDIAPATH=C:\setup\mssql\media", "/Q" -Wait -NoNewWindow

Write-Host "Extracting SQL Server installation files..."
Start-Process -FilePath "C:\setup\mssql\media\SQLEXPR_x64_ENU.exe" -ArgumentList "/x:C:\setup\mssql\extraction", "/q" -Wait -NoNewWindow

Write-Host "SQL Server media download complete"
