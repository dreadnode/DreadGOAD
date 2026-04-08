# https://www.thehacker.recipes/ad/movement/kerberos/delegations/constrained#without-protocol-transition
Set-ADComputer -Identity "summit$" -ServicePrincipalNames @{Add='HTTP/beacon.hq.deltasystems.local'}
Set-ADComputer -Identity "summit$" -Add @{'msDS-AllowedToDelegateTo'=@('HTTP/beacon.hq.deltasystems.local','HTTP/beacon')}
# Set-ADComputer -Identity "summit$" -Add @{'msDS-AllowedToDelegateTo'=@('CIFS/beacon.hq.deltasystems.local','CIFS/beacon')}
