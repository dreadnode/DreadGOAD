$identity = 'jon.snow'
$spn = 'CIFS/thewall.north.sevenkingdoms.local'
$delegateTo = @('CIFS/winterfell.north.sevenkingdoms.local', 'CIFS/winterfell')

# Re-adding a value a multi-valued attribute already holds is an LDAP constraint
# violation, which fails the play on a lab reset, so only add what is missing.
# jon.snow already carries a kerberoastable SPN from ad-data: add, never replace.
$user = Get-ADUser -Identity $identity -Properties ServicePrincipalNames, 'msDS-AllowedToDelegateTo'

if ($user.ServicePrincipalNames -notcontains $spn) {
    Set-ADUser -Identity $identity -ServicePrincipalNames @{Add = $spn }
}

Set-ADAccountControl -Identity $identity -TrustedToAuthForDelegation $true

$missing = @($delegateTo | Where-Object { $user.'msDS-AllowedToDelegateTo' -notcontains $_ })
if ($missing.Count -gt 0) {
    Set-ADUser -Identity $identity -Add @{'msDS-AllowedToDelegateTo' = $missing }
}
