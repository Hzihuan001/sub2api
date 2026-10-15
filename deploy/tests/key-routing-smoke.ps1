[CmdletBinding()]
param(
  [switch]$Execute,
  [string]$Project = 'sub2api-operator-test-key-routing-20261015',
  [int]$Port = 18097,
  [int]$MockPort = 18098,
  [string]$CandidateImage = '',
  [string]$ExpectedImageId = '',
  [string]$ExpectedCommit = '',
  [string]$OldImage = 'ghcr.io/hzihuan001/sub2api@sha256:cb074f11766d9190f4e5f3479a0750c7a759a562bfc142aa3067a0d4bfbfc42e',
  [string]$OutputDirectory = ''
)

# Seven fixed gateway requests against synthetic upstreams on a NEW internal
# Docker network. Never builds/pulls, never contacts a provider, never reuses
# an app's data. The project and redacted evidence remain available afterwards.
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 required' }
if (-not $Execute) {
  Write-Output 'Prepared only. Use -Execute -CandidateImage <local tag> -ExpectedImageId sha256:<image> -ExpectedCommit <40-char commit>. Creates a fresh isolated project, performs 7 synthetic requests, restarts, reads through custom.6, and restores the candidate. Retains project/evidence.'
  return
}
function Assert-True([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Docker {
  $arguments = $args
  $result = & docker.exe @arguments
  if ($LASTEXITCODE -ne 0) { throw "Docker failed: $($arguments[0])" }
  return $result
}
Assert-True ($Project -match '^sub2api-operator-test-key-routing-[a-zA-Z0-9_-]+$') 'A dedicated new key-routing project is required'
Assert-True ($Port -ge 1024 -and $Port -le 65535 -and $MockPort -ge 1024 -and $MockPort -le 65535 -and $Port -ne $MockPort) 'Invalid ports'
Assert-True ($CandidateImage -and $ExpectedImageId -match '^sha256:[a-f0-9]{64}$' -and $ExpectedCommit -match '^[a-f0-9]{40}$') 'Exact image ID and full commit required'
Assert-True ($OldImage -eq 'ghcr.io/hzihuan001/sub2api@sha256:cb074f11766d9190f4e5f3479a0750c7a759a562bfc142aa3067a0d4bfbfc42e') 'Rollback must use the specified immutable custom.6 release'
Assert-True ((Docker image inspect $CandidateImage --format '{{.Id}}').Trim() -eq $ExpectedImageId) 'Candidate image ID mismatch'
$oldImageId = (Docker image inspect $OldImage --format '{{.Id}}').Trim()
foreach ($image in @('node:24-alpine', 'postgres:18-alpine', 'redis:8-alpine')) { $null = Docker image inspect $image }
$deployDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$composeFile = Join-Path $deployDir 'docker-compose.operator-test.yml'
$fixture = Join-Path $deployDir 'acceptance/mock-failover-upstream.mjs'
$network = "${Project}_default"
Assert-True (-not @(Docker ps -aq --filter "label=com.docker.compose.project=$Project").Count) 'Project already exists'
Assert-True (-not @(Docker network ls -q --filter "name=^${network}$").Count) 'Project network already exists'
foreach ($name in @("${Project}_app_data", "${Project}_postgres_data", "${Project}_redis_data")) {
  Assert-True (-not @(Docker volume ls -q --filter "name=^${name}$").Count) "Volume already exists: $name"
}
foreach ($testPort in @($Port, $MockPort)) {
  $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $testPort)
  try { $listener.Start() } finally { $listener.Stop() }
}
$run = [Guid]::NewGuid().ToString('N').Substring(0, 10)
if (-not $OutputDirectory) { $OutputDirectory = Join-Path ([IO.Path]::GetTempPath()) "key-routing-smoke-$run" }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
Assert-True (-not (Test-Path -LiteralPath $OutputDirectory)) 'Evidence directory must be new'
$composeOverride = @"
services:
  sub2api:
    cpus: 2
    mem_limit: 2g
  key-routing-mock:
    image: node:24-alpine
    command: ["node", "/fixture/server.mjs"]
    ports:
      - "127.0.0.1:${MockPort}:8080"
    volumes:
      - ./acceptance/mock-failover-upstream.mjs:/fixture/server.mjs:ro
    healthcheck:
      test: ["CMD", "node", "-e", "fetch('http://localhost:8080/health').then(r=>{if(!r.ok)process.exit(1)})"]
      interval: 2s
      timeout: 3s
      retries: 20
networks:
  default:
    internal: true
"@
function Compose {
  $arguments = $args
  $composeOverride | & docker.exe compose --project-name $Project --project-directory $deployDir --file $composeFile --file - @arguments
  if ($LASTEXITCODE -ne 0) { throw "Compose failed: $($arguments -join ' ')" }
}
$httpClient = @'
const fs = require('node:fs');
const p = JSON.parse(fs.readFileSync(0, 'utf8'));
if (!['http://sub2api:8080', 'http://key-routing-mock:8080'].includes(p.origin) || !p.path.startsWith('/')) throw new Error('Non-fixture URL');
(async () => {
  const headers = {};
  if (p.token) headers.authorization = 'Bearer ' + p.token;
  if (p.body !== null) headers['content-type'] = 'application/json';
  const r = await fetch(p.origin + p.path, {method:p.method, headers, body:p.body === null ? undefined : JSON.stringify(p.body), redirect:'error', signal:AbortSignal.timeout(45000)});
  process.stdout.write(JSON.stringify({status:r.status, content:await r.text()}));
})().catch(e => { process.stderr.write(e.message); process.exitCode = 1; });
'@
$containerIds = @{}
$currentImageId = $ExpectedImageId
$gatewayRequests = 0
$evidence = [ordered]@{
  status = 'running'; project = $Project; started_at = [DateTime]::UtcNow.ToString('o')
  candidate_image = $ExpectedImageId; candidate_commit = $ExpectedCommit; rollback_image = $OldImage
  fixture_sha256 = (Get-FileHash -LiteralPath $fixture -Algorithm SHA256).Hash.ToLowerInvariant()
  limits = @{ gateway_requests = 7; successful_upstream_requests = 5; external_network = $false }
  phases = [Collections.Generic.List[object]]::new(); lifecycle = [Collections.Generic.List[object]]::new()
  resources = @{}; retained = $true
}
function Save-Evidence {
  $evidence.gateway_requests = $script:gatewayRequests
  $evidence | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath (Join-Path $OutputDirectory 'evidence.json') -Encoding utf8
}
function Http([string]$Method, [string]$Path, [string]$Token = '', [object]$Body = $null, [string]$Origin = 'http://sub2api:8080') {
  $payload = @{ method=$Method; path=$Path; token=$Token; body=$Body; origin=$Origin } | ConvertTo-Json -Depth 20 -Compress
  $raw = $payload | & docker.exe exec -i $containerIds['key-routing-mock'] node -e $httpClient
  if ($LASTEXITCODE -ne 0) { throw "Fixture HTTP transport failed: $Method $Path; not retried" }
  $result = ($raw -join "`n") | ConvertFrom-Json -AsHashtable
  $json = $null
  if ($result.content) { try { $json = $result.content | ConvertFrom-Json -AsHashtable -NoEnumerate } catch {} }
  return @{ Status=[int]$result.status; Json=$json; Content=[string]$result.content }
}
function Api([string]$Method, [string]$Path, [string]$Token = '', [object]$Body = $null, [int]$Expected = 200) {
  $result = Http $Method "/api/v1$Path" $Token $Body
  Assert-True ($result.Status -eq $Expected) "$Method $Path expected $Expected, got $($result.Status)"
  return $result
}
function Login([string]$Email, [string]$Password) {
  $token = [string](Api POST '/auth/login' '' @{email=$Email;password=$Password}).Json.data.access_token
  Assert-True ([bool]$token) 'Login returned no token'
  return $token
}
function Assert-OwnedProject([switch]$AllowAppReplacement) {
  $net = (((Docker network inspect $network) -join "`n") | ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
  Assert-True ($net.Internal -and $net.Labels['com.docker.compose.project'] -eq $Project) 'Wrong or externally connected project network'
  foreach ($service in @('sub2api','postgres','redis','key-routing-mock')) {
    $ids = @(Docker ps -aq --filter "label=com.docker.compose.project=$Project" --filter "label=com.docker.compose.service=$service")
    Assert-True ($ids.Count -eq 1) "Expected one $service container"
    $item = (((Docker inspect $ids[0]) -join "`n") | ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
    Assert-True ($item.Config.Labels['com.docker.compose.project'] -eq $Project -and $item.NetworkSettings.Networks.Count -eq 1 -and $item.NetworkSettings.Networks.ContainsKey($network)) "Unexpected ownership/network: $service"
    if ($containerIds.ContainsKey($service) -and -not ($AllowAppReplacement -and $service -eq 'sub2api')) {
      Assert-True ($containerIds[$service] -eq $item.Id) "Unexpected replacement: $service"
    }
    $containerIds[$service] = $item.Id
    if ($service -eq 'sub2api') { Assert-True ($item.Image -eq $script:currentImageId) 'Unexpected application image' }
    if ($service -ne 'key-routing-mock') {
      $volume = switch ($service) { 'sub2api' {"${Project}_app_data"} 'postgres' {"${Project}_postgres_data"} 'redis' {"${Project}_redis_data"} }
      Assert-True (@($item.Mounts | Where-Object {$_.Type -eq 'volume' -and $_.Name -eq $volume}).Count -eq 1) "Unexpected data volume: $service"
    }
  }
  $version = (Docker exec $containerIds['sub2api'] /app/sub2api --version) -join "`n"
  if ($script:currentImageId -eq $ExpectedImageId) { Assert-True ($version.Contains($ExpectedCommit)) 'Candidate runtime commit mismatch' }
  $evidence.current_runtime_version = $version
  $evidence.containers = $containerIds.Clone()
}
function Wait-App {
  $deadline = [DateTime]::UtcNow.AddSeconds(120)
  do {
    try { if ((Http GET '/health').Status -eq 200) { return } } catch {}
    Start-Sleep -Seconds 2
  } while ([DateTime]::UtcNow -lt $deadline)
  throw 'App health wait expired'
}
function Restart-App([string]$Label) {
  Assert-OwnedProject
  $before = (Docker inspect $containerIds['sub2api'] --format '{{.State.StartedAt}}').Trim()
  $null = Docker restart --time 10 $containerIds['sub2api']
  Wait-App
  Assert-OwnedProject
  $after = (Docker inspect $containerIds['sub2api'] --format '{{.State.StartedAt}}').Trim()
  Assert-True ($before -ne $after) 'Restart did not start a new process'
  $evidence.lifecycle.Add(@{label=$Label;image=$script:currentImageId;before=$before;after=$after})
  Save-Evidence
}
function Switch-App([string]$Image, [string]$ImageId, [string]$Label) {
  Assert-OwnedProject
  $env:SUB2API_TEST_IMAGE = $Image
  $script:currentImageId = $ImageId
  $null = Compose up --detach --no-build --pull never --no-deps --force-recreate --wait --wait-timeout 180 sub2api
  Assert-OwnedProject -AllowAppReplacement
  Wait-App
  $evidence.lifecycle.Add(@{label=$Label;image=$ImageId;runtime=$evidence.current_runtime_version})
  Save-Evidence
}
function Sql([string]$Query) {
  $raw = $Query | & docker.exe exec -i $containerIds['postgres'] psql -X -qAt -U sub2api_operator_test -d sub2api_operator_test --set=ON_ERROR_STOP=1
  if ($LASTEXITCODE -ne 0) { throw 'Isolated fixture SQL failed' }
  return ($raw -join "`n").Trim()
}
function Route-Rows { return Sql "SELECT COALESCE(json_agg(group_id ORDER BY position),'[]'::json) FROM custom_api_key_routes WHERE api_key_id=$keyId;" }
function Assert-Routes([long[]]$Expected) {
  $keyData = (Api GET "/keys/$keyId" $userToken).Json.data
  Assert-True ($keyData.group_id -eq $primaryGroup -and (($keyData.backup_group_ids -join ',') -eq ($Expected -join ','))) 'Key API route order differs'
  $stored = (Route-Rows) | ConvertFrom-Json -NoEnumerate
  Assert-True (($stored -join ',') -eq ($Expected -join ',')) 'Persisted route order differs'
}
function Stats {
  $result = Http GET '/stats' '' $null 'http://key-routing-mock:8080'
  Assert-True ($result.Status -eq 200) 'Fixture stats unavailable'
  $rows = @($result.Json)
  foreach ($row in $rows) { Assert-True ($row.scenario -eq 'success' -and $row.path -eq '/v1/responses' -and $row.slot -in @('primary','backup','third')) 'Unexpected synthetic upstream call' }
  return ,$rows
}
function Ledger {
  return (Sql @"
SELECT json_build_object('balance',u.balance,'quota_used',k.quota_used,
 'usage_count',(SELECT COUNT(*) FROM usage_logs WHERE api_key_id=k.id),
 'dedup_count',(SELECT COUNT(*) FROM usage_billing_dedup WHERE api_key_id=k.id),
 'last_usage',(SELECT row_to_json(x) FROM (SELECT id,api_key_id,group_id,account_id,input_tokens,output_tokens,total_cost,actual_cost,rate_multiplier FROM usage_logs WHERE api_key_id=k.id ORDER BY id DESC LIMIT 1) x))
FROM users u JOIN api_keys k ON k.user_id=u.id WHERE u.id=$userId AND k.id=$keyId;
"@) | ConvertFrom-Json -AsHashtable
}
function Gateway([string]$Label,[string]$Slot,[long]$Group,[decimal]$Rate,[bool]$Stream=$false,[string]$Path='/v1/responses',[int]$Status=200) {
  Assert-True ($script:gatewayRequests -lt 7) 'Fixed gateway budget exhausted; do not retry'
  $beforeStats = Stats
  $before = Ledger
  $body = if ($Path -eq '/v1/images/generations') { @{model='gpt-image-2';prompt='synthetic primary-only';n=1;size='1024x1024'} } else { @{model='gpt-4o-mini';input="synthetic route $run $Label";stream=$Stream} }
  $script:gatewayRequests++
  Save-Evidence
  $result = Http POST $Path $apiKey $body
  $afterStats = Stats
  $newRows = @($afterStats | Select-Object -Skip $beforeStats.Count)
  $evidence.last_attempt = @{label=$Label;status=$result.Status;upstream=$newRows}
  Save-Evidence
  Assert-True ($result.Status -eq $Status) "$Label expected HTTP $Status, got $($result.Status)"
  if ($Status -eq 200) {
    Assert-True ($result.Content.Contains('backup-ok') -and $newRows.Count -eq 1 -and $newRows[0].slot -eq $Slot -and $newRows[0].stream -eq $Stream) "$Label selected wrong group or replayed upstream"
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    do { $after = Ledger; if ($after.usage_count -eq ($before.usage_count+1)) { break }; Start-Sleep -Milliseconds 250 } while ([DateTime]::UtcNow -lt $deadline)
    $usage = $after.last_usage
    Assert-True ($after.usage_count -eq ($before.usage_count+1) -and $after.dedup_count -eq ($before.dedup_count+1)) "$Label did not settle exactly once"
    Assert-True ($usage.api_key_id -eq $keyId -and $usage.group_id -eq $Group -and $usage.input_tokens -eq 10 -and $usage.output_tokens -eq 2) "$Label usage group/key/tokens mismatch"
    Assert-True ([decimal]$usage.rate_multiplier -eq $Rate -and [decimal]$usage.total_cost -gt 0) "$Label effective rate mismatch"
    $expectedCost = [Math]::Round(([decimal]$usage.total_cost*$Rate),8,[MidpointRounding]::AwayFromZero)
    Assert-True ([decimal]$before.balance-[decimal]$after.balance -eq $expectedCost -and [decimal]$after.quota_used-[decimal]$before.quota_used -eq $expectedCost) "$Label PostgreSQL balance/quota debit mismatch"
    Assert-True ([Math]::Abs([decimal]$usage.actual_cost-$expectedCost) -le [decimal]'0.00000001') "$Label usage cost mismatch"
    $evidence.phases.Add(@{label=$Label;status=$Status;selected_group=$Group;slot=$Slot;rate=$Rate;charged=$expectedCost;usage=$usage;ledger=$after})
  } else {
    Start-Sleep -Milliseconds 400
    $after = Ledger
    Assert-True ($newRows.Count -eq 0 -and $after.usage_count -eq $before.usage_count -and $after.dedup_count -eq $before.dedup_count -and $after.balance -eq $before.balance -and $after.quota_used -eq $before.quota_used) "$Label rejection reached upstream or billing"
    $evidence.phases.Add(@{label=$Label;status=$Status;upstream_attempts=0;billed=$false})
  }
  $evidence.upstream = $afterStats
  Save-Evidence
}
function Credentials([string]$Slot,[bool]$SupportsText=$true) {
  $mapping = if ($SupportsText) { @{'gpt-4o-mini'='gpt-4o-mini';'gpt-image-2'='gpt-image-2'} } else { @{'unrelated-model'='unrelated-model'} }
  return @{api_key="success:$Slot";base_url='http://key-routing-mock:8080/v1';model_mapping=$mapping}
}
function Cool-Accounts([long[]]$Ids) {
  Assert-OwnedProject
  $list = $Ids -join ','
  $null = Sql "BEGIN; UPDATE accounts SET temp_unschedulable_until=NOW()+INTERVAL '30 minutes',temp_unschedulable_reason='isolated key route fixture',updated_at=NOW() WHERE id IN ($list); INSERT INTO scheduler_outbox(event_type,account_id) SELECT 'account_changed',id FROM accounts WHERE id IN ($list); COMMIT;"
  Start-Sleep -Seconds 3
}

$previousPort = [Environment]::GetEnvironmentVariable('SUB2API_TEST_PORT','Process')
$previousImage = [Environment]::GetEnvironmentVariable('SUB2API_TEST_IMAGE','Process')
$null = New-Item -ItemType Directory -Path $OutputDirectory
try {
  $env:SUB2API_TEST_PORT=[string]$Port
  $env:SUB2API_TEST_IMAGE=$CandidateImage
  Save-Evidence
  $null = Compose config --quiet
  $null = Compose up --detach --no-build --pull never --wait --wait-timeout 180
  Assert-OwnedProject
  Wait-App
  $adminToken = Login 'admin@operator-test.local' 'Operator-Test-Admin-Password-2026!'
  $compliance = (Api GET '/admin/compliance' $adminToken).Json.data
  if ($compliance.required) { $null = Api POST '/admin/compliance/accept' $adminToken @{phrase=$compliance.ack_phrase_en;language='en'} }
  $email="key-routing-$run@operator-test.local"
  $password='Key-Routing-Synthetic-2026!'
  $userId=[long](Api POST '/admin/users' $adminToken @{email=$email;password=$password;role='user';balance=10;concurrency=5}).Json.data.id
  $userToken=Login $email $password
  $groupIds=@();$accountIds=@()
  foreach ($slot in @('primary','backup','third')) {
    $rate=switch($slot){'primary'{2}'backup'{0.5}'third'{0.75}}
    $group=[long](Api POST '/admin/groups' $adminToken @{name="key-route-$run-$slot";platform='openai';subscription_type='standard';rate_multiplier=$rate;allow_image_generation=($slot -ne 'primary');image_price_1k=0.01;image_price_2k=0.01;image_price_4k=0.01}).Json.data.id
    Assert-True ($group -gt 0) 'Invalid synthetic group'
    $groupIds += $group
    # Passthrough intentionally supports every model, regardless of an old
    # model_mapping. The primary must use whitelist mode for this scenario.
    $account=[long](Api POST '/admin/accounts' $adminToken @{name="key-route-$run-$slot";platform='openai';type='apikey';concurrency=5;priority=0;rate_multiplier=1;group_ids=@($group);extra=@{openai_passthrough=($slot -ne 'primary');openai_responses_mode='force_responses'};credentials=(Credentials $slot ($slot -ne 'primary'))}).Json.data.id
    Assert-True ($account -gt 0) 'Invalid synthetic account'
    $accountIds += $account
  }
  $primaryGroup=$groupIds[0];$backupGroup=$groupIds[1];$thirdGroup=$groupIds[2]
  $primaryId=$accountIds[0]
  $created=(Api POST '/keys' $userToken @{name="key-routing-$run";group_id=$primaryGroup;backup_group_ids=@($backupGroup,$thirdGroup);quota=1}).Json.data
  $keyId=[long]$created.id;$apiKey=[string]$created.key
  Assert-True ($userId -gt 0 -and $keyId -gt 0 -and [bool]$apiKey) 'Invalid synthetic owner/key'
  $evidence.resources=@{user=$userId;key=$keyId;groups=$groupIds;accounts=$accountIds}
  Assert-Routes @($backupGroup,$thirdGroup)
  $null=Api PUT "/keys/$keyId" $userToken @{name="key-routing-$run-renamed"}
  Assert-Routes @($backupGroup,$thirdGroup)
  $null=Api PUT "/keys/$keyId" $userToken @{backup_group_ids=@()}
  Assert-Routes @()
  $null=Api PUT "/keys/$keyId" $userToken @{backup_group_ids=@($backupGroup,$thirdGroup)}
  Assert-Routes @($backupGroup,$thirdGroup)
  Start-Sleep -Seconds 3
  Assert-True ((Stats).Count -eq 0) 'Unexpected pre-scenario upstream request'
  Gateway 'primary-model-unsupported' 'backup' $backupGroup 0.5
  $null=Api PUT "/keys/$keyId" $userToken @{backup_group_ids=@($thirdGroup,$backupGroup)}
  Assert-Routes @($thirdGroup,$backupGroup)
  Gateway 'backup-order-updated' 'third' $thirdGroup 0.75
  $null=Api PUT "/keys/$keyId" $userToken @{backup_group_ids=@($backupGroup,$thirdGroup)}
  $null=Api PUT "/admin/accounts/$primaryId" $adminToken @{credentials=(Credentials 'primary' $true)}
  Cool-Accounts @($primaryId)
  Gateway 'primary-all-cooling-sse' 'backup' $backupGroup 0.5 $true
  $null=Api PUT "/admin/users/$userId" $adminToken @{group_rates=@{"$backupGroup"=0.25}}
  $routesBefore=Route-Rows
  Restart-App 'extension-and-rate-config-restart'
  Assert-Routes @($backupGroup,$thirdGroup)
  Assert-True ((Route-Rows) -eq $routesBefore) 'Restart changed persisted routes'
  Gateway 'selected-group-user-rate' 'backup' $backupGroup 0.25
  $null=Api POST "/admin/accounts/$primaryId/recover-state" $adminToken
  Start-Sleep -Seconds 3
  Gateway 'primary-recovered' 'primary' $primaryGroup 2
  Cool-Accounts $accountIds
  Gateway 'all-groups-unavailable' '' 0 0 $false '/v1/responses' 503
  Gateway 'images-primary-only' '' 0 0 $false '/v1/images/generations' 403
  Assert-True ($gatewayRequests -eq 7 -and (Stats).Count -eq 5) 'Fixed request/attempt totals differ'
  $ledgerBefore=Ledger
  $migrationBefore=Sql "SELECT checksum FROM schema_migrations WHERE filename='9000_custom_api_key_routes.sql';"
  Assert-True ($migrationBefore -match '^[a-f0-9]{64}$') 'Custom route migration is not applied'
  $routesBefore=Route-Rows
  Switch-App $OldImage $oldImageId 'rollback-custom6-readability'
  $legacyKey=(Api GET "/keys/$keyId" $userToken).Json.data
  Assert-True ($legacyKey.id -eq $keyId -and $legacyKey.group_id -eq $primaryGroup) 'Old release cannot read primary key binding'
  Assert-True ((Route-Rows) -eq $routesBefore) 'Old release changed extension rows'
  Assert-True ((Ledger | ConvertTo-Json -Depth 10 -Compress) -eq ($ledgerBefore | ConvertTo-Json -Depth 10 -Compress)) 'Rollback changed fixture money state'
  Switch-App $CandidateImage $ExpectedImageId 'candidate-restored'
  Assert-Routes @($backupGroup,$thirdGroup)
  Assert-True ((Sql "SELECT checksum FROM schema_migrations WHERE filename='9000_custom_api_key_routes.sql';") -eq $migrationBefore) 'Migration checksum changed across rollback/restart'
  Assert-True ((Stats).Count -eq 5) 'Lifecycle verification generated an upstream call'
  $evidence.final_ledger=Ledger
  $evidence.migration_checksum=$migrationBefore
  $evidence.status='passed'
} catch {
  $evidence.status='failed';$evidence.error=$_.Exception.Message
  throw
} finally {
  $evidence.finished_at=[DateTime]::UtcNow.ToString('o')
  Save-Evidence
  [Environment]::SetEnvironmentVariable('SUB2API_TEST_PORT',$previousPort,'Process')
  [Environment]::SetEnvironmentVariable('SUB2API_TEST_IMAGE',$previousImage,'Process')
  Write-Output "Evidence: $(Join-Path $OutputDirectory 'evidence.json'); project retained: $Project; gateway calls: $gatewayRequests/7"
}
