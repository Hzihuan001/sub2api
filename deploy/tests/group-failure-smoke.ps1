[CmdletBinding()]
param(
  [switch]$Execute,
  [string]$Project = 'sub2api-operator-test-group-20261015',
  [int]$Port = 18095,
  [int]$MockPort = 18096,
  [string]$CandidateImage = '',
  [string]$ExpectedImageId = '',
  [string]$ExpectedCommit = '',
  [string]$OutputDirectory = ''
)

# Correctness only: eight gateway requests, no warmup/load/retry of a test step.
# Uses the existing synthetic upstream. Internal networking prevents external
# provider access; Docker exec runs clients inside that network. Ports are only
# reserved Compose metadata (Docker Desktop may not publish internal networks).
# The fresh project and synthetic evidence are retained, including on failure.
# No existing project, volume, account, or unknown image task is reused/deleted.
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 is required' }
if (-not $Execute) {
  Write-Output 'Prepared only. Pass -Execute plus an exact prebuilt local image, image ID and commit. Creates a NEW group-only project; retains it and evidence. Maximum 8 gateway requests / 23 expected upstream attempts; no performance measurement.'
  return
}
if ($Project -notmatch '^sub2api-operator-test-group-[a-zA-Z0-9_-]+$') { throw 'A new group-only isolated project is required' }
if ($Port -lt 1024 -or $Port -gt 65535 -or $MockPort -lt 1024 -or $MockPort -gt 65535 -or $Port -eq $MockPort) { throw 'Invalid distinct ports' }
if ($Port -in @(18080,18086,18088,18089,18090,18091,18092,18093,18094) -or $MockPort -in @(18080,18086,18088,18089,18090,18091,18092,18093,18094)) { throw 'Existing acceptance ports must not be reused' }
if (-not $CandidateImage -or $ExpectedImageId -notmatch '^sha256:[a-f0-9]{64}$' -or $ExpectedCommit -notmatch '^[a-f0-9]{40}$') { throw 'Exact local image name, sha256 image ID and 40-character runtime commit are required' }

$deployDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$composeFile = Join-Path $deployDir 'docker-compose.operator-test.yml'
$fixture = Join-Path $deployDir 'acceptance/mock-failover-upstream.mjs'
$network = "${Project}_default"
$run = [Guid]::NewGuid().ToString('N').Substring(0, 10)
if (-not $OutputDirectory) { $OutputDirectory = Join-Path ([IO.Path]::GetTempPath()) "group-failure-smoke-$run" }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
if (Test-Path -LiteralPath $OutputDirectory) { throw 'Evidence directory must be new; refusing to overwrite an earlier run' }

function Assert-True([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Docker {
  # Keep native flags verbatim; an advanced function would consume psql -d as
  # PowerShell's common -Debug parameter before it reaches Docker.
  $Arguments = $args
  $output = & docker.exe @Arguments
  if ($LASTEXITCODE -ne 0) { throw "Docker failed: $($Arguments[0])" }
  return $output
}

# All images must already exist: this runner never builds or pulls.
$actualImage = (Docker image inspect $CandidateImage --format '{{.Id}}').Trim()
Assert-True ($actualImage -eq $ExpectedImageId) 'Local image ID differs from the approved candidate'
foreach ($imageName in @('node:24-alpine', 'postgres:18-alpine', 'redis:8-alpine')) { $null = Docker image inspect $imageName }
Assert-True (-not @(Docker ps -aq --filter "label=com.docker.compose.project=$Project").Count) 'Project containers already exist'
Assert-True (-not @(Docker network ls -q --filter "name=^${network}$").Count) 'Project network already exists'
Assert-True (-not @(Docker volume ls -q --filter "label=com.docker.compose.project=$Project").Count) 'Project volumes already exist'
foreach ($volumeName in @("${Project}_app_data", "${Project}_postgres_data", "${Project}_redis_data")) {
  Assert-True (-not @(Docker volume ls -q --filter "name=^${volumeName}$").Count) "Volume already exists: $volumeName"
}
foreach ($testPort in @($Port, $MockPort)) {
  $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $testPort)
  try { $listener.Start() } finally { $listener.Stop() }
}

$composeOverride = @"
services:
  sub2api:
    cpus: 2
    mem_limit: 2g
  group-failure-mock:
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
  $Arguments = $args
  $composeOverride | & docker.exe compose --project-name $Project --project-directory $deployDir --file $composeFile --file - @Arguments
  if ($LASTEXITCODE -ne 0) { throw "Compose failed: $($Arguments -join ' ')" }
}

