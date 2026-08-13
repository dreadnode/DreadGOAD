$task = '/c powershell New-PSDrive -Name "Public" -PSProvider "FileSystem" -Root "\\Bravos\private"'
$repeat = (New-TimeSpan -Minutes 2)
$taskName = "responder_bot"
$user = "north.sevenkingdoms.local\robb.stark"
$password = "{{ lab.domains[lab.hosts.dc02.domain].users['robb.stark'].password }}"

$action = New-ScheduledTaskAction -Execute "cmd.exe" -Argument "$task"
$trigger = New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval $repeat
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -RunOnlyIfNetworkAvailable -DontStopOnIdleEnd

Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -User $user -Password $password -Settings $settings -Force
