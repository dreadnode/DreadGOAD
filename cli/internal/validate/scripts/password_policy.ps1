# Default Domain Password Policy probe — reports the three values the
# validator cares about (ComplexityEnabled, MinPasswordLength,
# LockoutThreshold) so the GOAD lab's intentionally weak password
# settings can be confirmed.
#
# Output:
#   { "found": bool, "complexity": bool, "min_length": int,
#     "lockout_threshold": int, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{
    found             = $false
    complexity        = $false
    min_length        = 0
    lockout_threshold = 0
    error             = $null
}

try {
    $p = Get-ADDefaultDomainPasswordPolicy -ErrorAction Stop
    if ($p) {
        $result.found             = $true
        $result.complexity        = [bool]$p.ComplexityEnabled
        $result.min_length        = [int]$p.MinPasswordLength
        $result.lockout_threshold = [int]$p.LockoutThreshold
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
