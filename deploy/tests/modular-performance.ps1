[CmdletBinding()]
param(
  [switch]$Execute,
  [string]$BaselineImage = 'ghcr.io/hzihuan001/sub2api:0.2.16-custom.5',
  [string]$CandidateImage,
  [string]$ExpectedCandidateImageId,
  [string]$ExpectedCandidateCommit,
  [switch]$SseCrossoverDiagnostic,
  [string]$OutputDirectory = 'D:/projects/go-tmp/modular-performance-20261015'
)
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 required' }
$project = 'sub2api-operator-test-perf20261015'
$port = 18092
$base = "http://127.0.0.1:$port"
$compose = @('compose', '-p', $project, '-f', (Join-Path $PSScriptRoot '../docker-compose.operator-test.yml'), '-f', (Join-Path $PSScriptRoot 'modular-perf.compose.yml'))
if (-not $Execute) { Write-Output 'Read-only plan: internal Docker network, synthetic HTTP/SSE; default two baseline/candidate rounds, optional fixed CBBC SSE diagnostic on the retained fixture. Add -Execute and exact candidate image ID.'; return }
if (-not $CandidateImage -or $ExpectedCandidateImageId -notmatch '^sha256:[0-9a-f]{64}$' -or $ExpectedCandidateCommit -notmatch '^[0-9a-f]{40}$') { throw 'Exact candidate image/ID/runtime commit required' }
$context = (& docker context inspect --format '{{.Endpoints.docker.Host}}').Trim()
if ($LASTEXITCODE -ne 0 -or $context -notmatch '^(npipe|unix)://' -or ($env:DOCKER_HOST -and $env:DOCKER_HOST -notmatch '^(npipe|unix)://')) { throw 'Only local Docker engine allowed' }
$baseline = & docker image inspect $BaselineImage | ConvertFrom-Json | Select-Object -First 1
$candidate = & docker image inspect $CandidateImage | ConvertFrom-Json | Select-Object -First 1
if ($baseline.Id -ne 'sha256:3a2f925574266819adc3cddad9595170cd71404519f315326f83d61098b6a80b') { throw 'Wrong frozen published baseline' }
if ($candidate.Id -ne $ExpectedCandidateImageId) { throw 'Candidate image ID mismatch' }
if ((Test-Path $OutputDirectory) -and @(Get-ChildItem -LiteralPath $OutputDirectory -Force).Count) { throw 'Use a new evidence directory; never overwrite a prior measurement' }
if (-not $SseCrossoverDiagnostic) {
if (@(& docker ps -a -q --filter "label=com.docker.compose.project=$project").Count -or @(& docker volume ls -q --filter "label=com.docker.compose.project=$project").Count -or @(& docker network ls -q --filter "label=com.docker.compose.project=$project").Count) { throw 'Performance project already exists; inspect evidence rather than overwrite/reinitialize' }
foreach ($volume in @('app_data','postgres_data','redis_data')) {
  & docker volume inspect "${project}_$volume" 2>$null | Out-Null
  if ($LASTEXITCODE -eq 0) { throw "Pre-existing exact volume name: ${project}_$volume" }
}
& docker network inspect "${project}_default" 2>$null | Out-Null
if ($LASTEXITCODE -eq 0) { throw 'Pre-existing exact network name' }
foreach ($service in @('sub2api','postgres','redis','perf-mock')) {
  & docker container inspect "$project-$service-1" 2>$null | Out-Null
  if ($LASTEXITCODE -eq 0) { throw "Pre-existing exact container name: $service" }
}
if (Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue) { throw 'Performance port occupied' }
}
$null = New-Item -ItemType Directory -Path $OutputDirectory -Force
$env:SUB2API_TEST_PORT = "$port"
function Compose([string[]]$Arguments) { & docker @compose @Arguments; if ($LASTEXITCODE -ne 0) { throw "Compose failed: $Arguments" } }
function Control([string]$Method, [string]$Path, [string]$Token = '', [object]$Body = $null) {
  # Desktop does not publish ports on an internal-only network. Control calls
  # use its existing fixture container; the measured client is unchanged.
  $container=(& docker @compose ps -q perf-mock).Trim()
  $script=@'
let raw=''; for await (const chunk of process.stdin) raw += chunk;
const c=JSON.parse(raw);
if (c.path !== '/health' && !c.path.startsWith('/api/v1/')) throw new Error('invalid local control path');
const headers={}; if(c.token) headers.authorization=`Bearer ${c.token}`;
if(c.body !== null) headers['content-type']='application/json';
const r=await fetch(`http://sub2api:8080${c.path}`, {method:c.method,headers,body:c.body===null?undefined:JSON.stringify(c.body),signal:AbortSignal.timeout(c.path==='/health'?2000:45000)});
console.log(JSON.stringify({status:r.status,content:await r.text()}));
'@
  $config=@{method=$Method;path=$Path;token=$Token;body=$Body} | ConvertTo-Json -Depth 20 -Compress
  $text=$config | & docker exec -i $container node --input-type=module -e $script
  if ($LASTEXITCODE -ne 0) { throw "Local control request failed: $Method $Path" }
  return $text | ConvertFrom-Json
}
function Api([string]$Method, [string]$Path, [string]$Token = '', [object]$Body = $null) {
  $response=Control $Method $Path $Token $Body
  if ($response.status -ge 300) { throw "$Method $Path HTTP $($response.status): $($response.content)" }
  if ($response.content) { return $response.content | ConvertFrom-Json }
}
function Wait-App {
  for ($i=0; $i -lt 90; $i++) {
    try { if ((Api GET '/health').status -eq 'ok') { return } } catch { }
    Start-Sleep -Seconds 1
  }
  throw 'Synthetic app unhealthy'
}
function Verify-Isolation {
  $network=& docker network inspect "${project}_default" | ConvertFrom-Json | Select-Object -First 1
  if (-not $network.Internal -or $network.Labels.'com.docker.compose.project' -ne $project) { throw 'Network is not isolated/internal' }
  foreach ($entry in @(@('sub2api','app_data','/app/data'),@('postgres','postgres_data','/var/lib/postgresql/data'),@('redis','redis_data','/data'))) {
    $service=$entry[0]; $volume=$entry[1]; $destination=$entry[2]
    $id=(& docker @compose ps -q $service).Trim()
    $container=& docker inspect $id | ConvertFrom-Json | Select-Object -First 1
    $mount=@($container.Mounts | Where-Object { $_.Destination -eq $destination })
    $vol=& docker volume inspect "${project}_$volume" | ConvertFrom-Json | Select-Object -First 1
    if ($container.Config.Labels.'com.docker.compose.project' -ne $project -or $container.Config.Labels.'com.docker.compose.service' -ne $service -or $mount.Count -ne 1 -or $mount[0].Type -ne 'volume' -or $mount[0].Name -ne "${project}_$volume" -or $vol.Labels.'com.docker.compose.project' -ne $project -or $vol.Labels.'com.docker.compose.volume' -ne $volume) { throw "Wrong synthetic mount: $service" }
  }
}
function Assert-SnapshotIdentity($Identity, [string]$Container, [string]$ExpectedImageId) {
  if (-not $Identity -or $Container -notmatch '^[0-9a-f]{64}$' -or $Identity.Id -ne $Container -or $ExpectedImageId -notmatch '^sha256:[0-9a-f]{64}$' -or $Identity.Image -ne $ExpectedImageId -or -not $Identity.State.StartedAt -or $null -eq $Identity.RestartCount -or $Identity.RestartCount -lt 0 -or -not $Identity.State.Running) { throw 'Missing or mismatched snapshot identity' }
  $parsed=[datetimeoffset]::MinValue
  if (-not [datetimeoffset]::TryParse($Identity.State.StartedAt, [ref]$parsed) -or $parsed.Year -le 1) { throw 'Invalid snapshot start time' }
}
function State([string]$ExpectedImageId) {
  $container=(& docker @compose ps -q sub2api).Trim()
  if ($LASTEXITCODE -ne 0 -or $container -notmatch '^[0-9a-f]{64}$') { throw 'Snapshot container lookup failed' }
  $identityJson=& docker inspect $container
  if ($LASTEXITCODE -ne 0) { throw 'Snapshot inspect failed' }
  $identities=@($identityJson | ConvertFrom-Json)
  if ($identities.Count -ne 1) { throw 'Snapshot identity missing or ambiguous' }
  $identity=$identities[0]
  Assert-SnapshotIdentity $identity $container $ExpectedImageId
  $raw=& docker exec $container sh -c 'if [ -r /sys/fs/cgroup/cpu.stat ]; then cat /sys/fs/cgroup/cpu.stat; elif [ -r /sys/fs/cgroup/cpuacct/cpuacct.usage ]; then printf "usage_nsec "; cat /sys/fs/cgroup/cpuacct/cpuacct.usage; elif [ -r /sys/fs/cgroup/cpu,cpuacct/cpuacct.usage ]; then printf "usage_nsec "; cat /sys/fs/cgroup/cpu,cpuacct/cpuacct.usage; else exit 1; fi; cat /proc/1/status'
  if ($LASTEXITCODE -ne 0) { throw 'Resource snapshot failed' }
  $cpu=([regex]::Match(($raw -join "`n"), '(?m)^usage_usec (\d+)$')).Groups[1].Value
  if (-not $cpu) {
    $nsec=([regex]::Match(($raw -join "`n"), '(?m)^usage_nsec (\d+)$')).Groups[1].Value
    if ($nsec) { $cpu=[math]::Floor([double]$nsec/1000) }
  }
  $rss=([regex]::Match(($raw -join "`n"), '(?m)^VmRSS:\s+(\d+) kB$')).Groups[1].Value
  if (-not $cpu -or -not $rss) { throw 'Resource counters missing' }
  return @{ cpuUsec=[long]$cpu; rssKiB=[long]$rss; containerId=$identity.Id; imageId=$identity.Image; startedAt=$identity.State.StartedAt; restartCount=$identity.RestartCount }
}
function MockStats {
  $container=(& docker @compose ps -q perf-mock).Trim()
  $text=& docker exec $container node -e "fetch('http://localhost:8080/stats').then(r=>r.text()).then(console.log)"
  if ($LASTEXITCODE -ne 0) { throw 'Mock stats failed' }
  return $text | ConvertFrom-Json
}
function Load([bool]$Stream, [int]$Count) {
  $container=(& docker @compose ps -q perf-mock).Trim()
  $config=@{key=$script:syntheticKey;stream=$Stream;count=$Count;concurrency=2} | ConvertTo-Json -Compress
  $text=$config | & docker exec -i $container node /fixture/perf.mjs client
  if ($LASTEXITCODE -ne 0) { throw 'Synthetic workload failed' }
  return $text | ConvertFrom-Json
}
$all=[Collections.Generic.List[object]]::new()
$versions=@{}
$initialized=$false
$initialStats=$null
$fixtureHash=(Get-FileHash -Algorithm SHA256 (Join-Path $PSScriptRoot 'modular-perf-fixture.mjs')).Hash.ToLowerInvariant()
if ($SseCrossoverDiagnostic) {
  # A bounded one-off diagnostic of the retained run, not arbitrary fixture resume.
  if ($ExpectedCandidateCommit -ne '53fd17b5b46be47a216f55381beaac24eefe667f' -or $ExpectedCandidateImageId -ne 'sha256:a11840c20fa0e546ec1518f44762cb51ce448382a38beece0214f46c0e116a11' -or $fixtureHash -ne 'b46b2da7968cc2b6236c30a7fa58cafe77ca98c1ede91376779a4ab001be1d4b') { throw 'Diagnostic immutable inputs differ' }
  $prior=Get-Content -LiteralPath 'D:/projects/go-tmp/modular-performance-53fd17b5-cgroup1-20261015/provenance.json' -Raw | ConvertFrom-Json
  if ($prior.candidateId -ne $candidate.Id -or $prior.baselineId -ne $baseline.Id -or $prior.totalMeasuredRequests -ne 4000) { throw 'Original complete performance evidence missing or different' }
  $retained=@{postgres='015130dd4d259c4303fb9cad5dc98416e2969f136f58d1f0fc92af239f232c4c';redis='4a41a1b5edc309c80f3c4019a72291e80ffecd05804d20838bad29fb6b576529';'perf-mock'='54f7699a0ac15ad1b659d77ac7f3144dd966cfa5574ab34868e0cdcf991ddfdd';sub2api='1369b03e92d1d0e0ae2e850dd224ee6a604f42469816f5f6d1ce813111cde7b1'}
  foreach($service in $retained.Keys) {
    $id=(& docker @compose ps -q $service).Trim()
    if ($LASTEXITCODE -ne 0 -or $id -ne $retained[$service]) { throw "Retained diagnostic container differs: $service" }
  }
  Verify-Isolation
  $initialStats=MockStats
  if ($initialStats.total -ne 4241 -or $initialStats.http -ne 2120 -or $initialStats.sse -ne 2120 -or $initialStats.probe -ne 1 -or $initialStats.rejected -ne 0) { throw 'Retained fixture has changed since the original measurement' }
  $user=(Api POST '/api/v1/auth/login' '' @{email='performance@operator-test.local';password='Performance-Local-2026!'}).data.access_token
  $keys=(Api GET '/api/v1/keys?page=1&page_size=20' $user).data.items
  $matching=@($keys | Where-Object { $_.name -eq 'performance-local' })
  if ($matching.Count -ne 1 -or $matching[0].key -notmatch '^sk-') { throw 'Original synthetic key missing; do not create replacement data' }
  $script:syntheticKey=$matching[0].key
  $initialized=$true
  $schedule=@('candidate','baseline','baseline','candidate')
} else { $schedule=@('baseline','candidate','baseline','candidate') }
$stage=0
foreach ($variant in $schedule) {
    $stage++
    $round=if($SseCrossoverDiagnostic){$stage}else{[int][math]::Ceiling($stage/2)}
    $env:SUB2API_TEST_IMAGE = if ($variant -eq 'baseline') { $BaselineImage } else { $CandidateImage }
    Compose @('config','--quiet')
    if (-not $initialized) { Compose @('up','-d','--no-build') } elseif ($SseCrossoverDiagnostic) { Compose @('up','-d','--no-deps','--no-build','--force-recreate','sub2api') } else { Compose @('up','-d','--no-deps','--no-build','sub2api') }
    Wait-App
    Verify-Isolation
    $actual=(& docker inspect (& docker @compose ps -q sub2api) --format '{{.Image}}').Trim()
    $expected=if ($variant -eq 'baseline') { $baseline.Id } else { $candidate.Id }
    if ($actual -ne $expected) { throw 'Running image mismatch' }
    $runtimeCommit=if ($variant -eq 'baseline') { '00e8f24a40eba481d1e61cfebb5f4ab3f9401a2b' } else { $ExpectedCandidateCommit }
    $binaryVersion=(& docker exec (& docker @compose ps -q sub2api) /app/sub2api --version) -join "`n"
    if ($LASTEXITCODE -ne 0 -or -not $binaryVersion.Contains($runtimeCommit)) { throw "Running binary revision mismatch: $variant" }
    $versions[$variant]=$binaryVersion
    if (-not $initialized) {
      $admin=(Api POST '/api/v1/auth/login' '' @{email='admin@operator-test.local';password='Operator-Test-Admin-Password-2026!'}).data.access_token
      $compliance=(Api GET '/api/v1/admin/compliance' $admin).data
      if ($compliance.required) { $null=Api POST '/api/v1/admin/compliance/accept' $admin @{phrase=$compliance.ack_phrase_en;language='en'} }
      $null=Api POST '/api/v1/admin/users' $admin @{email='performance@operator-test.local';password='Performance-Local-2026!';role='user';balance=100;concurrency=10}
      $user=(Api POST '/api/v1/auth/login' '' @{email='performance@operator-test.local';password='Performance-Local-2026!'}).data.access_token
      $group=(Api POST '/api/v1/admin/groups' $admin @{name='performance-local';platform='openai';subscription_type='standard';rate_multiplier=1}).data
      $null=Api POST '/api/v1/admin/accounts' $admin @{name='performance-local';platform='openai';type='apikey';concurrency=10;priority=0;rate_multiplier=1;group_ids=@($group.id);extra=@{openai_passthrough=$true;openai_responses_mode='force_responses'};credentials=@{api_key='synthetic-perf-key';base_url='http://perf-mock:8080/v1';model_mapping=@{'gpt-4o-mini'='gpt-4o-mini'}}}
      $script:syntheticKey=(Api POST '/api/v1/keys' $user @{name='performance-local';group_id=$group.id;quota=10}).data.key
      $initialized=$true
    }
    Start-Sleep -Seconds 3
    $streams=if($SseCrossoverDiagnostic){@($true)}else{@($false,$true)}
    foreach ($stream in $streams) {
      $warmupBefore=MockStats
      $null=Load $stream 30
      $warmupAfter=MockStats
      @{round=$round;variant=$variant;imageId=$actual;stream=$stream;count=30;before=$warmupBefore;after=$warmupAfter} | ConvertTo-Json -Depth 10 -Compress | Add-Content -LiteralPath (Join-Path $OutputDirectory 'warmups.jsonl')
      $windowCount=if($SseCrossoverDiagnostic){3}else{5}
      foreach ($window in 1..$windowCount) {
        $statsBefore=MockStats
        $before=State $expected
        $load=Load $stream 100
        $after=State $expected
        $statsAfter=MockStats
        $deltaHttp=$statsAfter.http-$statsBefore.http
        $deltaSse=$statsAfter.sse-$statsBefore.sse
        if ($statsAfter.total-$statsBefore.total -ne 100 -or $statsAfter.probe-$statsBefore.probe -ne 0 -or $statsAfter.rejected-$statsBefore.rejected -ne 0 -or $deltaHttp+$deltaSse -ne 100 -or ($stream -and $deltaSse -ne 100) -or (-not $stream -and $deltaHttp -ne 100)) { throw 'Upstream call count differs from user requests' }
        if ($before.containerId -ne $after.containerId -or $before.imageId -ne $after.imageId -or $before.startedAt -ne $after.startedAt -or $before.restartCount -ne $after.restartCount -or $after.cpuUsec -lt $before.cpuUsec) { throw 'Application restarted or resource counters reset during measured window' }
        $entry=[pscustomobject]@{round=$round;variant=$variant;imageId=$actual;stream=$stream;window=$window;cpuUsecPerRequest=($after.cpuUsec-$before.cpuUsec)/100;rssKiB=$after.rssKiB;upstreamCalls=100;load=$load}
        $all.Add($entry)
        $entry | ConvertTo-Json -Depth 20 -Compress | Add-Content -LiteralPath (Join-Path $OutputDirectory 'windows.jsonl')
        Write-Output "$variant round=$round stream=$stream window=$window p50=$($load.p50TotalMs) p95=$($load.p95TotalMs) cpu_us/request=$($entry.cpuUsecPerRequest) rss_kib=$($entry.rssKiB)"
      }
    }
}
$all | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath (Join-Path $OutputDirectory 'results.json')
@{baselineId=$baseline.Id;baselineRevision='00e8f24a40eba481d1e61cfebb5f4ab3f9401a2b';candidateId=$candidate.Id;candidateRevision=$ExpectedCandidateCommit;binaryVersions=$versions;project=$project;port=$port;totalMeasuredRequests=($all.Count*100);diagnostic=[bool]$SseCrossoverDiagnostic;schedule=$schedule;fixtureSha256=$fixtureHash;mockBefore=$initialStats;mock=MockStats;note='Runtime commits verified from the running binary --version, not optional OCI labels. Internal network only; 5ms response delay, SSE extra 5ms; concurrency2. CPU is app-container window cost including background work and sampling, not isolated request CPU. RSS sampled after each window, not peak RSS. No production or paid provider calls.'} | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $OutputDirectory 'provenance.json')
Write-Output "Evidence: $OutputDirectory. Isolated project retained; no automatic volume/image cleanup."
