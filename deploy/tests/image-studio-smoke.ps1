[CmdletBinding()]
param(
  [string]$Project = 'sub2api-operator-test-v0216',
  [int]$Port = 18086,
  [int]$MockPort = 18087,
  [int]$TimeoutSeconds = 240,
  [string]$ResumeBatchId = '',
  [string]$ResumeFixtureSuffix = '',
  [switch]$KeepFixtures
)

if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 or newer is required.' }
$ErrorActionPreference = 'Stop'
if ($Project -notmatch '^sub2api-operator-test-[a-zA-Z0-9_-]+$') { throw 'Only isolated operator-test projects are permitted.' }
if ($Port -lt 1024 -or $Port -gt 65535 -or $MockPort -lt 1024 -or $MockPort -gt 65535 -or $Port -eq $MockPort) { throw 'Invalid local ports.' }
$network = "${Project}_default"
$networkProject = (& docker network inspect $network --format '{{index .Labels "com.docker.compose.project"}}').Trim()
if ($LASTEXITCODE -ne 0 -or $networkProject -ne $Project) { throw 'Expected isolated Compose network is missing.' }
$resume = [bool]$ResumeBatchId
if ($resume -and ($ResumeBatchId -notmatch '^imgbatch_[a-f0-9]{32}$' -or $ResumeFixtureSuffix -notmatch '^[a-f0-9]{10}$')) {
  throw 'Resume requires a valid synthetic batch ID and the ten-character fixture suffix.'
}
$suffix = if ($resume) { $ResumeFixtureSuffix } else { [Guid]::NewGuid().ToString('N').Substring(0, 10) }
if ($resume) { $KeepFixtures = $true }
$container = "image-smoke-$suffix"
$mockScript = (Resolve-Path (Join-Path $PSScriptRoot '../acceptance/mock-image-upstream.mjs')).Path
$baseUri = "http://127.0.0.1:$Port"
$mockUri = "http://127.0.0.1:$MockPort"
$artifactDir = Join-Path ([IO.Path]::GetTempPath()) "moshu-image-smoke-$suffix"
$null = New-Item -ItemType Directory -Path $artifactDir -Force
$reference = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aO7sAAAAASUVORK5CYII='
$adminToken = ''
$userId = $groupId = $accountId = $null
$started = $false

function Request-Json {
  param([string]$Method, [string]$Path, [string]$Token = '', [object]$Body = $null, [hashtable]$ExtraHeaders = @{})
  $headers = @{ 'User-Agent' = 'moshu-local-image-smoke/1' }
  if ($Token) { $headers.Authorization = "Bearer $Token" }
  foreach ($entry in $ExtraHeaders.GetEnumerator()) { $headers[$entry.Key] = $entry.Value }
  $parameters = @{ Uri = "$baseUri$Path"; Method = $Method; Headers = $headers; TimeoutSec = 45; SkipHttpErrorCheck = $true }
  if ($null -ne $Body) { $parameters.Body = ConvertTo-Json $Body -Depth 20 -Compress; $parameters.ContentType = 'application/json' }
  $response = Invoke-WebRequest @parameters
  if ([int]$response.StatusCode -lt 200 -or [int]$response.StatusCode -ge 300) {
    throw "$Method $Path returned HTTP $($response.StatusCode): $($response.Content)"
  }
  if ($response.Content) { return $response.Content | ConvertFrom-Json }
}

function Assert-True {
  param([bool]$Condition, [string]$Message)
  if (-not $Condition) { throw $Message }
}

function Assert-Png {
  param([byte[]]$Bytes, [string]$Label)
  Assert-True ($Bytes.Length -ge 8 -and [Convert]::ToHexString($Bytes[0..7]) -eq '89504E470D0A1A0A') "$Label did not contain PNG bytes"
}

