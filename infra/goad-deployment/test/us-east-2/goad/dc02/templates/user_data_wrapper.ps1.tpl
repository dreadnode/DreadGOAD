<powershell>
$EncodedUserData = "${compressed_user_data}"
$DecodedUserData = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($EncodedUserData))
Invoke-Expression $DecodedUserData
</powershell>
