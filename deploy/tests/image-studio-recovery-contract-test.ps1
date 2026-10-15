[CmdletBinding()]
param()

# Deterministic script-level regressions. Import function ASTs only: never execute
# the harness's top-level Docker/API orchestration, and require no running stack.
if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 or newer is required.' }
$ErrorActionPreference = 'Stop'
$source = Join-Path $PSScriptRoot 'image-studio-recovery.ps1'
$tokens = $null
$parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($source, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count) { throw ($parseErrors -join "`n") }
foreach ($name in @('Assert-True', 'Assert-OwnedDataMount', 'Assert-Container', 'Get-QueueSnapshot', 'Queue-Drained', 'Assert-KeyUsage', 'Snapshot')) {
  $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name }, $true)
  if (-not $definition) { throw "Missing production harness function $name" }
  Invoke-Expression $definition.Extent.Text
}
$script:passed = [Collections.Generic.List[string]]::new()
function Check([string]$Name, [scriptblock]$Action) {
  & $Action
  $script:passed.Add($Name)
}
function Must-Reject([scriptblock]$Action, [string]$Reason) {
  $failure = $null
  try { & $Action | Out-Null } catch { $failure = $_.Exception.Message }
  if (-not $failure -or $failure -notmatch $Reason) { throw "Expected rejection matching '$Reason', got '$failure'" }
}

$Project = 'sub2api-operator-test-modular'
$network = "${Project}_default"
$batch = 'imgbatch_11111111111111111111111111111111'
$keyId = 11
$script:redisCalls = 0
$script:queueState = @{}
function Redis([string[]]$Command) {
  $script:redisCalls++
  # The former multi-command implementation fails here instead of accepting a
  # delayed -> ready transition between reads. The Redis result is one instant.
  Assert-True ($Command.Count -eq 9 -and $Command[0] -eq 'EVAL_RO' -and $Command[2] -eq '5') 'Queue check must issue one read-only atomic snapshot'
  $expectedKeys = @('batch_image:queue:ready', 'batch_image:queue:active', 'batch_image:queue:delayed', "batch_image:queue:lock:$batch", "batch_image:queue:inflight:$batch", $batch)
  Assert-True (($Command[3..8] -join '|') -eq ($expectedKeys -join '|')) 'Atomic snapshot omitted or changed an exact queue key'
  foreach ($key in 1..5) { Assert-True ($Command[1].Contains("KEYS[$key]")) "Lua omitted key $key" }
  foreach ($read in @('LPOS', 'ZSCORE', 'EXISTS')) { Assert-True ($Command[1].Contains("'$read'")) "Lua omitted $read membership read" }
  Assert-True ($Command[1] -notmatch "redis\.call\('(DEL|SET|LPUSH|RPOP|ZADD|ZREM)'" ) 'Snapshot Lua must not mutate Redis'
  return $script:queueState | ConvertTo-Json -Compress
}
function Reset-Queue {
  $script:redisCalls = 0
  $script:queueState = @{ ready = 0; active = 0; delayed = 0; delayed_score = $null; lock = 0; inflight = 0 }
}
Check 'empty atomic snapshot is drained' {
  Reset-Queue
  Assert-True (Queue-Drained $batch) 'Empty queue should be drained'
  Assert-True ($script:redisCalls -eq 1) 'Queue membership was read more than once'
}
foreach ($membership in @('ready', 'active', 'delayed', 'lock', 'inflight')) {
  Check "reject $membership membership" {
    Reset-Queue
    $script:queueState[$membership] = 1
    if ($membership -eq 'delayed') { $script:queueState.delayed_score = 12345 }
    Assert-True (-not (Queue-Drained $batch)) "$membership must prevent a false ACK"
    Assert-True ($script:redisCalls -eq 1) 'Membership was not a single atomic read'
  }
}
Check 'delayed-to-ready snapshots cannot be mistaken for an ACK' {
  Reset-Queue
  $script:queueState.delayed = 1
  Assert-True (-not (Queue-Drained $batch)) 'Delayed work was mistaken for an ACK'
  Reset-Queue
  $script:queueState.ready = 1
  Assert-True (-not (Queue-Drained $batch)) 'Moved ready work was mistaken for an ACK'
}
Check 'missing inflight evidence fails closed' {
  Reset-Queue
  $script:queueState.Remove('inflight')
  Must-Reject { Queue-Drained $batch } 'Malformed atomic queue'
}