try {
  if ($resume) {
    $containerProject = (& docker inspect $container --format '{{index .Config.Labels "moshu.acceptance.project"}}').Trim()
    if ($LASTEXITCODE -ne 0 -or $containerProject -ne $Project) { throw 'Existing synthetic image fixture does not belong to this project.' }
  } else {
    & docker run --detach --name $container --label "moshu.acceptance.project=$Project" --network $network --publish "127.0.0.1:${MockPort}:8080" --mount "type=bind,source=$mockScript,target=/fixture/mock-image-upstream.mjs,readonly" node:24-alpine node /fixture/mock-image-upstream.mjs | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Unable to start local image fixture.' }
    $started = $true
  }
  $deadline = [DateTime]::UtcNow.AddSeconds(30)
  do {
    try { $health = Invoke-RestMethod "$mockUri/health" -TimeoutSec 2; if ($health.status -eq 'ok') { break } } catch { }
    Start-Sleep -Milliseconds 300
  } while ([DateTime]::UtcNow -lt $deadline)
  Assert-True ($health.status -eq 'ok') 'Image fixture did not start'

  $admin = Request-Json POST '/api/v1/auth/login' -Body @{ email = 'admin@operator-test.local'; password = 'Operator-Test-Admin-Password-2026!' }
  $adminToken = [string]$admin.data.access_token
  Assert-True ([bool]$adminToken) 'Synthetic administrator login failed'
  $compliance = Request-Json GET '/api/v1/admin/compliance' $adminToken
  if ($compliance.data.required) {
    $null = Request-Json POST '/api/v1/admin/compliance/accept' $adminToken @{ phrase = $compliance.data.ack_phrase_en; language = 'en' }
  }
  if (-not $resume) {
    $group = Request-Json POST '/api/v1/admin/groups' $adminToken @{
      name = "image-smoke-$suffix"; platform = 'openai'; subscription_type = 'standard'; rate_multiplier = 1
      allow_image_generation = $true; allow_batch_image_generation = $true
      image_price_1k = 0.01; image_price_2k = 0.01; image_price_4k = 0.01
      batch_image_discount_multiplier = 1; batch_image_hold_multiplier = 1
    }
    $groupId = [long]$group.data.id
    $account = Request-Json POST '/api/v1/admin/accounts' $adminToken @{
      name = "image-fixture-$suffix"; platform = 'openai'; type = 'apikey'; concurrency = 3; priority = 1; rate_multiplier = 1
      credentials = @{ api_key = 'local-image-fixture-not-a-real-key'; base_url = "http://${container}:8080/v1"; model_mapping = @{ 'gpt-image-2' = 'gpt-image-2' } }
      group_ids = @($groupId)
    }
    $accountId = [long]$account.data.id
    $user = Request-Json POST '/api/v1/admin/users' $adminToken @{
      email = "image-smoke-$suffix@operator-test.local"; password = 'Image-Smoke-Password-2026!'; role = 'user'
      balance = 10; concurrency = 3; allowed_groups = @($groupId)
    }
    $userId = [long]$user.data.id
  }
  $login = Request-Json POST '/api/v1/auth/login' -Body @{ email = "image-smoke-$suffix@operator-test.local"; password = 'Image-Smoke-Password-2026!' }
  if ($resume) {
    $keys = Request-Json GET '/api/v1/keys' $login.data.access_token
    $keyData = @($keys.data.items | Where-Object { $_.name -eq 'local-image-smoke' })
    Assert-True ($keyData.Count -eq 1) 'Existing synthetic fixture key was not found'
    $apiKey = [string]$keyData[0].key
  } else {
    $key = Request-Json POST '/api/v1/keys' $login.data.access_token @{ name = 'local-image-smoke'; group_id = $groupId; quota = 1 }
    $apiKey = [string]$key.data.key
  }
  Assert-True ([bool]$apiKey) 'API Key was not returned'

  $models = Request-Json GET '/v1/models' $apiKey
  Assert-True ('gpt-image-2' -in @($models.data.id)) 'Image model is missing from model discovery'
  $batchModels = Request-Json GET '/v1/images/batches/models' $apiKey
  $batchModel = @($batchModels.data | Where-Object { $_.id -eq 'gpt-image-2' -and $_.provider -eq 'app_managed' })
  Assert-True ($batchModel.Count -eq 1) 'App-managed image model is missing'
  Assert-True ($batchModel[0].mode -eq 'managed_fanout') 'Unexpected image execution mode'

  if (-not $resume) {
    $single = Request-Json POST '/v1/images/generations' $apiKey @{ model = 'gpt-image-2'; prompt = 'local single fixture'; n = 1; size = '2048x2048'; output_format = 'jpeg'; response_format = 'b64_json' }
    Assert-Png ([Convert]::FromBase64String($single.data[0].b64_json)) 'Single-image response'
    $payload = @{
      model = 'gpt-image-2'; provider = 'app_managed'; task_name = "local-image-smoke-$suffix"; image_size = '2K'; response_mime_type = 'image/jpeg'
      items = @(
        @{ custom_id = 'shared'; prompt = 'local shared reference fixture'; output_count = 2; reference_images = @(@{ id = 'shared.png'; mime_type = 'image/png'; data = $reference }) },
        @{ custom_id = 'individual'; prompt = 'local individual reference fixture'; output_count = 1; reference_images = @(@{ id = 'individual.png'; mime_type = 'image/png'; data = $reference }) }
      )
    }
    $idempotency = [Guid]::NewGuid().ToString()
    $job = Request-Json POST '/v1/images/batches' $apiKey $payload @{ 'Idempotency-Key' = $idempotency }
    Assert-True ($job.item_count -eq 3) 'Output fan-out did not create three single-image items'
    $repeated = Request-Json POST '/v1/images/batches' $apiKey $payload @{ 'Idempotency-Key' = $idempotency }
    Assert-True ($repeated.id -eq $job.id) 'Repeated batch submission created a duplicate job'
  } else {
    $job = Request-Json GET "/v1/images/batches/$ResumeBatchId" $apiKey
  }
  $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
  do {
    $current = Request-Json GET "/v1/images/batches/$($job.id)" $apiKey
    if ($current.status -eq 'completed') { break }
    if ($current.status -in @('failed', 'cancelled', 'output_deleted')) { throw "Image batch ended as $($current.status)" }
    Start-Sleep -Seconds 1
  } while ([DateTime]::UtcNow -lt $deadline)
  Assert-True ($current.status -eq 'completed' -and $current.success_count -eq 3 -and $current.fail_count -eq 0) 'Batch did not complete successfully before timeout'
  Assert-True ($null -ne $current.actual_cost -and [Math]::Abs([double]$current.actual_cost - 0.03) -lt 0.0000001) 'Batch settlement did not charge exactly the three synthetic successful outputs'
  $items = Request-Json GET "/v1/images/batches/$($job.id)/items" $apiKey
  Assert-True (@($items.data).Count -eq 3) 'Batch item list does not contain three results'
  foreach ($item in $items.data) {
    Assert-True ($item.status -eq 'succeeded' -and $item.mime_type -eq 'image/png' -and $item.file_extension -eq 'png') "Unexpected item metadata for $($item.custom_id)"
    $escapedId = [Uri]::EscapeDataString($item.custom_id)
    $target = Join-Path $artifactDir "$($item.custom_id).png"
    $download = Invoke-WebRequest "$baseUri/v1/images/batches/$($job.id)/items/$escapedId/content" -Headers @{ Authorization = "Bearer $apiKey" } -OutFile $target -PassThru -TimeoutSec 30
    Assert-True ([string]$download.Headers['Content-Type'] -match 'image/png') 'Single-image download MIME is not PNG'
    Assert-Png ([IO.File]::ReadAllBytes($target)) 'Single-image download'
  }
  $zipPath = Join-Path $artifactDir 'moshu-images.zip'
  $null = Invoke-WebRequest "$baseUri/v1/images/batches/$($job.id)/download" -Headers @{ Authorization = "Bearer $apiKey" } -OutFile $zipPath -TimeoutSec 30
  $zip = [IO.Compression.ZipFile]::OpenRead($zipPath)
  try {
    $images = @($zip.Entries | Where-Object { $_.Name -match '\.png$' })
    Assert-True ($images.Count -eq 3) 'ZIP does not contain three PNG images'
  } finally { $zip.Dispose() }
  $stats = Invoke-RestMethod "$mockUri/__smoke/stats" -TimeoutSec 5
  Assert-True (@($stats.requests).Count -eq 4) 'Unexpected upstream request count (single + three batch items expected)'
  $edits = @($stats.requests | Where-Object { $_.path -eq '/v1/images/edits' })
  Assert-True ($edits.Count -eq 3) 'References were not sent to every expanded batch output'
  Assert-True (@($stats.requests | Where-Object { $null -ne $_.n -and $_.n -ne 1 }).Count -eq 0) 'An upstream request asked for more than one output'
  Assert-True (@($edits | Where-Object { -not $_.idempotency_key -or $_.references.Count -ne 1 }).Count -eq 0) 'Batch reference or retry key was lost'
  Assert-True (@($edits.idempotency_key | Select-Object -Unique).Count -eq 3) 'Different outputs shared an upstream retry key'
  [pscustomobject]@{
    status = 'passed'; project = $Project; batch_id = $job.id; images = 3; resumed = $resume
    upstream_requests = $stats.requests.Count; max_upstream_concurrency = $stats.max_active
    actual_cost = $current.actual_cost; artifact_directory = $artifactDir
  } | ConvertTo-Json
} finally {
  if (-not $KeepFixtures -and $adminToken) {
    foreach ($target in @(@('accounts', $accountId), @('users', $userId), @('groups', $groupId))) {
      if ($target[1]) { try { $null = Request-Json DELETE "/api/v1/admin/$($target[0])/$($target[1])" $adminToken } catch { Write-Warning "Synthetic $($target[0]) cleanup failed: $($_.Exception.Message)" } }
    }
  }
  if ($started -and -not $KeepFixtures) {
    & docker stop $container | Out-Null
    & docker rm $container | Out-Null
  }
}
