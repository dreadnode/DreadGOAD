Set-ADUser -Identity "christine.martin" -ServicePrincipalNames @{Add='CIFS/thewall.hq.deltasystems.local'}
Get-ADUser -Identity "christine.martin" | Set-ADAccountControl -TrustedToAuthForDelegation $true
Set-ADUser -Identity "christine.martin" -Add @{'msDS-AllowedToDelegateTo'=@('CIFS/beacon.hq.deltasystems.local','CIFS/beacon')}
