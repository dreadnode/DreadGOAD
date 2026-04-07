$ProgressPreference = 'SilentlyContinue'
$ErrorActionPreference = 'Stop'

$sqlInstanceName = "SQLEXPRESS"

# Create configuration file
$configContent = @"
[OPTIONS]
ACTION="Install"
FEATURES=SQLENGINE
INSTANCENAME="$sqlInstanceName"
INSTANCEID="$sqlInstanceName"
SQLSVCACCOUNT="NT AUTHORITY\NETWORK SERVICE"
SQLSYSADMINACCOUNTS="BUILTIN\Administrators" "NT AUTHORITY\NETWORK SERVICE"
AGTSVCSTARTUPTYPE="Automatic"
SQLSVCSTARTUPTYPE="Automatic"
BROWSERSVCSTARTUPTYPE="Automatic"
SECURITYMODE="SQL"
SAPWD="TempSaPassword123!"
TCPENABLED="1"
NPENABLED="1"
IACCEPTSQLSERVERLICENSETERMS="True"
QUIET="True"
QUIETSIMPLE="False"
UpdateEnabled="False"
ERRORREPORTING="False"
SQMREPORTING="False"
"@

Write-Host "Creating SQL Server configuration file..."
$configContent | Out-File -FilePath "C:\setup\mssql\sql_conf.ini" -Encoding ASCII

Write-Host "Installing SQL Server Express 2019 (this may take 15-25 minutes)..."

$process = Start-Process -FilePath "C:\setup\mssql\extraction\SETUP.EXE" `
    -ArgumentList "/ConfigurationFile=C:\setup\mssql\sql_conf.ini" `
    -Wait -NoNewWindow -PassThru

if ($process.ExitCode -eq 0 -or $process.ExitCode -eq 3010) {
    Write-Host "SQL Server Express installation completed successfully"
} else {
    # Check if SQL is actually installed despite exit code
    $sqlService = Get-Service -Name "MSSQL`$SQLEXPRESS" -ErrorAction SilentlyContinue
    if ($sqlService) {
        Write-Host "SQL Server Express installation completed (service exists)"
    } else {
        Write-Error "SQL Server installation failed with exit code: $($process.ExitCode)"
    }
}
