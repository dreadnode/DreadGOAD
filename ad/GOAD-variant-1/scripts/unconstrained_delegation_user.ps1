Get-ADUser -Identity "ryan.myers" | Set-ADAccountControl -TrustedForDelegation $true
