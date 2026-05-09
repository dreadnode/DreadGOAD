# Share permissive ACL probe — scans known share roots populated by the
# vulns_permissions role (C:\shares, C:\inetpub\wwwroot\upload,
# C:\thewall) for ACEs that grant Modify/Write/FullControl to permissive
# principals (Everyone / Authenticated Users / IIS_IUSRS / Users). The
# role lays these ACLs down so non-privileged accounts can drop content.
#
# Output:
#   { "entries": [ { "path": string, "identity": string,
#                    "rights": string } ],
#     "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{ entries = @(); error = $null }

try {
    $paths   = @('C:\shares','C:\inetpub\wwwroot\upload','C:\thewall')
    $entries = @()
    foreach ($p in $paths) {
        if (-not (Test-Path $p)) { continue }
        $acl = Get-Acl -Path $p -ErrorAction SilentlyContinue
        foreach ($ace in $acl.Access) {
            if ($ace.IdentityReference -match 'Everyone|Authenticated Users|IIS_IUSRS|Users' -and
                $ace.FileSystemRights -match 'FullControl|Modify|Write') {
                $entries += [ordered]@{
                    path     = $p
                    identity = "$($ace.IdentityReference)"
                    rights   = "$($ace.FileSystemRights)"
                }
            }
        }
    }
    $result.entries = @($entries)
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
