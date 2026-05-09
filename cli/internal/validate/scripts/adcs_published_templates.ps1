# ADCS published templates probe — enumerates the certificateTemplates
# multi-valued attribute on every pKIEnrollmentService object in the
# Configuration partition. Must run on a DC because ADWS is only reliable
# there.
#
# Output:
#   { "templates": [string, ...], "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{
    templates = @()
    error     = $null
}

try {
    $sb = "CN=Enrollment Services,CN=Public Key Services,CN=Services," + `
          (Get-ADRootDSE).configurationNamingContext
    $tmpls = Get-ADObject -Filter {objectClass -eq 'pKIEnrollmentService'} `
              -SearchBase $sb -Properties certificateTemplates |
              Select-Object -ExpandProperty certificateTemplates
    if ($tmpls) {
        $result.templates = @($tmpls |
            ForEach-Object { "$_".Trim() } |
            Where-Object   { $_ -ne '' })
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
