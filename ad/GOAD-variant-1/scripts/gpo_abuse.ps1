Install-WindowsFeature -Name GPMC
$gpo_exist = Get-GPO -Name "OperationsWallpaper" -erroraction ignore

if ($gpo_exist) {
    # Do nothing
    #Remove-GPO -Name "OperationsWallpaper"
    #Remove the link of the GPO Remove-OperationsWallpaper if it exists
    #Remove-GPLink -Name "OperationsWallpaper" -Target "DC=hq,DC=deltasystems,DC=local" -erroraction 'silentlycontinue'
} else {
    New-GPO -Name "OperationsWallpaper" -comment "Change Wallpaper"
    New-GPLink -Name "OperationsWallpaper" -Target "DC=hq,DC=deltasystems,DC=local"

    #https://www.thewindowsclub.com/set-desktop-wallpaper-using-group-policy-and-registry-editor
    Set-GPRegistryValue -Name "OperationsWallpaper" -key "HKEY_CURRENT_USER\Control Panel\Colors" -ValueName Background -Type String -Value "100 175 200"
    #Set-GPPrefRegistryValue -Name "OperationsWallpaper" -Context User -Action Create -Key "HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Policies\System" -ValueName Wallpaper -Type String -Value "C:\tmp\GOAD.png"

    Set-GPRegistryValue -Name "OperationsWallpaper" -key "HKEY_CURRENT_USER\Control Panel\Desktop" -ValueName Wallpaper -Type String -Value ""
    #Set-GPPrefRegistryValue -Name "OperationsWallpaper" -Context User -Action Create -Key "HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Policies\System" -ValueName WallpaperStyle -Type String -Value "4"

    Set-GPRegistryValue -Name "OperationsWallpaper" -Key "HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows NT\CurrentVersion\WinLogon" -ValueName SyncForegroundPolicy -Type DWORD -Value 1

    # Allow stephanie.williams to Edit Settings of the GPO
    # https://learn.microsoft.com/en-us/powershell/module/grouppolicy/set-gppermission?view=windowsserver2022-ps
    Set-GPPermissions -Name "OperationsWallpaper" -PermissionLevel GpoEditDeleteModifySecurity -TargetName "stephanie.williams" -TargetType "User"
}