# Request tokens travel only through stdin and are never included in evidence or
# command arguments. No HTTP request is retried by this client.
$httpClient = @'
const fs = require('node:fs');
const p = JSON.parse(fs.readFileSync(0, 'utf8'));
if (!['http://sub2api:8080', 'http://group-failure-mock:8080'].includes(p.origin) || !p.path.startsWith('/')) throw new Error('Non-fixture URL');
(async () => {
  const headers = {};
  if (p.token) headers.authorization = 'Bearer ' + p.token;
  if (p.body !== null) headers['content-type'] = 'application/json';
  const r = await fetch(p.origin + p.path, {method:p.method, headers, body:p.body === null ? undefined : JSON.stringify(p.body), redirect:'error', signal:AbortSignal.timeout(45000)});
  const content = await r.text();
  process.stdout.write(JSON.stringify({status:r.status, content}));
})().catch(e => { process.stderr.write(e.message); process.exitCode = 1; });
'@
$containerIds = @{}
$adminToken = ''
$operatorPolicy = $null
$gatewayRequests = 0
$evidence = [ordered]@{
  status = 'running'; project = $Project; started_at = [DateTime]::UtcNow.ToString('o')
  candidate_image = $ExpectedImageId; candidate_commit = $ExpectedCommit
  fixture_sha256 = (Get-FileHash -LiteralPath $fixture -Algorithm SHA256).Hash.ToLowerInvariant()
  limits = @{ gateway_requests = 8; expected_upstream_attempts = 23; performance_measurement = $false }
  phases = [Collections.Generic.List[object]]::new(); restarts = [Collections.Generic.List[object]]::new()
  resources = @{}; gateway_requests = 0; retained = $true
}
function Save-Evidence {
  $evidence.gateway_requests = $script:gatewayRequests
  $evidence | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath (Join-Path $OutputDirectory 'evidence.json') -Encoding utf8
}
function Http([string]$Method, [string]$Path, [string]$Token = '', [object]$Body = $null, [string]$Origin = 'http://sub2api:8080') {
  $payload = @{ method = $Method; path = $Path; token = $Token; body = $Body; origin = $Origin } | ConvertTo-Json -Depth 20 -Compress
  $raw = $payload | & docker.exe exec -i $containerIds['group-failure-mock'] node -e $httpClient
  if ($LASTEXITCODE -ne 0) { throw "Fixture HTTP transport failed: $Method $Path (not retried)" }
  $response = ($raw -join "`n") | ConvertFrom-Json -AsHashtable
  $json = $null
  if ($response.content) { try { $json = $response.content | ConvertFrom-Json -AsHashtable -NoEnumerate } catch { } }
  return @{ Status = [int]$response.status; Json = $json; Content = [string]$response.content }
}
function Api([string]$Method, [string]$Path, [string]$Token = '', [object]$Body = $null, [int]$Expected = 200) {
  $result = Http $Method "/api/v1$Path" $Token $Body
  Assert-True ($result.Status -eq $Expected) "$Method $Path expected HTTP $Expected, received $($result.Status)"
  return $result
}
function Login([string]$Email, [string]$Password) {
  $result = Api POST '/auth/login' '' @{ email = $Email; password = $Password }
  $token = [string]$result.Json.data.access_token
  Assert-True ([bool]$token) 'Login did not return a token'
  return $token
}
function Accept-Compliance([string]$Token) {
  $result = Api GET '/admin/compliance' $Token
  if ($result.Json.data.required) { $null = Api POST '/admin/compliance/accept' $Token @{ phrase = $result.Json.data.ack_phrase_en; language = 'en' } }
}
function Assert-OwnedProject {
  $info = ((Docker network inspect $network) -join "`n") | ConvertFrom-Json -AsHashtable -NoEnumerate
  Assert-True ($info.Count -eq 1 -and $info[0].Internal -and $info[0].Labels['com.docker.compose.project'] -eq $Project) 'Network is not this internal-only project'
  foreach ($service in @('sub2api', 'postgres', 'redis', 'group-failure-mock')) {
    $ids = @(Docker ps -aq --filter "label=com.docker.compose.project=$Project" --filter "label=com.docker.compose.service=$service")
    Assert-True ($ids.Count -eq 1) "Expected exactly one $service container"
    $info = ((Docker inspect $ids[0]) -join "`n") | ConvertFrom-Json -AsHashtable -NoEnumerate
    $item = $info[0]
    Assert-True ($item.Config.Labels['com.docker.compose.project'] -eq $Project -and $item.Config.Labels['com.docker.compose.service'] -eq $service) "Wrong owner: $service"
    Assert-True ($item.NetworkSettings.Networks.Count -eq 1 -and $item.NetworkSettings.Networks.ContainsKey($network)) "Unexpected network: $service"
    if ($containerIds.ContainsKey($service)) { Assert-True ($containerIds[$service] -eq $item.Id) "Unexpected container replacement: $service" }
    $containerIds[$service] = $item.Id
    if ($service -eq 'sub2api') { Assert-True ($item.Image -eq $ExpectedImageId) 'Running app image ID changed' }
    if ($service -ne 'group-failure-mock') {
      $expectedVolume = switch ($service) { 'sub2api' { "${Project}_app_data" } 'postgres' { "${Project}_postgres_data" } 'redis' { "${Project}_redis_data" } }
      Assert-True (@($item.Mounts | Where-Object { $_.Type -eq 'volume' -and $_.Name -eq $expectedVolume }).Count -eq 1) "Unexpected data mount: $service"
      $volume = ((Docker volume inspect $expectedVolume) -join "`n") | ConvertFrom-Json -AsHashtable -NoEnumerate
      Assert-True ($volume[0].Labels['com.docker.compose.project'] -eq $Project) "Wrong volume owner: $service"
    }
  }
  $version = (Docker exec $containerIds['sub2api'] /app/sub2api --version) -join "`n"
  Assert-True ($version.Contains($ExpectedCommit)) 'Running binary does not report the expected full runtime commit'
  $evidence.runtime_version = $version
  $evidence.containers = $containerIds.Clone()
}
function Wait-App {
  $deadline = [DateTime]::UtcNow.AddSeconds(120)
  do {
    try { if ((Http GET '/health').Status -eq 200) { return } } catch { }
    Start-Sleep -Seconds 2
  } while ([DateTime]::UtcNow -lt $deadline)
  throw 'Application did not recover within the bounded health wait'
}
function Restart-App([string]$Label) {
  Assert-OwnedProject
  $before = (Docker inspect $containerIds['sub2api'] --format '{{.State.StartedAt}}').Trim()
  $null = Docker restart --time 10 $containerIds['sub2api']
  Wait-App
  Assert-OwnedProject
  $after = (Docker inspect $containerIds['sub2api'] --format '{{.State.StartedAt}}').Trim()
  Assert-True ($before -ne $after) 'App restart did not start a new process'
  $evidence.restarts.Add(@{ label = $Label; before = $before; after = $after })
  Save-Evidence
}
function Policy([int]$Threshold) { return @{ rules = @{ overload = @{ threshold = $Threshold; window_seconds = 600; cooldown_seconds = 600 } } } }
function Assert-Policy([long]$GroupId, [int]$Threshold) {
  $actual = (Api GET "/admin/groups/$GroupId/failure-policy" $adminToken).Json.data.rules
  Assert-True ($actual.Count -eq 1 -and $actual.overload.threshold -eq $Threshold -and $actual.overload.window_seconds -eq 600 -and $actual.overload.cooldown_seconds -eq 600) "Group $GroupId policy did not round-trip exactly"
}
function Deadline {
  $account = (Api GET "/admin/accounts/$primaryId" $adminToken).Json.data
  if (-not $account.overload_until) { return $null }
  return [DateTimeOffset]$account.overload_until
}
function Assert-NotCooling {
  $until = Deadline
  Assert-True ($null -eq $until -or $until -le [DateTimeOffset]::UtcNow) 'Primary unexpectedly cooled below the configured threshold'
}
function Recover-Primary([string]$Label) {
  $null = Api POST "/admin/accounts/$primaryId/recover-state" $adminToken
  Assert-NotCooling
  # Only scheduler outbox propagation; no gateway probing or retry is used.
  Start-Sleep -Seconds 2
  $evidence.phases.Add(@{ label = $Label; recovery = 'cleared' })
  Save-Evidence
}
function Stats {
  $result = Http GET '/stats' '' $null 'http://group-failure-mock:8080'
  Assert-True ($result.Status -eq 200) 'Mock stats unavailable'
  $rows = @($result.Json)
  foreach ($row in $rows) {
    Assert-True ($row.scenario -eq 'overload' -and $row.slot -in @('primary', 'backup') -and $row.path -eq '/v1/responses' -and -not $row.stream) 'Unexpected mock request'
  }
  return ,$rows
}
function Gateway([string]$Label, [string]$Key, [int]$ExpectedPrimary) {
  Assert-True ($script:gatewayRequests -lt 8) 'Gateway request budget exhausted; no retry allowed'
  $before = Stats
  $script:gatewayRequests++
  Save-Evidence
  $result = Http POST '/v1/responses' $Key @{ model = 'gpt-4o-mini'; input = "synthetic group policy $run $Label"; stream = $false }
  $after = Stats
  $newRows = @($after | Select-Object -Skip $before.Count)
  $primaryCount = @($newRows | Where-Object slot -eq 'primary').Count
  $backupCount = @($newRows | Where-Object slot -eq 'backup').Count
  $evidence.phases.Add(@{ label = $Label; request = $script:gatewayRequests; http_status = $result.Status; primary_attempts = $primaryCount; backup_attempts = $backupCount })
  $evidence.upstream = $after
  Save-Evidence
  Assert-True ($result.Status -eq 200 -and $result.Json.object -eq 'response' -and $result.Json.status -eq 'completed') "$Label did not return a complete successful response"
  $text = @($result.Json.output | ForEach-Object { $_.content } | ForEach-Object { $_.text }) -join ''
  Assert-True ($text -eq 'backup-ok') "$Label returned unexpected semantic output"
  Assert-True ($primaryCount -eq $ExpectedPrimary -and $backupCount -eq 1 -and $newRows.Count -eq ($ExpectedPrimary + 1)) "$Label attempt mismatch: primary=$primaryCount backup=$backupCount"
}

