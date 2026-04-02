$ProgressPreference = 'SilentlyContinue'

Write-Host "Configuring SSM Agent for post-DC-promotion operation..."

# SSM agent needs special configuration to survive DC promotion
# Create a scheduled task to restart SSM agent after DC promotion
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument '-Command "Start-Sleep -Seconds 60; Restart-Service AmazonSSMAgent"'
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable

Register-ScheduledTask -TaskName "RestartSSMAfterBoot" -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force

Write-Host "SSM Agent configuration complete"

# Ensure SSM agent is running now
$ssmService = Get-Service -Name "AmazonSSMAgent" -ErrorAction SilentlyContinue
if ($ssmService) {
    if ($ssmService.Status -ne 'Running') {
        Start-Service -Name "AmazonSSMAgent"
        Write-Host "SSM Agent started"
    } else {
        Write-Host "SSM Agent already running"
    }
} else {
    Write-Host "Warning: SSM Agent not found"
}
