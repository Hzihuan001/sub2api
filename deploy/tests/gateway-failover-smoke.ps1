[CmdletBinding()]
param([string]$Project = 'sub2api-operator-test-v0216', [int]$Port = 18086, [int]$MockPort = 18088)
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 is required' }
if ($Project -notmatch '^sub2api-operator-test-[a-zA-Z0-9_-]+$') { throw 'Only isolated test projects are allowed' }
if ($Port -lt 1024 -or $MockPort -lt 1024 -or $Port -gt 65535 -or $MockPort -gt 65535 -or $Port -eq $MockPort) { throw 'Invalid ports' }
$network = "${Project}_default"
if ((& docker network inspect $network --format '{{index .Labels "com.docker.compose.project"}}').Trim() -ne $Project) { throw 'Wrong isolated network' }
$suffix = [Guid]::NewGuid().ToString('N').Substring(0,10)
$container = "failover-smoke-$suffix"
$mock = (Resolve-Path (Join-Path $PSScriptRoot '../acceptance/mock-failover-upstream.mjs')).Path
$base = "http://127.0.0.1:$Port"
$fixtureBase = "http://127.0.0.1:$MockPort"
$resources = [Collections.Generic.List[object]]::new()
$results = [Collections.Generic.List[object]]::new()
$adminToken = ''
$started = $false
function Api([string]$Method, [string]$Path, [string]$Token = '', [object]$Body = $null) {
  $p = @{ Uri = "$base$Path"; Method = $Method; TimeoutSec = 45; SkipHttpErrorCheck = $true }
  if ($Token) { $p.Headers = @{ Authorization = "Bearer $Token" } }
  if ($null -ne $Body) { $p.Body = ConvertTo-Json $Body -Depth 20 -Compress; $p.ContentType = 'application/json' }
  $r = Invoke-WebRequest @p
  if ($r.StatusCode -ge 300) { throw "$Method $Path HTTP $($r.StatusCode): $($r.Content)" }
  if ($r.Content) { return $r.Content | ConvertFrom-Json }
}
try {
  & docker run --detach --name $container --label "moshu.acceptance.project=$Project" --network $network --publish "127.0.0.1:${MockPort}:8080" --mount "type=bind,source=$mock,target=/fixture/server.mjs,readonly" node:24-alpine node /fixture/server.mjs | Out-Null
  if ($LASTEXITCODE -ne 0) { throw 'Mock startup failed' }
  $started = $true
  $null = Invoke-RestMethod "$fixtureBase/health" -TimeoutSec 5
  $admin = Api POST '/api/v1/auth/login' '' @{ email='admin@operator-test.local'; password='Operator-Test-Admin-Password-2026!' }
  $adminToken = $admin.data.access_token
  $c = Api GET '/api/v1/admin/compliance' $adminToken
  if ($c.data.required) { $null = Api POST '/api/v1/admin/compliance/accept' $adminToken @{ phrase=$c.data.ack_phrase_en; language='en' } }
  $u = Api POST '/api/v1/admin/users' $adminToken @{ email="failover-$suffix@operator-test.local"; password='Failover-Smoke-2026!'; role='user'; balance=10; concurrency=5 }
  $resources.Add(@('users', $u.data.id))
  $login = Api POST '/api/v1/auth/login' '' @{ email="failover-$suffix@operator-test.local"; password='Failover-Smoke-2026!' }
  foreach ($scenario in @('overload','quota','model','server','timeout','auth','rate','stream','safety','parameter','partial')) {
    $g = Api POST '/api/v1/admin/groups' $adminToken @{ name="failover-$suffix-$scenario"; platform='openai'; subscription_type='standard'; rate_multiplier=1 }
    $resources.Add(@('groups', $g.data.id))
    $primaryId = 0
    foreach ($slot in @('primary','backup')) {
      $a = Api POST '/api/v1/admin/accounts' $adminToken @{
        name="failover-$suffix-$scenario-$slot"; platform='openai'; type='apikey'; concurrency=5
        priority=$(if ($slot -eq 'primary') {0} else {100}); rate_multiplier=1; group_ids=@($g.data.id)
        extra=@{ openai_passthrough=$true }
        credentials=@{ api_key="${scenario}:$slot"; base_url="http://${container}:8080/v1"; model_mapping=@{'gpt-4o-mini'='gpt-4o-mini'}; pool_mode=$true; pool_mode_retry_count=2 }
      }
      $resources.Add(@('accounts', $a.data.id))
      if ($slot -eq 'primary') { $primaryId = $a.data.id }
    }
    $k = Api POST '/api/v1/keys' $login.data.access_token @{ name="failover-$scenario"; group_id=$g.data.id; quota=1 }
    $payload = @{ model='gpt-4o-mini'; input='synthetic failover fixture'; stream=($scenario -in @('stream','partial')) } | ConvertTo-Json -Compress
    $r = Invoke-WebRequest "$base/v1/responses" -Method Post -ContentType 'application/json' -Body $payload -Headers @{ Authorization="Bearer $($k.data.key)" } -SkipHttpErrorCheck -TimeoutSec 45
    # Invoke-RestMethod arrays are unrolled explicitly for reliable counts.
    $all = Invoke-RestMethod "$fixtureBase/stats" -TimeoutSec 5
    $seen = @($all | Where-Object scenario -eq $scenario)
    $pCount = @($seen | Where-Object slot -eq 'primary').Count
    $bCount = @($seen | Where-Object slot -eq 'backup').Count
    if ($pCount -lt 1) { throw "$scenario did not exercise the primary account" }
    if ($scenario -in @('safety','parameter','partial')) {
      if ($bCount -ne 0 -or $pCount -ne 1) { throw "$scenario was unsafely replayed: primary=$pCount backup=$bCount" }
      if ($scenario -eq 'partial' -and $r.Content -notmatch 'already-output') { throw 'Partial content was lost' }
    } else {
      if ($r.StatusCode -ne 200 -or $r.Content -notmatch 'backup-ok' -or $bCount -ne 1) { throw "$scenario did not safely fail over: HTTP=$($r.StatusCode) primary=$pCount backup=$bCount body=$($r.Content)" }
      $max = if ($scenario -eq 'overload') { 3 } elseif ($scenario -in @('server','stream')) { 2 } else { 1 }
      if ($pCount -gt $max) { throw "$scenario exceeded retry budget: $pCount > $max" }
    }
    if ($scenario -eq 'overload') {
      $account = Api GET "/api/v1/admin/accounts/$primaryId" $adminToken
      if (-not $account.data.overload_until) { throw 'Third overload did not persist account cooldown' }
      $next = Invoke-WebRequest "$base/v1/responses" -Method Post -ContentType 'application/json' -Body $payload -Headers @{ Authorization="Bearer $($k.data.key)" } -SkipHttpErrorCheck -TimeoutSec 20
      $all = Invoke-RestMethod "$fixtureBase/stats" -TimeoutSec 5
      if (@($all | Where-Object { $_.scenario -eq 'overload' -and $_.slot -eq 'primary' }).Count -ne $pCount -or $next.Content -notmatch 'backup-ok') { throw 'Next request selected cooled primary' }
    }
    $results.Add([pscustomobject]@{scenario=$scenario; status='passed'; primary_attempts=$pCount; backup_attempts=$bCount})
  }
  $results | ConvertTo-Json
} finally {
  if ($adminToken) {
    foreach ($kind in @('accounts','users','groups')) {
      foreach ($resource in $resources) {
        if ($resource[0] -eq $kind) { try { $null = Api DELETE "/api/v1/admin/$kind/$($resource[1])" $adminToken } catch { Write-Warning "Synthetic $kind cleanup failed" } }
      }
    }
  }
  if ($started) { & docker stop $container | Out-Null; & docker rm $container | Out-Null }
}
