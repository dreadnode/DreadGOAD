# SYSVOL plaintext credentials probe — scans C:\Windows\SYSVOL for files
# matching common credential markers (password, pwd, secret, cpassword).
# Used to confirm vulns_directory / vulns_files staged plaintext content
# under SYSVOL on a DC.
#
# Output:
#   { "root_present": bool, "file_count": int, "files": [string],
#     "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{
    root_present = $false
    file_count   = 0
    files        = @()
    error        = $null
}

try {
    $root = 'C:\Windows\SYSVOL'
    if (Test-Path $root) {
        $result.root_present = $true
        $hits = Get-ChildItem -Path $root -Recurse -File -ErrorAction SilentlyContinue |
            Select-String -Pattern 'password|pwd|secret|cpassword' -ErrorAction SilentlyContinue
        if ($hits) {
            $files = $hits | Group-Object Path | ForEach-Object { $_.Name }
            $result.files      = @($files)
            $result.file_count = @($files).Count
        }
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
