[CmdletBinding()]
param(
  [ValidateSet('sub2api-operator-test-modular')][string]$Project = 'sub2api-operator-test-modular',
  [int]$Port = 18089,
  [int]$MockPort = 18090,
  [int]$TimeoutSeconds = 180,
  [string]$ExpectedImageId = '',
  [switch]$Execute
)

# No Docker command is run without an explicit execution opt-in. This script never
# builds, deploys, removes fixtures, flushes Redis, or alters application source.
if (-not $Execute) {
  Write-Output 'Prepared only. Read deploy/acceptance/IMAGE_RECOVERY.md; pass -Execute -ExpectedImageId sha256:<candidate image ID> only after the isolated candidate is ready.'
  return
}
if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 or newer is required.' }
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
if ($ExpectedImageId -notmatch '^sha256:[a-f0-9]{64}$') { throw 'An exact prebuilt candidate image ID is required.' }
if ($Port -lt 1024 -or $Port -gt 65535 -or $MockPort -lt 1024 -or $MockPort -gt 65535 -or $Port -eq $MockPort) { throw 'Invalid loopback ports.' }
if ($TimeoutSeconds -lt 30 -or $TimeoutSeconds -gt 600) { throw 'Timeout must be between 30 and 600 seconds.' }
if ($env:DOCKER_HOST -and $env:DOCKER_HOST -notmatch '^(npipe:////\./pipe/|unix:///)') { throw 'Remote Docker hosts are forbidden.' }

$run = [Guid]::NewGuid().ToString('N').Substring(0, 10)
$control = [Guid]::NewGuid().ToString('N')
$baseUri = "http://127.0.0.1:$Port"
$mockUri = "http://127.0.0.1:$MockPort"
$mockName = "image-recovery-$run"
$network = "${Project}_default"
$artifactDir = Join-Path ([IO.Path]::GetTempPath()) "moshu-image-recovery-$run"
$null = New-Item -ItemType Directory -Path $artifactDir
$ownedBatches = [Collections.Generic.List[string]]::new()
$evidence = [ordered]@{
  status = 'running'; run = $run; project = $Project; candidate_image = $ExpectedImageId
  started_at = [DateTime]::UtcNow.ToString('o'); checkpoints = @(); kills = @(); fixture_ids = @{}
  limitations = @('Only this run''s Redis active timestamp and lock TTL are accelerated while app is stopped; natural TTL waiting is not tested.',
    'The durable checkpoint window is held by a test-only PostgreSQL row lock; no result or settling state is fabricated.',
    'Unknown outcome remains held and unresolved; this is not automatic unknown-outcome recovery.',
    'Fixtures and unknown hold are deliberately retained; no automatic cleanup or refund is performed.')
}
$script:lockProcess = $null
$script:lockApp = ''
$script:appStoppedByHarness = $false
$script:mockStarted = $false
$appId = $pgId = $redisId = ''

