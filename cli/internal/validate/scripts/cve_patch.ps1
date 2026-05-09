# CVE patch probe — reports the first installed mitigating KB (if any).
#
# Inputs:
#   KBs — list of KB IDs that mitigate the CVE.
#
# Output:
#   { "installed": "<KB id>" | null, "error": string|null }
# A null `installed` means none of the KBs are present, i.e. the host is
# still vulnerable (the desired post-condition for an intentionally-unpatched
# lab host).

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$KBs = {{psarr .KBs}}

$result = [ordered]@{ installed = $null; error = $null }
try {
    foreach ($kb in $KBs) {
        $h = Get-HotFix -Id $kb -ErrorAction SilentlyContinue
        if ($h) { $result.installed = $kb; break }
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
