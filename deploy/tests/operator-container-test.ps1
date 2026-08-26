[CmdletBinding()]
param(
  [string]$Project = "sub2api-operator-test-$PID",
  [int]$Port = 18080,
  [string]$Image = 'sub2api:0.1.183-custom.2',
  [switch]$UsePublishedImage,
  [switch]$Keep
)

if ($PSVersionTable.PSVersion.Major -lt 7) {
  throw 'operator-container-test.ps1 requires PowerShell 7 or newer (pwsh).'
}

$ErrorActionPreference = 'Stop'

if ($Project -notmatch '^sub2api-operator-test-[a-zA-Z0-9_-]+$') {
  throw 'Project must start with sub2api-operator-test- and contain only safe characters.'
}
if ($Port -lt 1024 -or $Port -gt 65535) {
  throw 'Port must be between 1024 and 65535.'
}

$composeFile = Join-Path $PSScriptRoot '..\docker-compose.operator-test.yml'
$baseUri = "http://127.0.0.1:$Port/api/v1"
$env:SUB2API_TEST_PORT = [string]$Port
$env:SUB2API_TEST_IMAGE = $Image
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path.Replace('\', '/')
$commit = (& git -c "safe.directory=$repoRoot" rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or -not $commit) {
  throw 'Unable to resolve the source commit for container build metadata.'
}
$env:SUB2API_TEST_COMMIT = $commit
$env:SUB2API_TEST_BUILD_DATE = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')

function Invoke-Compose {
  param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
  & docker compose --project-name $Project --file $composeFile @Arguments
  if ($LASTEXITCODE -ne 0) {
    throw "docker compose failed: $($Arguments -join ' ')"
  }
}

function Invoke-Api {
  param(
    [ValidateSet('GET', 'POST', 'PUT', 'DELETE')][string]$Method,
    [string]$Path,
    [string]$Token = '',
    [string]$AdminApiKey = '',
    [object]$Body = $null
  )

  $headers = @{ 'User-Agent' = 'sub2api-operator-container-test/1' }
  if ($Token) { $headers.Authorization = "Bearer $Token" }
  if ($AdminApiKey) { $headers['x-api-key'] = $AdminApiKey }
  $request = @{
    Uri = "$baseUri$Path"
    Method = $Method
    Headers = $headers
    SkipHttpErrorCheck = $true
    TimeoutSec = 30
  }
  if ($null -ne $Body) {
    $request.Body = $Body | ConvertTo-Json -Depth 12 -Compress
    $request.ContentType = 'application/json'
  }
  $response = Invoke-WebRequest @request
  $json = $null
  if ($response.Content) {
    try { $json = $response.Content | ConvertFrom-Json } catch { }
  }
  [pscustomobject]@{
    Status = [int]$response.StatusCode
    Json = $json
    Content = $response.Content
  }
}

function Assert-Status {
  param([object]$Response, [int[]]$Expected, [string]$Label)
  if ($Response.Status -notin $Expected) {
    throw "$Label expected HTTP $($Expected -join '/') but received $($Response.Status): $($Response.Content)"
  }
}

function Login {
  param([string]$Email, [string]$Password)
  $response = Invoke-Api POST '/auth/login' -Body @{ email = $Email; password = $Password }
  Assert-Status $response @(200) "login $Email"
  $token = [string]$response.Json.data.access_token
  if (-not $token) { throw "login $Email returned no access token" }
  [pscustomobject]@{ Token = $token; User = $response.Json.data.user }
}

function Accept-Compliance {
  param([string]$Token, [string]$Label)
  $status = Invoke-Api GET '/admin/compliance' -Token $Token
  Assert-Status $status @(200) "$Label compliance status"
  if ($status.Json.data.required -eq $true) {
    $phrase = [string]$status.Json.data.ack_phrase_en
    $accept = Invoke-Api POST '/admin/compliance/accept' -Token $Token -Body @{
      phrase = $phrase
      language = 'en'
    }
    Assert-Status $accept @(200) "$Label compliance acceptance"
  }
}

function Wait-Health {
  param([int]$Seconds = 180)
  $deadline = [DateTime]::UtcNow.AddSeconds($Seconds)
  do {
    try {
      $health = Invoke-WebRequest -Uri "http://127.0.0.1:$Port/health" -SkipHttpErrorCheck -TimeoutSec 5
      if ([int]$health.StatusCode -eq 200) { return }
    } catch { }
    Start-Sleep -Seconds 2
  } while ([DateTime]::UtcNow -lt $deadline)
  throw 'Sub2API did not become healthy before the timeout.'
}

function Test-OperatorWebSocket {
  param([string]$Token)
  $socket = [System.Net.WebSockets.ClientWebSocket]::new()
  $socket.Options.AddSubProtocol('sub2api-admin')
  $socket.Options.AddSubProtocol("jwt.$Token")
  $cancel = [Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds(15))
  try {
    $uri = [Uri]"ws://127.0.0.1:$Port/api/v1/admin/ops/ws/qps"
    $null = $socket.ConnectAsync($uri, $cancel.Token).GetAwaiter().GetResult()
    if ($socket.State -ne [System.Net.WebSockets.WebSocketState]::Open) {
      throw "WebSocket state is $($socket.State)"
    }
    $null = $socket.CloseAsync(
      [System.Net.WebSockets.WebSocketCloseStatus]::NormalClosure,
      'test complete',
      [Threading.CancellationToken]::None
    ).GetAwaiter().GetResult()
  } finally {
    $cancel.Dispose()
    $socket.Dispose()
  }
}

$started = $false
try {
  Invoke-Compose config --quiet
  if ($UsePublishedImage) {
    if ($Image -notmatch '@sha256:[a-fA-F0-9]{64}$') {
      throw 'Published-image acceptance requires an immutable @sha256 digest.'
    }
    Invoke-Compose pull sub2api
    Invoke-Compose up --detach --no-build --wait --wait-timeout 240
  } else {
    Invoke-Compose up --detach --build --wait --wait-timeout 240
  }
  $started = $true
  Wait-Health

  $admin = Login 'admin@operator-test.local' 'Operator-Test-Admin-Password-2026!'
  Accept-Compliance $admin.Token 'admin'

  $candidateResponse = Invoke-Api POST '/admin/users' -Token $admin.Token -Body @{
    email = 'operator@operator-test.local'
    password = 'Operator-Test-Password-2026!'
    username = 'operator-test'
    role = 'user'
  }
  Assert-Status $candidateResponse @(200) 'create operator candidate'
  $operatorId = [int64]$candidateResponse.Json.data.id

  $userResponse = Invoke-Api POST '/admin/users' -Token $admin.Token -Body @{
    email = 'user@operator-test.local'
    password = 'User-Test-Password-2026!'
    username = 'user-test'
    role = 'user'
  }
  Assert-Status $userResponse @(200) 'create regular user'
  $userId = [int64]$userResponse.Json.data.id
  $adminId = [int64]$admin.User.id

  # Test setup only: promote an ordinary candidate directly because privileged
  # role assignment correctly requires an enrolled step-up 2FA session.
  Invoke-Compose exec -T postgres psql -U sub2api_operator_test -d sub2api_operator_test --set=ON_ERROR_STOP=1 -c "UPDATE users SET role = 'operator' WHERE id = $operatorId;"
  Invoke-Compose restart sub2api
  Wait-Health

  $admin = Login 'admin@operator-test.local' 'Operator-Test-Admin-Password-2026!'
  $operator = Login 'operator@operator-test.local' 'Operator-Test-Password-2026!'
  $user = Login 'user@operator-test.local' 'User-Test-Password-2026!'
  Accept-Compliance $admin.Token 'admin'
  Accept-Compliance $operator.Token 'operator'

  $ordinaryKey = Invoke-Api POST '/keys' -Token $user.Token -Body @{ name = 'ordinary-user-key' }
  Assert-Status $ordinaryKey @(200) 'ordinary user API key creation'
  $ordinaryKeyId = [int64]$ordinaryKey.Json.data.id
  Assert-Status (Invoke-Api PUT "/admin/api-keys/$ordinaryKeyId" -Token $operator.Token -Body @{ group_id = 0 }) @(200) 'operator ordinary-user API key mutation'

  $privilegedKey = Invoke-Api POST '/keys' -Token $operator.Token -Body @{ name = 'operator-owned-key' }
  Assert-Status $privilegedKey @(200) 'operator personal API key creation'
  $privilegedKeyId = [int64]$privilegedKey.Json.data.id
  Assert-Status (Invoke-Api PUT "/admin/api-keys/$privilegedKeyId" -Token $operator.Token -Body @{ group_id = 0 }) @(403) 'operator privileged-owner API key mutation'

  $allowed = @(
    '/admin/dashboard/stats',
    '/admin/ops/capabilities',
    '/admin/users?page=1&page_size=5',
    '/admin/announcements?page=1&page_size=5',
    '/admin/redeem-codes/stats',
    '/admin/promo-codes?page=1&page_size=5',
    '/admin/usage/filter-options?account_search=test',
    '/admin/usage/cleanup-tasks?page=1&page_size=5'
  )
  foreach ($path in $allowed) {
    Assert-Status (Invoke-Api GET $path -Token $operator.Token) @(200) "operator allowed $path"
  }

  $denied = @(
    '/admin/groups',
    '/admin/channels',
    '/admin/subscriptions',
    '/admin/accounts',
    '/admin/plugins',
    '/admin/proxies',
    '/admin/audit-logs',
    '/admin/ops/advanced-settings',
    '/admin/settings'
  )
  foreach ($path in $denied) {
    Assert-Status (Invoke-Api GET $path -Token $operator.Token) @(403) "operator denied $path"
  }

  Assert-Status (Invoke-Api POST '/admin/ops/alert-rules' -Token $operator.Token -Body @{}) @(403) 'operator alert-rule mutation'
  Assert-Status (Invoke-Api POST '/admin/usage/cleanup-tasks' -Token $operator.Token -Body @{}) @(403) 'operator cleanup creation'
  Assert-Status (Invoke-Api PUT "/admin/users/$adminId" -Token $operator.Token -Body @{ status = 'disabled' }) @(403) 'operator admin mutation'
  Assert-Status (Invoke-Api PUT "/admin/users/$operatorId" -Token $operator.Token -Body @{ username = 'changed' }) @(403) 'operator self mutation'
  Assert-Status (Invoke-Api POST '/admin/users' -Token $operator.Token -Body @{
    email = 'forbidden-operator@operator-test.local'
    password = 'Forbidden-Test-Password-2026!'
    role = 'operator'
  }) @(403) 'operator privileged user creation'

  $before = Invoke-Api GET "/admin/users/$userId" -Token $operator.Token
  Assert-Status $before @(200) 'ordinary user before atomic batch check'
  $beforeConcurrency = [int]$before.Json.data.concurrency
  Assert-Status (Invoke-Api POST '/admin/users/batch-concurrency' -Token $operator.Token -Body @{
    user_ids = @($adminId, $userId)
    concurrency = 77
    mode = 'set'
  }) @(403) 'mixed privileged batch'
  $after = Invoke-Api GET "/admin/users/$userId" -Token $operator.Token
  Assert-Status $after @(200) 'ordinary user after atomic batch check'
  if ([int]$after.Json.data.concurrency -ne $beforeConcurrency) {
    throw 'Mixed privileged batch partially changed an ordinary user.'
  }

  Assert-Status (Invoke-Api GET '/admin/users' -Token $user.Token) @(403) 'regular user management denial'
  Assert-Status (Invoke-Api GET '/admin/settings' -Token $admin.Token) @(200) 'admin full access'
  Assert-Status (Invoke-Api GET '/admin/groups' -Token $admin.Token) @(200) 'admin group access'
  Assert-Status (Invoke-Api GET '/admin/users' -Token 'invalid-token') @(401) 'invalid JWT'

  $keyResponse = Invoke-Api POST '/admin/settings/admin-api-key/regenerate' -Token $admin.Token
  Assert-Status $keyResponse @(200) 'admin API key generation'
  $adminApiKey = [string]$keyResponse.Json.data.key
  Assert-Status (Invoke-Api GET '/admin/settings' -AdminApiKey $adminApiKey) @(200) 'admin API key maps to admin'

  Test-OperatorWebSocket $operator.Token

  Invoke-Compose restart sub2api
  Wait-Health
  $persistedOperator = Login 'operator@operator-test.local' 'Operator-Test-Password-2026!'
  Assert-Status (Invoke-Api GET '/admin/ops/capabilities' -Token $persistedOperator.Token) @(200) 'operator persists after restart'

  $logs = (& docker compose --project-name $Project --file $composeFile logs --no-color sub2api 2>&1 | Out-String)
  if ($logs -match '(?im)panic:|database.*connection.*(failed|refused)|redis.*connection.*(failed|refused)|migration.*(failed|error)') {
    throw "Container logs contain a fatal pattern:`n$logs"
  }

  Write-Host "Operator container acceptance passed for $Image on port $Port."
} finally {
  if ($started -and -not $Keep) {
    Invoke-Compose down --volumes --remove-orphans
  } elseif ($started) {
    Write-Host "Kept isolated project $Project for inspection."
  }
}
