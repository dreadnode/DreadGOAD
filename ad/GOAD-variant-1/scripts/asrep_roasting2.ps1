Get-ADUser -Identity "susan.white" | Set-ADAccountControl -DoesNotRequirePreAuth:$true
