# Share file count probe — reports how many files the vulns_files role
# staged under C:\shares. PASS when at least one file exists; the role
# drops fake credential dumps there for ATT&CK Discovery scenarios.
#
# Output:
#   { "root_present": bool, "file_count": int, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{
    root_present = $false
    file_count   = 0
    error        = $null
}

try {
    $root = 'C:\shares'
    if (Test-Path $root) {
        $result.root_present = $true
        $files = Get-ChildItem -Path $root -Recurse -File -ErrorAction SilentlyContinue
        $result.file_count = @($files).Count
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