$previousPort = [Environment]::GetEnvironmentVariable('SUB2API_TEST_PORT', 'Process')
$previousImage = [Environment]::GetEnvironmentVariable('SUB2API_TEST_IMAGE', 'Process')
$null = New-Item -ItemType Directory -Path $OutputDirectory
try {
  $env:SUB2API_TEST_PORT = [string]$Port
  $env:SUB2API_TEST_IMAGE = $CandidateImage
  Save-Evidence
  $null = Compose config --quiet
  # Recheck immediately before creation. A failed run must not be resumed by
  # invoking this script again against its retained project.
  Assert-True (-not @(Docker ps -aq --filter "label=com.docker.compose.project=$Project").Count) 'Project appeared during preflight'
  $null = Compose up --detach --no-build --pull never --wait --wait-timeout 180
  Assert-OwnedProject
  Wait-App
  $adminToken = Login 'admin@operator-test.local' 'Operator-Test-Admin-Password-2026!'
  Accept-Compliance $adminToken
  $operatorPolicy = (Api GET '/admin/roles/operator/permissions' $adminToken).Json.data

  $password = 'Group-Smoke-Synthetic-Only-2026!'
  $userEmail = "group-$run@operator-test.local"
  $operatorEmail = "group-operator-$run@operator-test.local"
  $userId = [long](Api POST '/admin/users' $adminToken @{ email = $userEmail; password = $password; role = 'user'; balance = 10; concurrency = 5 }).Json.data.id
  $operatorId = [long](Api POST '/admin/users' $adminToken @{ email = $operatorEmail; password = $password; role = 'user' }).Json.data.id
  Assert-True ($userId -gt 0 -and $operatorId -gt 0 -and $userId -ne $operatorId) 'Synthetic user IDs invalid'
  # Fixture-only promotion before first login, limited to the exact freshly
  # created numeric ID/email. Production role changes still require step-up.
  $null = Docker exec $containerIds['postgres'] psql -U sub2api_operator_test -d sub2api_operator_test --set=ON_ERROR_STOP=1 -c "UPDATE users SET role='operator' WHERE id=$operatorId AND email='$operatorEmail';"
  $role = (Docker exec $containerIds['postgres'] psql -U sub2api_operator_test -d sub2api_operator_test -At --set=ON_ERROR_STOP=1 -c "SELECT role FROM users WHERE id=$operatorId AND email='$operatorEmail';") -join ''
  Assert-True ($role.Trim() -eq 'operator') 'Synthetic operator promotion did not persist'
  $userToken = Login $userEmail $password
  $operatorToken = Login $operatorEmail $password
  Accept-Compliance $operatorToken

  $groupIds = @()
  foreach ($name in @('a', 'b')) {
    $id = [long](Api POST '/admin/groups' $adminToken @{ name = "group-$run-$name"; platform = 'openai'; subscription_type = 'standard'; rate_multiplier = 1 }).Json.data.id
    Assert-True ($id -gt 0) 'Synthetic group ID invalid'
    $groupIds += $id
  }
  $groupA = $groupIds[0]; $groupB = $groupIds[1]
  $policyPath = "/admin/groups/$groupA/failure-policy"
  $null = Api PUT $policyPath $adminToken (Policy 4)
  $null = Api PUT "/admin/groups/$groupB/failure-policy" $adminToken (Policy 2)
  Assert-Policy $groupA 4
  Assert-Policy $groupB 2
  foreach ($method in @('GET', 'PUT')) {
    $body = if ($method -eq 'PUT') { Policy 1 } else { $null }
    $null = Api $method $policyPath $userToken $body 403
  }
  $permissions = $operatorPolicy | ConvertTo-Json -Depth 20 | ConvertFrom-Json -AsHashtable
  $permissions.permissions['groups.read'] = $false
  $permissions.permissions['groups.write'] = $false
  $null = Api PUT '/admin/roles/operator/permissions' $adminToken $permissions
  $null = Api GET $policyPath $operatorToken $null 403
  $null = Api PUT $policyPath $operatorToken (Policy 1) 403
  $permissions.permissions['groups.read'] = $true
  $null = Api PUT '/admin/roles/operator/permissions' $adminToken $permissions
  $null = Api GET $policyPath $operatorToken
  $null = Api PUT $policyPath $operatorToken (Policy 1) 403
  $permissions.permissions['groups.write'] = $true
  $null = Api PUT '/admin/roles/operator/permissions' $adminToken $permissions
  $null = Api PUT $policyPath $operatorToken (Policy 4)
  Assert-Policy $groupA 4
  $null = Api PUT '/admin/roles/operator/permissions' $adminToken $operatorPolicy
  $evidence.phases.Add(@{ label = 'configuration_permissions'; admin = 'read/write'; user = 'denied'; operator = 'deny, read-only, read/write; restored' })

  $primaryId = 0L; $backupId = 0L
  foreach ($slot in @('primary', 'backup')) {
    $id = [long](Api POST '/admin/accounts' $adminToken @{
      name = "group-$run-$slot"; platform = 'openai'; type = 'apikey'; concurrency = 5
      priority = $(if ($slot -eq 'primary') { 0 } else { 100 }); rate_multiplier = 1; group_ids = $groupIds
      extra = @{ openai_passthrough = $true; openai_responses_mode = 'force_responses' }
      credentials = @{ api_key = "overload:$slot"; base_url = 'http://group-failure-mock:8080/v1'; model_mapping = @{ 'gpt-4o-mini' = 'gpt-4o-mini' }; pool_mode = $true; pool_mode_retry_count = 2 }
    }).Json.data.id
    Assert-True ($id -gt 0) 'Synthetic account ID invalid'
    if ($slot -eq 'primary') { $primaryId = $id } else { $backupId = $id }
  }
  $keyA = [string](Api POST '/keys' $userToken @{ name = "group-$run-a"; group_id = $groupA; quota = 1 }).Json.data.key
  $keyB = [string](Api POST '/keys' $userToken @{ name = "group-$run-b"; group_id = $groupB; quota = 1 }).Json.data.key
  Assert-True ([bool]$keyA -and [bool]$keyB) 'Synthetic API keys missing'
  $null = Api GET $policyPath $keyA $null 401
  $null = Api PUT $policyPath $keyA (Policy 1) 401
  Assert-Policy $groupA 4
  $evidence.resources = @{ user = $userId; operator = $operatorId; group_a = $groupA; group_b = $groupB; primary = $primaryId; backup = $backupId }
  $evidence.rules = @{ a = (Policy 4); b = (Policy 2) }
  Start-Sleep -Seconds 2
  Assert-True ((Stats).Count -eq 0) 'Fixture received a gateway request before the bounded scenario'

  Gateway 'a-three-below-four' $keyA 3
  Assert-NotCooling
  Restart-App 'partial-counter-persistence'
  Assert-Policy $groupA 4
  Assert-Policy $groupB 2
  Gateway 'a-fourth-after-restart' $keyA 1
  $firstUntil = Deadline
  Assert-True ($null -ne $firstUntil -and $firstUntil -gt [DateTimeOffset]::UtcNow.AddSeconds(500)) 'Fourth A failure did not persist the configured cooldown'
  Gateway 'b-shared-account-skipped' $keyB 0
  Recover-Primary 'active-cooldown-reset'
  Gateway 'a-three-after-recovery' $keyA 3
  Assert-NotCooling
  Recover-Primary 'partial-counter-reset'
  Gateway 'a-three-after-partial-reset' $keyA 3
  Assert-NotCooling
  Gateway 'b-own-second-failure' $keyB 2
  $secondUntil = Deadline
  Assert-True ($null -ne $secondUntil -and $secondUntil -gt [DateTimeOffset]::UtcNow.AddSeconds(500)) 'Independent B threshold did not persist cooldown'
  Restart-App 'active-cooldown-persistence'
  Assert-Policy $groupA 4
  Assert-Policy $groupB 2
  Assert-True ((Deadline) -eq $secondUntil) 'Restart changed the persisted cooldown deadline'
  Gateway 'a-cooldown-after-restart' $keyA 0
  Assert-True ((Deadline) -eq $secondUntil) 'Skipped request extended the cooldown deadline'
  Recover-Primary 'final-active-reset'
  Gateway 'a-eligible-after-final-reset' $keyA 3
  Assert-NotCooling
  Assert-OwnedProject
  Assert-True ($gatewayRequests -eq 8 -and (Stats).Count -eq 23) 'Fixed request/attempt totals did not match'
  $evidence.status = 'passed'
} catch {
  $evidence.status = 'failed'
  $evidence.error = $_.Exception.Message
  throw
} finally {
  if ($adminToken -and $null -ne $operatorPolicy) {
    try { $null = Api PUT '/admin/roles/operator/permissions' $adminToken $operatorPolicy; $evidence.operator_policy_restored = $true }
    catch { $evidence.operator_policy_restored = $false; $evidence.status = 'failed'; Write-Warning 'Synthetic operator policy restore failed; retained project requires inspection' }
  }
  $evidence.finished_at = [DateTime]::UtcNow.ToString('o')
  Save-Evidence
  [Environment]::SetEnvironmentVariable('SUB2API_TEST_PORT', $previousPort, 'Process')
  [Environment]::SetEnvironmentVariable('SUB2API_TEST_IMAGE', $previousImage, 'Process')
  Write-Output "Evidence: $(Join-Path $OutputDirectory 'evidence.json'); project retained: $Project; gateway calls: $gatewayRequests/8"
}
if ($evidence.status -ne 'passed') { throw 'Group failure correctness smoke did not pass' }
