# IIS upload folder ACL probe — checks whether the C:\inetpub\wwwroot\
# upload directory grants Modify/Write/FullControl to IIS_IUSRS, which is
# what the vulns_permissions role lays down so the IIS web app can drop
# user-supplied uploads onto disk.
#
# Output:
#   { "dir_present": bool, "ace_present": bool, "rights": string,
#     "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{
    dir_present = $false
    ace_present = $false
    rights      = ''
    error       = $null
}

try {
    $p = 'C:\inetpub\wwwroot\upload'
    if (Test-Path $p) {
        $result.dir_present = $true
        $acl = Get-Acl -Path $p -ErrorAction SilentlyContinue
        $ace = $acl.Access | Where-Object {
            $_.IdentityReference -match 'IIS_IUSRS' -and
            $_.FileSystemRights  -match 'FullControl|Modify|Write'
        } | Select-Object -First 1
        if ($ace) {
            $result.ace_present = $true
            $result.rights      = "$($ace.FileSystemRights)"
        }
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
