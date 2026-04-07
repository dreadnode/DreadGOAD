$ProgressPreference = 'SilentlyContinue'

Write-Host "Configuring SQL Server TCP port..."

# Set TCP port to 1433
$regPath = "HKLM:\Software\Microsoft\Microsoft SQL Server\MSSQL15.SQLEXPRESS\MSSQLServer\SuperSocketNetLib\Tcp\IPAll"
if (Test-Path $regPath) {
    Set-ItemProperty -Path $regPath -Name "TcpPort" -Value "1433"
    Set-ItemProperty -Path $regPath -Name "TcpDynamicPorts" -Value ""
    Write-Host "TCP port configured to 1433"
} else {
    Write-Host "Registry path not found - SQL Server may need a restart"
}

Write-Host "Configuring firewall rules for SQL Server..."

# Allow SQL Server through firewall
New-NetFirewallRule -DisplayName "MSSQL TCP 1433" -Direction Inbound -Protocol TCP -LocalPort 1433 -Action Allow -Profile Domain -ErrorAction SilentlyContinue
New-NetFirewallRule -DisplayName "MSSQL UDP 1434" -Direction Inbound -Protocol UDP -LocalPort 1434 -Action Allow -Profile Domain -ErrorAction SilentlyContinue

Write-Host "Firewall rules configured"

Write-Host "Verifying SQL Server installation..."

$sqlService = Get-Service -Name "MSSQL`$SQLEXPRESS" -ErrorAction SilentlyContinue
if ($sqlService) {
    Write-Host "SQL Server service found: $($sqlService.Status)"

    # Ensure service is running
    if ($sqlService.Status -ne 'Running') {
        Start-Service -Name "MSSQL`$SQLEXPRESS"
        Write-Host "SQL Server service started"
    }
} else {
    Write-Error "SQL Server service not found!"
}

# Configure SQL Browser service
$browserService = Get-Service -Name "SQLBrowser" -ErrorAction SilentlyContinue
if ($browserService) {
    Set-Service -Name "SQLBrowser" -StartupType Automatic
    if ($browserService.Status -ne 'Running') {
        Start-Service -Name "SQLBrowser"
    }
    Write-Host "SQL Browser service running"
}

Write-Host "SQL Server configuration complete"
