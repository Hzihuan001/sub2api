[CmdletBinding()]
param(
  [switch]$Execute,
  [ValidateRange(1,2)][int]$Rounds = 2,
  [string]$BaselineBackend = 'D:/projects/go-tmp/cancel-accounting-baseline-989594099-46dd295b/backend',
  [string]$Go = 'C:/Users/a/AppData/Local/Temp/codex-go1.27.2-runtime/go/bin/go.exe',
  [string]$OutputDirectory = 'D:/projects/go-tmp/modular-microbenchmark-20261015'
)
$ErrorActionPreference='Stop'
if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 required' }
$source=(Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$candidate=Join-Path $source 'backend'
$benchmark=(Resolve-Path (Join-Path $PSScriptRoot 'modular-perf-benchmark_test.go')).Path
$control=Join-Path $candidate 'internal/service/custom_modularization_benchmark_test.go'
function Get-SourceFingerprint([string]$Backend) {
 $files=@(Get-ChildItem -LiteralPath $Backend -Recurse -File -Force | Sort-Object FullName)
 if (-not $files.Count) { throw 'Empty source tree' }
 $lines=foreach($file in $files) { [IO.Path]::GetRelativePath($Backend,$file.FullName).Replace('\','/') + ':' + (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash }
 return [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes(($lines -join "`n")))).ToLowerInvariant()
}
function Assert-CandidateClean([string]$Repository) {
 & git -C $Repository diff --quiet HEAD -- backend
 if ($LASTEXITCODE -ne 0) { throw 'Candidate tracked backend differs from HEAD' }
 # Include ignored inputs too: an ignored Go file can still change compilation.
 $extraFiles=@(& git -C $Repository ls-files --others -- backend)
 if ($LASTEXITCODE -ne 0) { throw 'Candidate input listing failed' }
 $untracked=@($extraFiles | Where-Object { $_ -match '(\.go|/go\.(mod|sum|work))$' })
 if ($untracked.Count) { throw 'Candidate has untracked Go/module inputs' }
}
function Assert-BaselineTree([string]$Repository,[string]$Backend,[string]$Commit) {
 $tree=@(& git -C $Repository ls-tree -r $Commit -- backend)
 if ($LASTEXITCODE -ne 0) { throw 'Baseline Git tree unavailable' }
 # Benchmark compilation uses Go/module inputs, not Dockerfile/frontend assets.
 $entries=@($tree | Where-Object { $_ -match '(\.go|/go\.(mod|sum))$' })
 if (-not $entries.Count) { throw 'Baseline source list empty' }
 $expected=@(); $paths=@()
 foreach($entry in $entries) {
  if ($entry -notmatch '^\d+ blob ([0-9a-f]{40})\tbackend/(.+)$') { throw 'Unsupported baseline tree entry' }
  $expected+=$Matches[1]; $paths+=Join-Path $Backend $Matches[2]
 }
 foreach($path in $paths) { if(-not(Test-Path -LiteralPath $path -PathType Leaf)){throw "Baseline file missing: $path"} }
 # Git canonicalization permits the export's CRLF checkout without changing it.
 # Full raw-byte fingerprints still guard changes during a measurement.
 $actual=@($paths | & git -C $Repository hash-object --stdin-paths)
 if ($LASTEXITCODE -ne 0 -or $actual.Count -ne $expected.Count) { throw 'Baseline hash collection failed' }
 for($i=0;$i -lt $actual.Count;$i++){if($actual[$i] -ne $expected[$i]){throw "Baseline blob mismatch: $($paths[$i])"}}
 if (@(Get-ChildItem -LiteralPath $Backend -Recurse -File -Force | Where-Object { $_.Name -match '(\.go|^go\.(mod|sum))$' }).Count -ne $expected.Count) { throw 'Baseline has extra Go/module inputs' }
}
if (-not $Execute) { Write-Output "Read-only plan: same Go1.27.2, identical read-only overlays, $Rounds baseline/candidate rounds, count5/100ms. No runtime edits."; return }
if ((& $Go version) -notmatch 'go1.27.2') { throw 'Pinned Go version required' }
$hashes=@{
 'gateway_failure_policy.go'='6ba8c049685bd5d4a4995d46439d39d1fbc391f8b3b5e151b34b80c006a5a309'
 'openai_gateway_passthrough.go'='1c7458480660fbcf60652684cb111b244348587ffdd1a64fd636fb43d8024269'
 'openai_gateway_upstream_errors.go'='02a91e8385a8cffa8dc7bce889fc947168087dc7bf450e1d60ae13134c8c87ec'
}
foreach ($file in $hashes.Keys) {
 if ((Get-FileHash -Algorithm SHA256 (Join-Path $BaselineBackend "internal/service/$file")).Hash.ToLowerInvariant() -ne $hashes[$file]) { throw "Baseline changed: $file" }
}
if ((& git -C $source hash-object $control).Trim() -ne '3f85a17acf7adba0bdcfc573dd72d28a42d66e2f') { throw 'P0 control fixture changed' }
Assert-CandidateClean $source
$candidateCommit=(& git -C $source rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $candidateCommit -notmatch '^[0-9a-f]{40}$') { throw 'Candidate commit unavailable' }
Assert-BaselineTree $source $BaselineBackend '989594099c1bd10db911c737449d15d2bb6149ed'
$sourcesBefore=@{baseline=Get-SourceFingerprint $BaselineBackend;candidate=Get-SourceFingerprint $candidate;fixture=(Get-FileHash $benchmark -Algorithm SHA256).Hash;control=(Get-FileHash $control -Algorithm SHA256).Hash}
if ((Test-Path $OutputDirectory) -and @(Get-ChildItem -LiteralPath $OutputDirectory -Force).Count) { throw 'Use a new evidence directory; never overwrite a prior measurement' }
$null=New-Item -ItemType Directory -Path $OutputDirectory -Force
$env:GOTOOLCHAIN='local';$env:GOWORK='off';$env:GOCACHE='D:/projects/go-cache';$env:GOTMPDIR='D:/projects/go-tmp';$env:GOMAXPROCS='2';$env:GOPROXY='off';$env:GOSUMDB='off'
foreach ($round in 1..$Rounds) {
 foreach ($variant in @('baseline','candidate')) {
  $backend=if ($variant -eq 'baseline') {$BaselineBackend} else {$candidate}
  $replace=@{}
  $replace[(Join-Path $backend 'internal/service/modular_perf_overlay_test.go')]=$benchmark
  if ($variant -eq 'baseline') { $replace[(Join-Path $backend 'internal/service/custom_modularization_benchmark_test.go')]=$control }
  $overlay=Join-Path $OutputDirectory "$variant-overlay.json"
  @{Replace=$replace} | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $overlay -Encoding utf8
  Push-Location $backend
  try {
   & $Go test "-overlay=$overlay" -tags=unit ./internal/service -run '^$' -bench '^(BenchmarkCustomFailureClassification|BenchmarkModularHostFailureWrappers)$' -benchmem -count=5 -benchtime=100ms -timeout=10m 2>&1 | Tee-Object -FilePath (Join-Path $OutputDirectory "$variant-round$round.log")
   if ($LASTEXITCODE -ne 0) { throw "Benchmark failed: $variant round$round" }
  } finally { Pop-Location }
 }
}
Assert-CandidateClean $source
$commitAfter=(& git -C $source rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $commitAfter -ne $candidateCommit) { throw 'Candidate HEAD changed during benchmark' }
$sourcesAfter=@{baseline=Get-SourceFingerprint $BaselineBackend;candidate=Get-SourceFingerprint $candidate;fixture=(Get-FileHash $benchmark -Algorithm SHA256).Hash;control=(Get-FileHash $control -Algorithm SHA256).Hash}
foreach($name in $sourcesBefore.Keys){if($sourcesBefore[$name] -ne $sourcesAfter[$name]){throw "Source input changed during benchmark: $name"}}
@{baseline='989594099c1bd10db911c737449d15d2bb6149ed';candidate=$candidateCommit;go=(& $Go version);controlBlob=(& git -C $source hash-object $control).Trim();hostWrapperFixtureSHA256=(Get-FileHash $benchmark -Algorithm SHA256).Hash;sourcesBefore=$sourcesBefore;sourcesAfter=$sourcesAfter;note='Control classifier unchanged. Host wrapper benchmarks include HTTP/passthrough/stream/replay decision paths; no I/O. >20% median regression or added allocations must be investigated, not silently accepted.'} | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $OutputDirectory 'provenance.json')
