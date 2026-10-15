# Local contract tests only: no Docker, Go benchmark, model or network calls.
$ErrorActionPreference='Stop'
if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 required' }
foreach($file in @('modular-performance.ps1','modular-microbenchmark.ps1')) {
  $errors=$null;$tokens=$null
  $ast=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $file),[ref]$tokens,[ref]$errors)
  if($errors.Count){throw "Parse error: $file"}
  foreach($fn in $ast.FindAll({param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst]},$false)) {
    if($fn.Name -in @('Assert-SnapshotIdentity','State','Get-SourceFingerprint','Assert-CandidateClean','Assert-BaselineTree')) { . ([scriptblock]::Create($fn.Extent.Text)) }
  }
}
$checks=0
function Must-Fail([scriptblock]$Action){$threw=$false;try{& $Action}catch{$threw=$true};if(-not$threw){throw 'Expected rejection'};$script:checks++}
$container='a'*64;$image='sha256:'+('b'*64)
$valid=@{Id=$container;Image=$image;State=@{StartedAt='2026-10-15T01:00:00Z';Running=$true};RestartCount=0}
Assert-SnapshotIdentity $valid $container $image;$checks++
Must-Fail {Assert-SnapshotIdentity $null $container $image}
Must-Fail {Assert-SnapshotIdentity $valid '' $image}
Must-Fail {Assert-SnapshotIdentity $valid $container ('sha256:'+('c'*64))}
foreach($bad in @(
  @{Id=$container;Image=$image;State=@{StartedAt='bad';Running=$true};RestartCount=0},
  @{Id=$container;Image=$image;State=@{StartedAt='2026-10-15T01:00:00Z';Running=$true}},
  @{Id=$container;Image=$image;State=@{StartedAt='2026-10-15T01:00:00Z';Running=$false};RestartCount=0}
)){Must-Fail {Assert-SnapshotIdentity $bad $container $image}}

# Shadow the native command to prove inspect failures cannot reach exec.
$compose=@('compose');$inspectFails=$true;$execCount=0
function docker {
  $global:LASTEXITCODE=0
  switch($args[0]) {
    'compose' {return $script:container}
    'inspect' {if($script:inspectFails){$global:LASTEXITCODE=1;return};return ($script:valid|ConvertTo-Json -Depth 5)}
    'exec' {$script:execCount++;return @('usage_usec 100','VmRSS: 12 kB')}
    default {throw 'Unexpected mocked Docker command'}
  }
}
Must-Fail {State $image}
if($execCount -ne 0){throw 'Failed inspect reached exec'};$checks++
$inspectFails=$false;$state=State $image
if($state.cpuUsec -ne 100 -or $state.rssKiB -ne 12 -or $state.imageId -ne $image){throw 'Snapshot contract failed'};$checks++

# Disposable synthetic Git tree exercises dirty/changed/missing/extra guards.
$fixtureRoot=Join-Path ([IO.Path]::GetTempPath()) ('modular-perf-contract-'+[guid]::NewGuid().ToString('N'))
$repo=Join-Path $fixtureRoot 'repo';$export=Join-Path $fixtureRoot 'export'
$null=New-Item -ItemType Directory -Path (Join-Path $repo 'backend'),$export -Force
$content="package fixture`n"
[IO.File]::WriteAllText((Join-Path $repo 'backend/a.go'),$content)
[IO.File]::WriteAllText((Join-Path $export 'a.go'),$content)
& git -C $repo init -q
& git -C $repo -c core.autocrlf=false add backend
& git -C $repo -c user.name=local-contract -c user.email=contract@localhost commit -qm fixture
if($LASTEXITCODE -ne 0){throw 'Synthetic Git setup failed'}
$commit=(& git -C $repo rev-parse HEAD).Trim()
Assert-CandidateClean $repo;Assert-BaselineTree $repo $export $commit;$checks+=2
$before=Get-SourceFingerprint $export
[IO.File]::WriteAllText((Join-Path $export 'a.go'),$content+"// changed`n")
if((Get-SourceFingerprint $export) -eq $before){throw 'Changed content not detected'};$checks++
Must-Fail {Assert-BaselineTree $repo $export $commit}
[IO.File]::WriteAllText((Join-Path $export 'a.go'),$content)
[IO.File]::WriteAllText((Join-Path $export 'extra.go'),$content)
Must-Fail {Assert-BaselineTree $repo $export $commit}
[IO.File]::WriteAllText((Join-Path $repo 'backend/a.go'),$content+"// dirty`n")
Must-Fail {Assert-CandidateClean $repo}
[IO.File]::WriteAllText((Join-Path $repo 'backend/a.go'),$content)
[IO.File]::WriteAllText((Join-Path $repo 'backend/new.go'),$content)
Must-Fail {Assert-CandidateClean $repo}
[IO.File]::WriteAllText((Join-Path $repo '.gitignore'),"backend/new.go`n")
Must-Fail {Assert-CandidateClean $repo}
Write-Output "PASS: $checks PowerShell contract checks; synthetic fixture retained at $fixtureRoot; no load executed."
