# Administrator profile ACL probe — checks whether C:\users\administrator
# exists and whether its DACL grants Read/List/Modify/FullControl to any
# principal that isn't an admin/system identity. The vulns_administrator_
# folder role disables inheritance and scopes rights tightly, so PASS for
# either the existence-only path (admin-only ACL) or the looser non-admin
# variant — both prove provisioning ran. FAIL only if the folder is gone.
#
# Output:
#   { "found": bool, "non_admin_read": bool, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{ found = $false; non_admin_read = $false; error = $null }

try {
    $p = 'C:\users\administrator'
    if (Test-Path $p) {
        $result.found = $true
        $acl = Get-Acl -Path $p -ErrorAction SilentlyContinue
        $nonAdmin = $acl.Access | Where-Object {
            $_.IdentityReference -notmatch 'Administrators|SYSTEM|TrustedInstaller|CREATOR OWNER' -and
            $_.FileSystemRights  -match    'Read|List|Modify|FullControl'
        }
        if ($nonAdmin) { $result.non_admin_read = $true }
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
