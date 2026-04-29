# Scheduled task probe — reports whether a named task exists and its
# current State. PASS for any state (Ready, Running, Disabled) since
# we're verifying that provisioning created the task, not its runtime
# health.
#
# Inputs: TaskName
# Output:
#   { "found": bool, "state": string|null, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$TaskName = {{psq .TaskName}}

$result = [ordered]@{ found = $false; state = $null; error = $null }

try {
    $t = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($t) {
        $result.found = $true
        $result.state = "$($t.State)"
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
