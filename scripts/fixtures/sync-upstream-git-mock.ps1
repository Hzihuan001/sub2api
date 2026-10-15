param(
    [Parameter(Mandatory)][string]$SyncScript,
    [Parameter(Mandatory)][ValidateSet('apply', 'dry-run', 'advanced-ref', 'already-current', 'fetch-failure')][string]$Scenario
)

# Execute the real script with all external commands replaced by fail-closed
# functions. No Git operation, test suite, filesystem mutation or network call
# is performed. Unknown command shapes fail the test instead of reaching Git.
$ErrorActionPreference = 'Stop'
$global:SyncTestCalls = [System.Collections.Generic.List[object]]::new()
$global:SyncTestOldBase = '1111111111111111111111111111111111111111'
$global:SyncTestFreshBase = '2222222222222222222222222222222222222222'
$global:SyncTestLaterBase = '3333333333333333333333333333333333333333'
$global:SyncTestTag = if ($Scenario -eq 'already-current') { $global:SyncTestFreshBase } else { '4444444444444444444444444444444444444444' }
$global:SyncTestOriginBase = $global:SyncTestOldBase
$global:SyncTestRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$global:SyncTestScenario = $Scenario

function git {
    $values = @($args | ForEach-Object { [string]$_ })
    $global:SyncTestCalls.Add($values)
    $global:LASTEXITCODE = 0
    $command = $values -join ' '
    switch -Regex ($command) {
        '^rev-parse --show-toplevel$' { return $global:SyncTestRoot }
        '^fetch --prune upstream \+refs/tags/v\*:refs/tags/v\*$' { return }
        '^fetch origin custom/integration$' {
            if ($global:SyncTestScenario -eq 'fetch-failure') { $global:LASTEXITCODE = 1; return }
            $global:SyncTestOriginBase = $global:SyncTestFreshBase
            return
        }
        '^rev-parse origin/custom/integration$' { return $global:SyncTestOriginBase }
        '^rev-parse refs/tags/v9\.9\.9\^\{commit\}$' { return $global:SyncTestTag }
        '^merge-base --is-ancestor ' { $global:LASTEXITCODE = 1; return }
        '^status --porcelain$' { return }
        '^branch --list upgrade/offline-test$' {
            if ($global:SyncTestScenario -eq 'advanced-ref') { $global:SyncTestOriginBase = $global:SyncTestLaterBase }
            return
        }
        '^switch --create upgrade/offline-test ' { return }
        '^merge --no-ff --no-commit refs/tags/v9\.9\.9$' { return }
        '^commit --no-edit -m chore: merge upstream v9\.9\.9$' { return }
        '^range-diff ' { return }
        '^diff --check ' { return }
        default { throw "Unmocked Git command (never executed): $command" }
    }
}

function Test-Path {
    param([string]$LiteralPath)
    # Exercise the isolation check's base argument, but never run Go/pnpm.
    return $LiteralPath.Replace('\', '/') -like '*scripts/check-custom-isolation.py'
}

function Get-Command {
    param([string]$Name, [string]$ErrorAction)
    if ($Name -ne 'python') { throw "Unmocked executable lookup: $Name" }
    return [pscustomobject]@{ Source = 'Invoke-MockPython' }
}

function Invoke-MockPython {
    $global:SyncTestCalls.Add(@('python') + @($args | ForEach-Object { [string]$_ }))
    $global:LASTEXITCODE = 0
}

$parameters = @{
    Manifest = 'unused-offline-manifest.yml'
    UpstreamTag = 'v9.9.9'
    BaseBranch = 'custom/integration'
    UpgradeBranch = 'upgrade/offline-test'
    RunChecks = $true
}
if ($Scenario -ne 'dry-run') { $parameters.Apply = $true }
$failure = $null
$output = @()
try { $output = @(& $SyncScript @parameters 6>&1 | ForEach-Object { [string]$_ }) }
catch { $failure = $_.Exception.Message }
[pscustomobject]@{ scenario = $Scenario; calls = $global:SyncTestCalls.ToArray(); output = $output; error = $failure } | ConvertTo-Json -Depth 8 -Compress
