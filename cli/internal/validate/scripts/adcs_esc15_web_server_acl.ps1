# ADCS ESC15 probe — checks whether Domain Users have ExtendedRight on the
# Web Server cert template, which is the ACL grant that makes ESC15
# exploitable. Must run on a DC because ADWS is only reliable there.
#
# Output:
#   { "template_found": bool, "enroll_grant": bool, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{
    template_found = $false
    enroll_grant   = $false
    error          = $null
}

try {
    $sb = "CN=Certificate Templates,CN=Public Key Services,CN=Services," + `
          (Get-ADRootDSE).configurationNamingContext
    $t = Get-ADObject -Filter {displayName -eq 'Web Server' -and objectClass -eq 'pKICertificateTemplate'} `
                       -SearchBase $sb -Properties nTSecurityDescriptor -ErrorAction SilentlyContinue
    if ($t) {
        $result.template_found = $true
        Import-Module ActiveDirectory
        Set-Location AD:
        $acl = Get-Acl -Path $t.DistinguishedName
        $match = $acl.Access | Where-Object {
            $_.IdentityReference -like '*Domain Users*' -and
            $_.ActiveDirectoryRights -match 'ExtendedRight'
        }
        if ($match) { $result.enroll_grant = $true }
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
