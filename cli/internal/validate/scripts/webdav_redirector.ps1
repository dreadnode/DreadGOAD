# WebDAV-Redirector feature probe — reports whether the optional Windows
# feature is installed. Used to confirm the webdav role added the
# redirector that enables HTTP-auth coercion.
#
# Output:
#   { "found": bool, "state": string, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{ found = $false; state = ''; error = $null }

try {
    $f = Get-WindowsFeature -Name WebDAV-Redirector -ErrorAction SilentlyContinue
    if ($f) {
        $result.found = $true
        $result.state = "$($f.InstallState)"
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