function Assert-True([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Invoke-Docker([string[]]$Arguments) {
  $output = & docker @Arguments 2>&1
  if ($LASTEXITCODE -ne 0) { throw "Docker command failed ($($Arguments[0])): $($output -join ' ')" }
  return ($output -join "`n").Trim()
}
function Inspect-Container([string]$Id) {
  return @(ConvertFrom-Json -AsHashtable (Invoke-Docker @('inspect', $Id)))[0]
}
function Save-Evidence {
  $evidence['batch_ids'] = @($ownedBatches)
  $evidence['artifact_directory'] = $artifactDir
  $evidence | ConvertTo-Json -Depth 30 | Set-Content -LiteralPath (Join-Path $artifactDir 'evidence.json') -Encoding utf8
}
function Wait-Until([string]$Label, [scriptblock]$Predicate, [int]$Seconds = $TimeoutSeconds) {
  $deadline = [DateTime]::UtcNow.AddSeconds($Seconds)
  do {
    if (& $Predicate) { return }
    Start-Sleep -Milliseconds 300
  } while ([DateTime]::UtcNow -lt $deadline)
  throw "Timed out waiting for $Label"
}
function Request-Json([string]$Method, [string]$Path, [string]$Token = '', [object]$Body = $null) {
  Assert-True ($Path.StartsWith('/') -and -not $Path.StartsWith('//')) 'Only local API paths are allowed'
  $headers = @{ 'User-Agent' = 'moshu-local-image-recovery/1' }
  if ($Token) { $headers.Authorization = "Bearer $Token" }
  $parameters = @{ Uri = "$baseUri$Path"; Method = $Method; Headers = $headers; TimeoutSec = 30; MaximumRedirection = 0; SkipHttpErrorCheck = $true }
  if ($null -ne $Body) { $parameters.Body = ConvertTo-Json $Body -Depth 20 -Compress; $parameters.ContentType = 'application/json' }
  $response = Invoke-WebRequest @parameters
  Assert-True ([int]$response.StatusCode -ge 200 -and [int]$response.StatusCode -lt 300) "$Method $Path failed: HTTP $($response.StatusCode) $($response.Content)"
  if ($response.Content) { return $response.Content | ConvertFrom-Json }
}
function Mock-Request([string]$Path, [string]$Method = 'GET', [object]$Body = $null) {
  $parameters = @{ Uri = "$mockUri/__recovery/$Path"; Method = $Method; Headers = @{ 'X-Recovery-Control' = $control }; TimeoutSec = 5; MaximumRedirection = 0 }
  if ($null -ne $Body) { $parameters.Body = ConvertTo-Json $Body -Compress; $parameters.ContentType = 'application/json' }
  return Invoke-RestMethod @parameters
}
function Assert-OwnedDataMount([hashtable]$Info, [string]$DataPath, [string]$VolumeName) {
  Assert-True ($DataPath -match '^/[^\r\n]+$' -and $DataPath -notmatch '(^|/)\.\.?(/|$)') 'Data directory must be a resolved absolute container path'
  $dataRoot = $DataPath.TrimEnd('/')
  $covering = @($Info.Mounts | Where-Object { $dataRoot -eq $_.Destination.TrimEnd('/') -or $dataRoot.StartsWith($_.Destination.TrimEnd('/') + '/', [StringComparison]::Ordinal) })
  Assert-True ($covering.Count -gt 0) "Actual data directory $DataPath is not backed by a named Compose volume"
  # Only the most specific covering mount supplies this directory (Postgres 18
  # can have an unrelated anonymous /var/lib/postgresql parent mount). Also check
  # every child mount so a valid parent cannot hide foreign pg_wal/result/AOF data.
  $effective = $covering | Sort-Object { $_.Destination.Length } -Descending | Select-Object -First 1
  $relevant = @($effective) + @($Info.Mounts | Where-Object {
    $mountRoot = $_.Destination.TrimEnd('/')
    $mountRoot.StartsWith($dataRoot + '/', [StringComparison]::Ordinal)
  })
  foreach ($mount in $relevant) {
    Assert-True ($mount.Type -eq 'volume' -and [bool]$mount.Name -and $mount.RW -eq $true) "Actual data directory $DataPath has a bind, anonymous, or read-only mount"
    $volume = @(ConvertFrom-Json -AsHashtable (Invoke-Docker @('volume', 'inspect', $mount.Name)))[0]
    Assert-True ($volume.Labels['com.docker.compose.project'] -eq $Project -and $volume.Labels['com.docker.compose.volume'] -eq $VolumeName) "Data volume for $DataPath does not belong to this project's $VolumeName"
    Assert-True ($volume.Driver -eq 'local' -and (-not $volume.Options -or $volume.Options.Count -eq 0)) 'External/bind-backed volume drivers are forbidden'
  }
}
function Assert-Container([string]$Id, [string]$Service) {
  $info = Inspect-Container $Id
  Assert-True ($info.Config.Labels['com.docker.compose.project'] -eq $Project -and $info.Config.Labels['com.docker.compose.service'] -eq $Service) "Container $Id does not belong to the exact isolated Compose service"
  Assert-True ($info.NetworkSettings.Networks.ContainsKey($network)) "Container $Service is not on the isolated network"
  foreach ($bindings in $info.HostConfig.PortBindings.Values) {
    foreach ($binding in $bindings) {
      Assert-True ($binding.HostIp -eq '127.0.0.1') "Container $Service has a non-loopback published port"
    }
  }
  if ($Service -eq 'sub2api') {
    Assert-True ($info.Image -eq $ExpectedImageId) 'Running candidate differs from the explicitly selected image'
    $bindings = @($info.HostConfig.PortBindings['8080/tcp'])
    Assert-True ($bindings.Count -eq 1 -and $bindings[0].HostIp -eq '127.0.0.1' -and $bindings[0].HostPort -eq "$Port") 'App loopback port does not match the test URL'
    Assert-True ($info.Config.WorkingDir -eq '/app') 'Unexpected app working directory'
    Assert-OwnedDataMount $info '/app/data' 'app_data'
  } elseif ($Service -eq 'postgres') {
    # These are read-only discovery commands. No SQL/Redis mutation is allowed
    # until actual storage, not just service labels or declared env, is checked.
    $configured = @($info.Config.Env | Where-Object { $_.StartsWith('PGDATA=') })
    Assert-True ($configured.Count -eq 1) 'Postgres must declare exactly one PGDATA'
    $configuredPath = Invoke-Docker @('exec', $Id, 'readlink', '-f', $configured[0].Substring(7))
    $actualPath = Invoke-Docker @('exec', $Id, 'psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1', '-U', 'sub2api_operator_test', '-d', 'sub2api_operator_test', '-c', 'SHOW data_directory;')
    $actualPath = Invoke-Docker @('exec', $Id, 'readlink', '-f', $actualPath)
    Assert-True ($actualPath -eq $configuredPath) 'Actual PostgreSQL data_directory differs from PGDATA'
    Assert-OwnedDataMount $info $actualPath 'postgres_data'
  } elseif ($Service -eq 'redis') {
    $directory = (Invoke-Docker @('exec', $Id, 'redis-cli', '--raw', '-n', '0', 'CONFIG', 'GET', 'dir')) -split "\r?\n"
    Assert-True ($directory.Count -eq 2 -and $directory[0] -eq 'dir') 'Cannot establish actual Redis data directory'
    $actualPath = Invoke-Docker @('exec', $Id, 'readlink', '-f', $directory[1])
    Assert-OwnedDataMount $info $actualPath 'redis_data'
  }
  return $info
}
function Assert-IsolatedStack {
  $null = Assert-Container $appId 'sub2api'
  $null = Assert-Container $pgId 'postgres'
  $null = Assert-Container $redisId 'redis'
}
function Invoke-Sql([string]$Sql) {
  return Invoke-Docker @('exec', $pgId, 'psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1', '-U', 'sub2api_operator_test', '-d', 'sub2api_operator_test', '-c', $Sql)
}
function Redis([string[]]$Command) { return Invoke-Docker (@('exec', $redisId, 'redis-cli', '--raw', '-n', '0') + $Command) }
function Assert-OwnedBatch([string]$BatchId) {
  Assert-True ($BatchId -match '^imgbatch_[a-f0-9]{32}$' -and $ownedBatches.Contains($BatchId)) 'Only this run''s exact batch IDs may be mutated'
  $owner = Invoke-Sql "SELECT count(*) FROM batch_image_jobs WHERE batch_id='$BatchId' AND user_id=$userId AND api_key_id=$keyId AND account_id=$accountId AND provider='app_managed';"
  Assert-True ($owner -eq '1') 'Batch ownership/protocol no longer matches the synthetic fixture'
}
function Assert-NoOtherJobs {
  $exclude = if ($ownedBatches.Count) { "AND batch_id NOT IN ('" + ($ownedBatches -join "','") + "')" } else { '' }
  $count = Invoke-Sql "SELECT count(*) FROM batch_image_jobs WHERE status NOT IN ('completed','failed','cancelled','output_deleted') $exclude;"
  Assert-True ($count -eq '0') 'Another unfinished batch exists; refusing to kill the app process'
  $lua = 'local ids=redis.call("LRANGE",KEYS[1],0,-1); for i=2,3 do for _,id in ipairs(redis.call("ZRANGE",KEYS[i],0,-1)) do ids[#ids+1]=id end end; return ids'
  $queued = Redis @('EVAL_RO', $lua, '3', 'batch_image:queue:ready', 'batch_image:queue:active', 'batch_image:queue:delayed')
  foreach ($id in ($queued -split "`n")) {
    if ($id) { Assert-True ($ownedBatches.Contains($id)) 'Another queued batch exists; refusing to kill the app process' }
  }
}
function Snapshot([string]$BatchId) {
  Assert-OwnedBatch $BatchId
  $sql = @"
WITH claims AS (
  SELECT request_id FROM usage_billing_dedup WHERE api_key_id=$keyId
  UNION ALL SELECT request_id FROM usage_billing_dedup_archive WHERE api_key_id=$keyId
)
SELECT json_build_object(
 'batch_id',j.batch_id,'status',j.status,'provider',j.provider,'success_count',j.success_count,'fail_count',j.fail_count,
 'hold_amount',j.hold_amount,'actual_cost',j.actual_cost,'manifest_hash',j.manifest_hash,
 'balance',u.balance,'frozen_balance',u.frozen_balance,'key_quota_used',k.quota_used,
 'hold_claims',(SELECT count(*) FROM claims WHERE request_id='batch_image_hold:$BatchId'),
 'capture_claims',(SELECT count(*) FROM claims WHERE request_id='batch_image_capture:$BatchId'),
 'release_claims',(SELECT count(*) FROM claims WHERE request_id='batch_image_release:$BatchId'),
 'usage_rows',(SELECT count(*) FROM usage_logs WHERE api_key_id=$keyId AND request_id='batch_image_capture:$BatchId'),
 'usage_cost',(SELECT COALESCE(sum(actual_cost),0) FROM usage_logs WHERE api_key_id=$keyId AND request_id='batch_image_capture:$BatchId'),
 'key_usage_rows',(SELECT count(*) FROM usage_logs WHERE api_key_id=$keyId),
 'key_usage_cost',(SELECT COALESCE(sum(actual_cost),0) FROM usage_logs WHERE api_key_id=$keyId),
 'key_usage_request_ids',(SELECT COALESCE(json_agg(request_id ORDER BY request_id),'[]'::json) FROM (SELECT DISTINCT request_id FROM usage_logs WHERE api_key_id=$keyId) key_requests),
 'items',(SELECT json_agg(json_build_object('custom_id',custom_id,'status',status,'image_count',image_count,'managed_async',input_payload->'managed_async')) FROM batch_image_items WHERE job_id=j.batch_id)
) FROM batch_image_jobs j JOIN users u ON u.id=j.user_id JOIN api_keys k ON k.id=j.api_key_id WHERE j.batch_id='$BatchId';
"@
  return (Invoke-Sql $sql | ConvertFrom-Json)
}
function Checkpoint([string]$Name, [string]$BatchId) {
  $state = Snapshot $BatchId
  $evidence.checkpoints += [ordered]@{ name = $Name; at = [DateTime]::UtcNow.ToString('o'); database = $state; upstream = Mock-Request 'stats' }
  Save-Evidence
  return $state
}
function Assert-Money([object]$State, [double]$Balance, [double]$Frozen) {
  Assert-True ([Math]::Abs([double]$State.balance - $Balance) -lt 0.0000001) 'Unexpected available balance / duplicate charge'
  Assert-True ([Math]::Abs([double]$State.frozen_balance - $Frozen) -lt 0.0000001) 'Unexpected frozen balance / hold release'
}
function Assert-KeyUsage([object]$State, [int]$Rows, [double]$Cost, [string[]]$RequestIds) {
  Assert-True ($State.key_usage_rows -eq $Rows -and [Math]::Abs([double]$State.key_usage_cost - $Cost) -lt 0.0000001) 'Unexpected total synthetic-key usage count/cost (including alternate request IDs)'
  $actual = @($State.key_usage_request_ids | Sort-Object) | ConvertTo-Json -Compress -AsArray
  $expected = @($RequestIds | Sort-Object) | ConvertTo-Json -Compress -AsArray
  Assert-True ($actual -eq $expected) 'Synthetic-key usage contains unexpected request IDs'
}
function Assert-UpstreamOnce([string]$Case, [bool]$Responded) {
  $stats = Mock-Request 'stats'
  $attempts = @($stats.requests | Where-Object { $_.case -eq $Case })
  Assert-True ($attempts.Count -eq 1) "$Case was submitted upstream more or less than once"
  Assert-True ($attempts[0].model -eq 'gpt-image-2' -and $attempts[0].n -eq 1 -and $attempts[0].size -eq '2048x2048') 'Upstream model/output contract changed'
  Assert-True ([bool]$attempts[0].response_sent_at -eq $Responded) 'Unexpected response release state'
}
function Start-RowLock([string]$BatchId) {
  Assert-OwnedBatch $BatchId
  $script:lockApp = "image_recovery_${run}_durable"
  $start = [Diagnostics.ProcessStartInfo]::new()
  $start.FileName = (Get-Command docker).Source
  $start.UseShellExecute = $false
  $start.CreateNoWindow = $true
  $start.RedirectStandardInput = $true
  $start.RedirectStandardOutput = $true
  $start.RedirectStandardError = $true
  foreach ($argument in @('exec', '-i', '-e', "PGAPPNAME=$script:lockApp", $pgId, 'psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1', '-U', 'sub2api_operator_test', '-d', 'sub2api_operator_test')) { $start.ArgumentList.Add($argument) }
  $script:lockProcess = [Diagnostics.Process]::Start($start)
  $script:lockProcess.StandardInput.WriteLine("BEGIN; SELECT batch_id FROM batch_image_jobs WHERE batch_id='$BatchId' FOR UPDATE; SELECT pg_sleep(900); ROLLBACK;")
  $script:lockProcess.StandardInput.Close()
  Wait-Until 'exact test job row lock' {
    Assert-True (-not $script:lockProcess.HasExited) 'Row-lock fixture exited before acquiring the lock'
    (Invoke-Sql "SELECT count(*) FROM pg_stat_activity WHERE application_name='$script:lockApp' AND wait_event='PgSleep' AND state='active';") -eq '1'
  } 15
}
function Stop-RowLock {
  if ($script:lockApp) {
    # Terminate only the exact test-owned database session, never other app queries.
    $null = Assert-Container $pgId 'postgres'
    $null = Invoke-Sql "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name='$script:lockApp' AND usename='sub2api_operator_test' AND datname='sub2api_operator_test' AND pid<>pg_backend_pid();"
    if ($script:lockProcess) {
      Assert-True ($script:lockProcess.WaitForExit(10000)) 'Test row-lock session did not terminate'
      $script:lockProcess.Dispose()
      $script:lockProcess = $null
    }
    $script:lockApp = ''
  }
}
function Kill-App([string]$Reason) {
  Assert-IsolatedStack
  Assert-NoOtherJobs
  $before = Inspect-Container $appId
  Assert-True ($before.State.Running) 'App must be running before the real process kill'
  # docker stop with an explicit SIGKILL is NOT graceful: it also suppresses the
  # unless-stopped auto-restart so exact lease acceleration can happen offline.
  $null = Invoke-Docker @('stop', '--signal', 'SIGKILL', '--timeout', '0', $appId)
  $script:appStoppedByHarness = $true
  $after = Inspect-Container $appId
  Assert-True (-not $after.State.Running -and $after.State.ExitCode -eq 137) 'Expected stopped app with SIGKILL exit code 137'
  $evidence.kills += [ordered]@{ reason = $Reason; container_id = $appId; image = $after.Image; before_started_at = $before.State.StartedAt; finished_at = $after.State.FinishedAt; exit_code = $after.State.ExitCode }
  Save-Evidence
}
function Accelerate-OwnedLease([string]$BatchId) {
  Assert-IsolatedStack
  Assert-OwnedBatch $BatchId
  Assert-True (-not (Inspect-Container $appId).State.Running) 'Lease acceleration is allowed only while the app is stopped'
  Assert-True ([bool](Redis @('ZSCORE', 'batch_image:queue:active', $BatchId))) 'Killed job was not actually active in the production Redis queue'
  $null = Redis @('ZADD', 'batch_image:queue:active', 'XX', '0', $BatchId)
  $null = Redis @('DEL', "batch_image:queue:lock:$BatchId")
  $evidence.checkpoints += [ordered]@{ name = 'exact_owned_lease_time_acceleration'; batch_id = $BatchId; active_score = 0; deleted_lock_key = "batch_image:queue:lock:$BatchId" }
  Save-Evidence
}
function Start-App {
  Assert-IsolatedStack
  $previousStart = (Inspect-Container $appId).State.StartedAt
  $null = Invoke-Docker @('start', $appId)
  $script:appStoppedByHarness = $false
  Wait-Until 'restarted app health' {
    try { (Invoke-RestMethod "$baseUri/health" -TimeoutSec 2 -MaximumRedirection 0).status -eq 'ok' } catch { $false }
  }
  $info = Assert-Container $appId 'sub2api'
  Assert-True ($info.State.Running -and $info.State.StartedAt -ne $previousStart) 'App process start timestamp did not change'
  $evidence.kills[-1]['restarted_at'] = $info.State.StartedAt
  Save-Evidence
}
function Get-QueueSnapshot([string]$BatchId) {
  Assert-True ($BatchId -match '^imgbatch_[a-f0-9]{32}$') 'Invalid queue snapshot batch ID'
  # One read-only Redis execution observes all memberships at a single instant.
  # Separate LRANGE/ZSCORE calls can miss a delayed -> ready transition entirely.
  $lua = @'
local delayed=redis.call('ZSCORE',KEYS[3],ARGV[1])
return cjson.encode({ready=redis.call('LPOS',KEYS[1],ARGV[1]) and 1 or 0,
 active=redis.call('ZSCORE',KEYS[2],ARGV[1]) and 1 or 0,
 delayed=delayed and 1 or 0, delayed_score=delayed and tonumber(delayed) or cjson.null,
 lock=redis.call('EXISTS',KEYS[4]), inflight=redis.call('EXISTS',KEYS[5])})
'@
  $state = Redis @('EVAL_RO', $lua, '5', 'batch_image:queue:ready', 'batch_image:queue:active', 'batch_image:queue:delayed', "batch_image:queue:lock:$BatchId", "batch_image:queue:inflight:$BatchId", $BatchId) | ConvertFrom-Json
  foreach ($field in @('ready', 'active', 'delayed', 'lock', 'inflight')) {
    Assert-True ($null -ne $state.$field -and $state.$field -in @(0, 1)) 'Malformed atomic queue membership snapshot'
  }
  return $state
}
function Queue-Drained([string]$BatchId) {
  $state = Get-QueueSnapshot $BatchId
  return ($state.ready -eq 0 -and $state.active -eq 0 -and $state.delayed -eq 0 -and $state.lock -eq 0 -and $state.inflight -eq 0)
}
function Inject-Duplicates([string]$BatchId, [int]$Count) {
  Assert-IsolatedStack
  Assert-OwnedBatch $BatchId
  Assert-True ($Count -ge 1 -and $Count -le 3) 'Duplicate injection count out of bounds'
  $arguments = @('LPUSH', 'batch_image:queue:ready') + (@($BatchId) * $Count)
  $null = Redis $arguments
  $evidence.checkpoints += [ordered]@{ name = 'explicit_duplicate_delivery'; batch_id = $BatchId; copies = $Count; app_running = (Inspect-Container $appId).State.Running }
  Save-Evidence
}
function Submit-Case([string]$Case) {
  $job = Request-Json 'POST' '/v1/images/batches' $apiKey @{
    model = 'gpt-image-2'; provider = 'app_managed'; task_name = "image-recovery-$run-$Case"
    image_size = '2K'; response_mime_type = 'image/png'
    items = @(@{ custom_id = $Case; prompt = "recovery:${run}:$Case"; output_count = 1 })
  }
  Assert-True ($job.id -match '^imgbatch_[a-f0-9]{32}$' -and $job.item_count -eq 1) 'Expected one ordinary managed fanout item'
  $ownedBatches.Add([string]$job.id)
  Save-Evidence
  Wait-Until "$Case upstream receipt" { @((Mock-Request 'stats').requests | Where-Object { $_.case -eq $Case }).Count -eq 1 }
  Assert-UpstreamOnce $Case $false
  return [string]$job.id
}

try {
  $context = @(ConvertFrom-Json -AsHashtable (Invoke-Docker @('context', 'inspect')))[0]
  Assert-True ($context.Endpoints.docker.Host -match '^(npipe:////\./pipe/|unix:///)') 'Only a local Docker engine is allowed'
  $net = @(ConvertFrom-Json -AsHashtable (Invoke-Docker @('network', 'inspect', $network)))[0]
  Assert-True ($net.Labels['com.docker.compose.project'] -eq $Project) 'Exact isolated Compose network was not found'
  foreach ($service in @('sub2api', 'postgres', 'redis')) {
    $ids = (Invoke-Docker @('ps', '-a', '-q', '--no-trunc', '--filter', "label=com.docker.compose.project=$Project", '--filter', "label=com.docker.compose.service=$service")) -split "`n"
    Assert-True ($ids.Count -eq 1 -and $ids[0] -match '^[a-f0-9]{64}$') "Expected exactly one $service container in the isolated project"
    switch ($service) { 'sub2api' { $appId = $ids[0] }; 'postgres' { $pgId = $ids[0] }; 'redis' { $redisId = $ids[0] } }
  }
  Assert-IsolatedStack
  Assert-True ((Inspect-Container $appId).State.Running) 'Candidate must already be running; this script does not deploy it'
  $appEnvironment = (Inspect-Container $appId).Config.Env
  foreach ($expected in @('DATABASE_HOST=postgres','DATABASE_DBNAME=sub2api_operator_test','REDIS_HOST=redis','REDIS_DB=0','BATCH_IMAGE_QUEUE_ENABLED=true','BATCH_IMAGE_APP_MANAGED_ENABLED=true')) {
    Assert-True ($expected -in $appEnvironment) "Unexpected isolated app configuration: $expected is required"
  }
  foreach ($entry in $appEnvironment) {
    Assert-True ($entry -notmatch '^BATCH_IMAGE_(QUEUE_(READY|ACTIVE|DELAYED)_KEY|LOCK_KEY_PREFIX|INFLIGHT_KEY_PREFIX|APP_MANAGED_RESULT_DIR)=') 'This harness requires default queue keys and result directory'
  }
  Assert-True ((Invoke-Sql 'SELECT current_database();') -eq 'sub2api_operator_test') 'Unexpected database'
  Assert-NoOtherJobs
  $evidence['containers'] = @{ app = $appId; postgres = $pgId; redis = $redisId; mock = $mockName }
  Save-Evidence
  $mockScript = (Resolve-Path (Join-Path $PSScriptRoot '../acceptance/mock-image-recovery-upstream.mjs')).Path
  $null = Invoke-Docker @('run', '--detach', '--pull=never', '--name', $mockName, '--label', "moshu.acceptance.project=$Project", '--label', "moshu.acceptance.run=$run", '--network', $network,
    '--publish', "127.0.0.1:${MockPort}:8080", '--env', "RECOVERY_RUN=$run", '--env', "RECOVERY_CONTROL_TOKEN=$control", '--mount', "type=bind,source=$mockScript,target=/fixture/recovery.mjs,readonly", 'node:24-alpine', 'node', '/fixture/recovery.mjs')
  $script:mockStarted = $true
  Wait-Until 'controlled synthetic upstream' { try { (Invoke-RestMethod "$mockUri/health" -TimeoutSec 2 -MaximumRedirection 0).run -eq $run } catch { $false } } 30
  $admin = Request-Json 'POST' '/api/v1/auth/login' '' @{ email = 'admin@operator-test.local'; password = 'Operator-Test-Admin-Password-2026!' }
  $adminToken = [string]$admin.data.access_token
  Assert-True ([bool]$adminToken) 'Synthetic administrator login failed'
  $compliance = Request-Json 'GET' '/api/v1/admin/compliance' $adminToken
  if ($compliance.data.required) { $null = Request-Json 'POST' '/api/v1/admin/compliance/accept' $adminToken @{ phrase = $compliance.data.ack_phrase_en; language = 'en' } }
  $group = Request-Json 'POST' '/api/v1/admin/groups' $adminToken @{
    name = "image-recovery-$run"; platform = 'openai'; subscription_type = 'standard'; rate_multiplier = 1
    allow_image_generation = $true; allow_batch_image_generation = $true
    image_price_1k = 0.01; image_price_2k = 0.01; image_price_4k = 0.01; batch_image_discount_multiplier = 1; batch_image_hold_multiplier = 1
  }
  $groupId = [long]$group.data.id
  $evidence.fixture_ids['group'] = $groupId
  Save-Evidence
  $account = Request-Json 'POST' '/api/v1/admin/accounts' $adminToken @{
    name = "image-recovery-$run"; platform = 'openai'; type = 'apikey'; concurrency = 1; priority = 1; rate_multiplier = 1
    credentials = @{ api_key = 'local-recovery-fixture-not-a-real-key'; base_url = "http://${mockName}:8080/v1"; model_mapping = @{ 'gpt-image-2' = 'gpt-image-2' } }; group_ids = @($groupId)
  }
  $accountId = [long]$account.data.id
  $evidence.fixture_ids['account'] = $accountId
  Save-Evidence
  $user = Request-Json 'POST' '/api/v1/admin/users' $adminToken @{
    email = "image-recovery-$run@operator-test.local"; password = 'Image-Recovery-Password-2026!'; role = 'user'; balance = 10; concurrency = 1; allowed_groups = @($groupId)
  }
  $userId = [long]$user.data.id
  $evidence.fixture_ids['user'] = $userId
  Save-Evidence
  $login = Request-Json 'POST' '/api/v1/auth/login' '' @{ email = "image-recovery-$run@operator-test.local"; password = 'Image-Recovery-Password-2026!' }
  $key = Request-Json 'POST' '/api/v1/keys' $login.data.access_token @{ name = "image-recovery-$run"; group_id = $groupId; quota = 1 }
  $apiKey = [string]$key.data.key
  $keyId = [long]$key.data.id
  Assert-True ($userId -gt 0 -and $groupId -gt 0 -and $accountId -gt 0 -and $keyId -gt 0 -and [bool]$apiKey) 'Synthetic fixture creation failed'
  $evidence.fixture_ids['api_key'] = $keyId
  Save-Evidence
  $models = Request-Json 'GET' '/v1/images/batches/models' $apiKey
  Assert-True (@($models.data | Where-Object { $_.id -eq 'gpt-image-2' -and $_.provider -eq 'app_managed' -and $_.mode -eq 'managed_fanout' }).Count -eq 1) 'Ordinary managed_fanout model is unavailable'

  # Case A: a real result is durable, but the job cannot yet advance to indexing.
  $durable = Submit-Case 'durable'
  Start-RowLock $durable
  $null = Mock-Request 'release' 'POST' @{ case = 'durable' }
  Wait-Until 'fsync-complete result progress and blocked parent update' {
    $success = Invoke-Sql "SELECT count(*) FROM batch_image_items WHERE job_id='$durable' AND status='success' AND image_count=1;"
    $blocked = Invoke-Sql "SELECT count(*) FROM pg_stat_activity a WHERE EXISTS (SELECT 1 FROM pg_stat_activity blocker WHERE blocker.application_name='$script:lockApp' AND blocker.pid=ANY(pg_blocking_pids(a.pid)));"
    $success -eq '1' -and [int]$blocked -ge 1
  } 30
  $partialPath = "/app/data/batch-images/$durable.jsonl.partial"
  $partial = Invoke-Docker @('exec', $appId, 'cat', $partialPath)
  $line = $partial | ConvertFrom-Json
  Assert-True ($line.key -eq 'durable' -and [bool]$line.response.candidates[0].content.parts[0].inlineData.data) 'Durable partial file did not contain the actual confirmed image'
  $partial | Set-Content -LiteralPath (Join-Path $artifactDir 'durable-before-kill.jsonl') -Encoding utf8
  $before = Checkpoint 'durable_fsynced_before_kill' $durable
  Assert-True ($before.status -eq 'running' -and $null -eq $before.actual_cost -and $before.hold_claims -eq 1 -and $before.capture_claims -eq 0 -and $before.usage_rows -eq 0) 'Checkpoint was not before settlement'
  Assert-Money $before 9.99 0.01
  Assert-KeyUsage $before 0 0 @()
  Kill-App 'durable partial result, before indexing/capture'
  Stop-RowLock
  Accelerate-OwnedLease $durable
  Start-App
  Wait-Until 'durable job completion from persisted result' { (Snapshot $durable).status -eq 'completed' }
  Wait-Until 'durable initial queue acknowledgement' { Queue-Drained $durable }
  $settled = Checkpoint 'durable_recovered_completed' $durable
  Assert-Money $settled 9.99 0
  Assert-KeyUsage $settled 1 0.01 @("batch_image_capture:$durable")
  Assert-True ($settled.success_count -eq 1 -and $settled.fail_count -eq 0 -and $settled.capture_claims -eq 1 -and $settled.release_claims -eq 0 -and $settled.usage_rows -eq 1 -and [Math]::Abs([double]$settled.actual_cost - 0.01) -lt 0.0000001 -and [Math]::Abs([double]$settled.usage_cost - 0.01) -lt 0.0000001) 'Recovered result was not billed/logged exactly once'
  Assert-UpstreamOnce 'durable' $true
  $imagePath = Join-Path $artifactDir 'durable-recovered.png'
  $null = Invoke-WebRequest "$baseUri/v1/images/batches/$durable/items/durable/content" -Headers @{ Authorization = "Bearer $apiKey" } -OutFile $imagePath -TimeoutSec 15 -MaximumRedirection 0
  $imageBytes = [IO.File]::ReadAllBytes($imagePath)
  Assert-True ($imageBytes.Length -ge 8 -and [Convert]::ToHexString($imageBytes[0..7]) -eq '89504E470D0A1A0A') 'Recovered indexed image could not be downloaded as PNG'

  # Case B: force actual duplicate Redis deliveries both before and after restart.
  $stable = $settled | ConvertTo-Json -Depth 15 -Compress
  Inject-Duplicates $durable 2
  Wait-Until 'live duplicate deliveries to be acknowledged' { Queue-Drained $durable }
  $afterLiveDuplicate = Checkpoint 'completed_live_duplicates' $durable
  Assert-True (($afterLiveDuplicate | ConvertTo-Json -Depth 15 -Compress) -eq $stable) 'Live duplicate deliveries changed financial, usage, or result state'
  Kill-App 'completed job, before three explicit Redis duplicate deliveries'
  Inject-Duplicates $durable 3
  Start-App
  Wait-Until 'restart duplicate deliveries to be acknowledged' { Queue-Drained $durable }
  $afterDuplicate = Checkpoint 'completed_restart_duplicates' $durable
  Assert-True (($afterDuplicate | ConvertTo-Json -Depth 15 -Compress) -eq $stable) 'Completed duplicate deliveries/restart changed financial, usage, or result state'
  Assert-UpstreamOnce 'durable' $true

  # Case C: receipt without any response/checkpoint is genuinely unknown.
  $unknown = Submit-Case 'unknown'
  $held = Checkpoint 'unknown_received_before_kill' $unknown
  Assert-Money $held 9.98 0.01
  Assert-KeyUsage $held 1 0.01 @("batch_image_capture:$durable")
  Assert-True ($held.status -eq 'running' -and $held.hold_claims -eq 1 -and $held.capture_claims -eq 0 -and $held.release_claims -eq 0 -and $held.usage_rows -eq 0 -and $held.items[0].managed_async.protocol -eq 'managed_fanout' -and [bool]$held.items[0].managed_async.started_at) 'Unknown case lacks a real durable dispatch claim'
  # Exact-path tests: exit 0 means missing, not that a mock success was synthesized.
  $null = Invoke-Docker @('exec', $appId, 'test', '!', '-e', "/app/data/batch-images/$unknown.jsonl.partial")
  $null = Invoke-Docker @('exec', $appId, 'test', '!', '-e', "/app/data/batch-images/$unknown.jsonl")
  Kill-App 'upstream received request, response still withheld, no result checkpoint'
  Accelerate-OwnedLease $unknown
  Start-App
  Wait-Until 'unknown recovery attempt to retain and requeue' { (Get-QueueSnapshot $unknown).delayed -eq 1 }
  $firstDelay = (Get-QueueSnapshot $unknown).delayed_score
  Inject-Duplicates $unknown 1
  Wait-Until 'second unknown delivery to requeue without resubmission' {
    $queue = Get-QueueSnapshot $unknown
    $queue.delayed -eq 1 -and $queue.delayed_score -ne $firstDelay -and $queue.ready -eq 0 -and $queue.active -eq 0 -and $queue.lock -eq 0 -and $queue.inflight -eq 1
  }
  $unknownAfter = Checkpoint 'unknown_recovered_and_duplicate_still_held' $unknown
  Assert-KeyUsage $unknownAfter 1 0.01 @("batch_image_capture:$durable")
  Assert-True (($unknownAfter | ConvertTo-Json -Depth 15 -Compress) -eq ($held | ConvertTo-Json -Depth 15 -Compress)) 'Unknown recovery changed the claim, hold, billing, usage, or result state'
  Assert-UpstreamOnce 'unknown' $false
  Assert-UpstreamOnce 'durable' $true
  $stats = Mock-Request 'stats'
  $unknownAttempt = @($stats.requests | Where-Object { $_.case -eq 'unknown' })[0]
  Assert-True ($stats.requests.Count -eq 2 -and [bool]$unknownAttempt.disconnected_at) 'Unknown request was not actually interrupted, or an unexpected upstream call occurred'
  $null = Invoke-Docker @('exec', $appId, 'test', '!', '-e', "/app/data/batch-images/$unknown.jsonl.partial")
  $null = Invoke-Docker @('exec', $appId, 'test', '!', '-e', "/app/data/batch-images/$unknown.jsonl")
  $evidence['status'] = 'passed'
  $evidence['finished_at'] = [DateTime]::UtcNow.ToString('o')
  Save-Evidence
  $evidence | ConvertTo-Json -Depth 30
} catch {
  $evidence['status'] = 'failed'
  $evidence['failure'] = $_.Exception.Message
  Save-Evidence
  throw
} finally {
  try { Stop-RowLock } catch { Write-Warning "Exact test row-lock release failed: $($_.Exception.Message)" }
  if ($script:appStoppedByHarness) {
    try { Start-App } catch { Write-Warning "Exact candidate app remains stopped: $($_.Exception.Message)" }
  }
  if ($script:mockStarted) {
    try { (Mock-Request 'stats') | ConvertTo-Json -Depth 15 | Set-Content -LiteralPath (Join-Path $artifactDir 'upstream-final.json') -Encoding utf8 } catch { Write-Warning 'Could not save final synthetic upstream statistics.' }
  }
  Write-Host "Recovery artifacts: $artifactDir. Synthetic fixtures, including any unknown hold, are retained; no cleanup/refund was performed."
}