$script:info = @{}
$script:volumes = @{}
$script:actualPgData = ''
$script:actualRedisDir = ''
function Inspect-Container([string]$Id) { return $script:info }
function Invoke-Docker([string[]]$Arguments) {
  # This replaces the harness transport completely. Unexpected operations throw;
  # there is intentionally no fallback to the real Docker executable.
  if ($Arguments.Count -eq 3 -and $Arguments[0] -eq 'volume' -and $Arguments[1] -eq 'inspect') {
    Assert-True ($script:volumes.ContainsKey($Arguments[2])) 'Unexpected volume fixture'
    return ConvertTo-Json -InputObject @($script:volumes[$Arguments[2]]) -Depth 12 -Compress
  }
  if ($Arguments[0] -eq 'exec' -and $Arguments[2] -eq 'readlink' -and $Arguments[3] -eq '-f' -and $Arguments.Count -eq 5) { return $Arguments[4] }
  if ($Arguments[0] -eq 'exec' -and $Arguments[2] -eq 'psql' -and $Arguments[-1] -eq 'SHOW data_directory;') { return $script:actualPgData }
  if ($Arguments.Count -eq 9 -and $Arguments[0] -eq 'exec' -and ($Arguments[2..8] -join ' ') -eq 'redis-cli --raw -n 0 CONFIG GET dir') { return "dir`n$script:actualRedisDir" }
  throw "Forbidden/unexpected Docker fixture call: $($Arguments -join ' ')"
}
function Reset-Service([string]$Service) {
  $root = if ($Service -eq 'postgres') { '/var/lib/postgresql/data' } else { '/data' }
  $label = if ($Service -eq 'postgres') { 'postgres_data' } else { 'redis_data' }
  $script:actualPgData = '/var/lib/postgresql/data'
  $script:actualRedisDir = '/data'
  $script:info = @{
    Config = @{ Labels = @{ 'com.docker.compose.project' = $Project; 'com.docker.compose.service' = $Service }; Env = @('PGDATA=/var/lib/postgresql/data') }
    NetworkSettings = @{ Networks = @{ $network = @{} } }; HostConfig = @{ PortBindings = @{} }
    Mounts = @(@{ Type = 'volume'; Name = 'fixture-volume'; Destination = $root; RW = $true })
  }
  $script:volumes = @{ 'fixture-volume' = @{ Driver = 'local'; Options = @{}; Labels = @{ 'com.docker.compose.project' = $Project; 'com.docker.compose.volume' = $label } } }
}
foreach ($service in @('postgres', 'redis')) {
  Check "accept isolated $service actual data volume" {
    Reset-Service $service
    $null = Assert-Container 'fixture-container' $service
  }
  Check "reject foreign-project $service volume despite valid service labels" {
    Reset-Service $service
    $script:volumes['fixture-volume'].Labels['com.docker.compose.project'] = 'other-project'
    Must-Reject { Assert-Container 'fixture-container' $service } 'does not belong'
  }
  Check "reject wrong $service Compose volume role" {
    Reset-Service $service
    $script:volumes['fixture-volume'].Labels['com.docker.compose.volume'] = 'app_data'
    Must-Reject { Assert-Container 'fixture-container' $service } 'does not belong'
  }
  Check "reject $service host bind" {
    Reset-Service $service
    $script:info.Mounts[0].Type = 'bind'
    Must-Reject { Assert-Container 'fixture-container' $service } 'bind, anonymous, or read-only'
  }
  Check "reject $service bind-backed named volume" {
    Reset-Service $service
    $script:volumes['fixture-volume'].Options = @{ type = 'none'; o = 'bind'; device = '/outside/data' }
    Must-Reject { Assert-Container 'fixture-container' $service } 'External/bind-backed'
  }
  Check "reject $service nested foreign mount" {
    Reset-Service $service
    $script:info.Mounts += @{ Type = 'volume'; Name = 'foreign-child'; Destination = "$($script:info.Mounts[0].Destination)/child"; RW = $true }
    $script:volumes['foreign-child'] = @{ Driver = 'local'; Options = @{}; Labels = @{ 'com.docker.compose.project' = 'other-project'; 'com.docker.compose.volume' = 'postgres_data' } }
    Must-Reject { Assert-Container 'fixture-container' $service } 'does not belong'
  }
  Check "reject $service actual data path outside declared mount" {
    Reset-Service $service
    if ($service -eq 'postgres') {
      $script:actualPgData = '/outside/data'
      $script:info.Config.Env = @('PGDATA=/outside/data')
    } else { $script:actualRedisDir = '/outside/data' }
    Must-Reject { Assert-Container 'fixture-container' $service } 'not backed by a named Compose volume'
  }
}
Check 'reject actual Postgres data_directory override differing from PGDATA' {
  Reset-Service 'postgres'
  $script:actualPgData = '/other/path'
  Must-Reject { Assert-Container 'fixture-container' 'postgres' } 'differs from PGDATA'
}
Check 'accept owned child volume shadowing unrelated Postgres 18 parent mount' {
  Reset-Service 'postgres'
  $script:info.Mounts += @{ Type = 'volume'; Name = 'anonymous-parent'; Destination = '/var/lib/postgresql'; RW = $true }
  $null = Assert-Container 'fixture-container' 'postgres'
}

