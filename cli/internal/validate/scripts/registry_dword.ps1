# Registry DWORD probe — reads a single DWORD value from a registry path.
#
# Inputs: Path, Name
# Output:
#   { "present": bool, "value": int, "error": string|null }
# `value` is undefined when `present` is false.

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$Path = {{psq .Path}}
$Name = {{psq .Name}}

$result = [ordered]@{ present = $false; value = 0; error = $null }

try {
    $p = Get-ItemProperty -Path $Path -Name $Name -ErrorAction SilentlyContinue
    if ($null -ne $p -and $null -ne $p.$Name) {
        $result.present = $true
        $result.value   = [int]$p.$Name
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
