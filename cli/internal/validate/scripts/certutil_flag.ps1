# certutil -getreg flag probe — reports whether a named flag string appears
# in the output of `certutil -getreg <Key>`. Used by checks where presence
# (or absence) of a flag is the post-condition we're verifying.
#
# Inputs:
#   Key  — registry sub-path, e.g. 'policy\EditFlags' or 'CA\InterfaceFlags'
#   Flag — flag identifier whose presence we're testing for
# Output:
#   { "present": bool, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$Key  = {{psq .Key}}
$Flag = {{psq .Flag}}

$result = [ordered]@{ present = $false; error = $null }

try {
    $output = certutil -getreg $Key 2>&1 | Out-String
    $result.present = $output.Contains($Flag)
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
