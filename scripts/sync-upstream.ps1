[CmdletBinding()]
param(
    [string]$Manifest = ".github/upstream-sync-manifest.yml",
    [string]$UpstreamTag,
    [string]$BaseBranch,
    [string]$UpgradeBranch,
    [switch]$Apply,
    [switch]$Push,
    [switch]$CreatePr,
    [switch]$RunChecks,
    [switch]$KeepWorktree
)

# Safe local companion for .github/workflows/upstream-sync.yml.
# Default mode only fetches metadata and prints a report. It never deploys.
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Invoke-Git {
    param([Parameter(Mandatory)][string[]]$Arguments)
    & git @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "git $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function Invoke-GitAllowFailure {
    param([Parameter(Mandatory)][string[]]$Arguments)
    & git @Arguments
    return $LASTEXITCODE
}

function Get-LatestUpstreamTag {
    $raw = git ls-remote --tags --refs upstream "v*"
    if ($LASTEXITCODE -ne 0) { throw "Unable to read upstream tags" }
    $tags = @(
        $raw |
            ForEach-Object { ($_ -split "`t", 2)[1] -replace '^refs/tags/', '' } |
            Where-Object { $_ -match '^v\d+\.\d+\.\d+(?:[-+].*)?$' }
    )
    if ($tags.Count -eq 0) { throw "No semantic v* tag was found on upstream" }
    return ($tags | Sort-Object { [version](($_ -replace '^v', '') -replace '[-+].*$', '') } -Descending | Select-Object -First 1)
}

function Read-ManifestValue {
    param([string]$Path, [string]$Name)
    # The workflow parses the same small YAML file with Ruby. For local use,
    # avoid requiring a PowerShell YAML module and use conservative defaults.
    if (-not (Test-Path -LiteralPath $Path)) { return $null }
    $line = Get-Content -LiteralPath $Path | Where-Object { $_ -match "^\s*${Name}:\s*(.+?)\s*$" } | Select-Object -First 1
    if ($null -eq $line) { return $null }
    return (($line -replace "^\s*${Name}:\s*", "").Trim("'").Trim('"'))
}

function Read-PatchBranches {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return @() }
    $inList = $false
    $items = [System.Collections.Generic.List[string]]::new()
    foreach ($line in (Get-Content -LiteralPath $Path)) {
        if ($line -match '^patch_branches:\s*$') { $inList = $true; continue }
        if ($inList -and $line -match '^[^\s-]') { $inList = $false }
        if ($inList -and $line -match '^\s+-\s+(.+?)\s*$') { $items.Add($Matches[1].Trim("'").Trim('"')) }
    }
    return $items.ToArray()
}

$repoRoot = (git rev-parse --show-toplevel).Trim()
if ($LASTEXITCODE -ne 0) { throw "Run this script from a Git working tree" }
Set-Location -LiteralPath $repoRoot

$manifestBase = Read-ManifestValue -Path $Manifest -Name "base_branch"
if ([string]::IsNullOrWhiteSpace($BaseBranch)) { $BaseBranch = if ($manifestBase) { $manifestBase } else { "main" } }

Write-Host "Fetching upstream tag metadata..."
Invoke-Git @("fetch", "--prune", "upstream", "+refs/tags/v*:refs/tags/v*")
if ([string]::IsNullOrWhiteSpace($UpstreamTag)) { $UpstreamTag = Get-LatestUpstreamTag }
if ($UpstreamTag -notmatch '^v\d+\.\d+\.\d+') { throw "Invalid upstream tag: $UpstreamTag" }

$safeTag = $UpstreamTag -replace '[^0-9A-Za-z._-]', '-'
if ([string]::IsNullOrWhiteSpace($UpgradeBranch)) { $UpgradeBranch = "upgrade/upstream-$safeTag" }
$baseRef = "origin/$BaseBranch"
$tagRef = "refs/tags/$UpstreamTag"

$baseSha = (git rev-parse $baseRef 2>$null).Trim()
$baseCode = $LASTEXITCODE
$tagSha = (git rev-parse $tagRef 2>$null).Trim()
$tagCode = $LASTEXITCODE
if ($baseCode -ne 0) { throw "Cannot resolve custom base $baseRef. Create the dedicated integration branch before running sync." }
if ($tagCode -ne 0) { throw "Cannot resolve upstream tag $tagRef" }

$report = [System.Collections.Generic.List[string]]::new()
$report.Add("# Upstream sync report")
$report.Add("")
$report.Add("- Base: ``$BaseBranch`` ($baseSha)")
$report.Add("- Upstream tag: ``$UpstreamTag`` ($tagSha)")
$report.Add("- Proposed branch: ``$UpgradeBranch``")
$report.Add("- Deployment: never performed by this script")
$report.Add("")

