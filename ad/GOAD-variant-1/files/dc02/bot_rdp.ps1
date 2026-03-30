# https://learn.microsoft.com/fr-fr/troubleshoot/windows-server/user-profiles-and-logon/turn-on-automatic-logon
if(-not(query session catherine2.ramos /server:summit)) {
  #kill process if exist
  Get-Process mstsc -IncludeUserName | Where {$_.UserName -eq "HQ\catherine2.ramos"}|Stop-Process
  #run the command
  mstsc /v:summit
}
