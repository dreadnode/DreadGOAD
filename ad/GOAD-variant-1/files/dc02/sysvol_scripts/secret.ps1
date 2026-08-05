# cypher script
# $domain="deltasystems.local"
# $EncryptionKeyBytes = New-Object Byte[] 32
# [Security.Cryptography.RNGCryptoServiceProvider]::Create().GetBytes($EncryptionKeyBytes)
# $EncryptionKeyBytes | Out-File "encryption.key"
# $EncryptionKeyData = Get-Content "encryption.key"
# Read-Host -AsSecureString | ConvertFrom-SecureString -Key $EncryptionKeyData | Out-File -FilePath "secret.encrypted"

# secret stored :
$keyData = 177, 252, 228, 64, 28, 91, 12, 201, 20, 91, 21, 139, 255, 65, 9, 247, 41, 55, 164, 28, 75, 132, 143, 71, 62, 191, 211, 61, 154, 61, 216, 91
$secret="76492d1116743f0423413b16050a5345MgB8ADIAWQBrAHkAUABYAHMAMABRAEgAWgA0AGMAZwB1ADgAVgBDAC8AMgBCAGcAPQA9AHwANgBjAGYAYgBlAGIANwBlADEAMQAyADUAMgBlADYAYQA1ADYAYwAyAGQAZgA4AGEAYgAwADgAMgAyAGEAYQAzAGMAZgA5ADQANwBjADIANgAzAGYAMQBkAGIAYQBiAGIAYQAyADgAZAA1AGEAOAAyADUAZAA4ADgAYgBhADAAYgA="

# B.J.