$expectedCapture = "batch_image_capture:$batch"
function Assert-OwnedBatch([string]$BatchId) { Assert-True ($BatchId -eq $batch) 'Unexpected snapshot batch' }
function Invoke-Sql([string]$Sql) {
  # Ensure key-wide fields really query all key rows, not the expected capture ID.
  foreach ($fragment in @(
    "'key_usage_rows',(SELECT count(*) FROM usage_logs WHERE api_key_id=$keyId)",
    "'key_usage_cost',(SELECT COALESCE(sum(actual_cost),0) FROM usage_logs WHERE api_key_id=$keyId)",
    "SELECT DISTINCT request_id FROM usage_logs WHERE api_key_id=$keyId")) {
    Assert-True ($Sql.Contains($fragment)) 'Snapshot omitted unfiltered synthetic-key usage evidence'
  }
  return '{"key_usage_rows":2,"key_usage_cost":0.02,"key_usage_request_ids":["unexpected-request","another-request"]}'
}
Check 'snapshot captures alternate-request-ID key usage and rejects it' {
  $state = Snapshot $batch
  Must-Reject { Assert-KeyUsage $state 1 0.01 @($expectedCapture) } 'total synthetic-key usage'
}
Check 'expected count/cost cannot hide an unexpected usage request ID' {
  $state = @{ key_usage_rows = 1; key_usage_cost = 0.01; key_usage_request_ids = @('unexpected-request') }
  Must-Reject { Assert-KeyUsage $state 1 0.01 @($expectedCapture) } 'unexpected request IDs'
}
Check 'zero usage before durable result is accepted' {
  Assert-KeyUsage @{ key_usage_rows = 0; key_usage_cost = 0; key_usage_request_ids = @() } 0 0 @()
}
Check 'exactly one durable usage row is accepted' {
  Assert-KeyUsage @{ key_usage_rows = 1; key_usage_cost = 0.01; key_usage_request_ids = @($expectedCapture) } 1 0.01 @($expectedCapture)
}
[pscustomobject]@{ status = 'passed'; checks = $script:passed.Count; cases = @($script:passed); docker_commands = 0 } | ConvertTo-Json -Depth 4
