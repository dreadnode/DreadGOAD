$task = '/c powershell New-PSDrive -Name "Public" -PSProvider "FileSystem" -Root "\\Beacon-app\Private"'
$repeat = (New-TimeSpan -Minutes 5)
$taskName = "ntlm_bot"
$user = "hq.deltasystems.local\william.wood"
$password = "<Bs!d4UGlv[ll>*x-Iz"

$action = New-ScheduledTaskAction -Execute "cmd.exe" -Argument "$task"
$trigger = New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval $repeat
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -RunOnlyIfNetworkAvailable -DontStopOnIdleEnd

$taskExists = Get-ScheduledTask | Where-Object {$_.TaskName -like $taskName }
if($taskExists) {
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
}
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -User $user -Password $password -Settings $settings