if ($baseSha -eq $tagSha) {
    $report.Add("No update: base already points at the selected upstream tag.")
    $report | Set-Content -LiteralPath (Join-Path $repoRoot "upstream-sync-report.md") -Encoding utf8
    $report | ForEach-Object { Write-Host $_ }
    exit 0
}

if (-not $Apply) {
    $report.Add("Dry run only. Re-run with -Apply to create the upgrade branch and merge.")
    $report | ForEach-Object { Write-Host $_ }
    exit 0
}

if (-not $KeepWorktree) {
    $status = git status --porcelain
    if ($status) { throw "Working tree is not clean. Commit or stash changes before -Apply.`n$status" }
}

Invoke-Git @("fetch", "origin", $BaseBranch)
$existing = git branch --list $UpgradeBranch
if ($existing) { throw "Upgrade branch already exists: $UpgradeBranch (choose another name)" }
Invoke-Git @("switch", "--create", $UpgradeBranch, $baseRef)

$mergeCode = Invoke-GitAllowFailure @("merge", "--no-ff", "--no-commit", $tagRef)
if ($mergeCode -ne 0) {
    $conflicts = @(git diff --name-only --diff-filter=U)
    $report.Add("## Conflicts")
    if ($conflicts.Count -eq 0) { $report.Add("Git reported a merge failure without conflict paths; inspect the command output.") }
    else { $conflicts | ForEach-Object { $report.Add("- ``$_``") } }
    $report.Add("")
    $report.Add("The merge was aborted. Resolve conflicts in a dedicated upgrade branch; this script does not auto-resolve semantic conflicts.")
    Invoke-Git @("merge", "--abort")
    $report | ForEach-Object { Write-Host $_ }
    if (-not $KeepWorktree) { Invoke-Git @("switch", $BaseBranch) }
    exit 2
}

foreach ($patchBranch in (Read-PatchBranches -Path $Manifest)) {
    if ([string]::IsNullOrWhiteSpace($patchBranch)) { continue }
    $patchCode = Invoke-GitAllowFailure @("fetch", "--prune", "origin", $patchBranch)
    if ($patchCode -ne 0) {
        $report.Add("Patch branch was not found on origin: ``$patchBranch``")
        $report | ForEach-Object { Write-Host $_ }
        Invoke-Git @("merge", "--abort")
        if (-not $KeepWorktree) { Invoke-Git @("switch", $BaseBranch) }
        exit 2
    }
    $patchCode = Invoke-GitAllowFailure @("merge", "--no-ff", "--no-commit", "origin/$patchBranch")
    if ($patchCode -ne 0) {
        $conflicts = @(git diff --name-only --diff-filter=U)
        $report.Add("Patch branch conflicted: ``$patchBranch``")
        $conflicts | ForEach-Object { $report.Add("- ``$_``") }
        Invoke-Git @("merge", "--abort")
        $report | ForEach-Object { Write-Host $_ }
        if (-not $KeepWorktree) { Invoke-Git @("switch", $BaseBranch) }
        exit 2
    }
}

# A no-commit merge leaves the index ready for a deterministic merge commit.
Invoke-Git @("commit", "--no-edit", "-m", "chore: merge upstream $UpstreamTag")
$rangeCode = Invoke-GitAllowFailure @("range-diff", "$tagRef..$baseSha", "$tagRef..HEAD")
$report.Add("## Merge")
$report.Add("Merge completed. ``git range-diff`` exit code: $rangeCode")

if ($RunChecks) {
    Write-Host "Running repository checks..."
    Invoke-Git @("diff", "--check", "$baseSha..HEAD")
    if (Test-Path -LiteralPath "backend/go.mod") {
        Push-Location backend
        try { & go test ./...; if ($LASTEXITCODE -ne 0) { throw "backend tests failed" } }
        finally { Pop-Location }
    }
    if (Test-Path -LiteralPath "frontend/package.json") {
        Push-Location frontend
        try { & pnpm install --frozen-lockfile; if ($LASTEXITCODE -ne 0) { throw "frontend install failed" } }
        finally { Pop-Location }
    }
    $report.Add("Checks completed successfully.")
}

$report | ForEach-Object { Write-Host $_ }
if ($Push) {
    Invoke-Git @("push", "--set-upstream", "origin", $UpgradeBranch)
    if ($CreatePr) {
        & gh pr create --draft --base $BaseBranch --head $UpgradeBranch --title "chore: sync upstream $UpstreamTag" --body ($report -join "`n")
        if ($LASTEXITCODE -ne 0) { throw "gh pr create failed" }
    }
}

Write-Host "Upstream sync finished. No deployment was performed."
