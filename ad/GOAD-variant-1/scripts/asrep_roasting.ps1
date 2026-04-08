Get-ADUser -Identity "alexander.peterson" | Set-ADAccountControl -DoesNotRequirePreAuth:$true
