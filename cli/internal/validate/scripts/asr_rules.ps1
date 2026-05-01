# ASR rules probe — counts the AttackSurfaceReductionRules_Ids that
# Defender currently has configured. Used to confirm an ASR-tagged host
# actually has rules in place.
#
# Output:
#   { "rule_count": int, "error": string|null }

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

$result = [ordered]@{ rule_count = 0; error = $null }

try {
    $ids = (Get-MpPreference -ErrorAction SilentlyContinue).AttackSurfaceReductionRules_Ids
    if ($ids) {
        $result.rule_count = @($ids).Count
    }
} catch {
    $result.error = "$_"
}

Write-Output '===BEGIN_JSON==='
$result | ConvertTo-Json -Compress -Depth 5
Write-Output '===END_JSON==='
