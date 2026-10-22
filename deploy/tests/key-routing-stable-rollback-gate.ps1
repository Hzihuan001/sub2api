[CmdletBinding()]
param(
  [switch]$Execute,
  [string]$SourceProject = 'moshu-reseller-acceptance-key-routing',
  [string]$OutputDirectory = ''
)

# Isolated business-level rollback check. Reads a synthetic COS PostgreSQL
# database using pg_dump, clones it to a disposable volume, and never joins,
# stops, mounts, or writes the existing acceptance services or their volumes.
# All test traffic uses the local fake upstream on a Docker internal network.
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 required' }
$StableTag = 'ghcr.io/hzihuan001/sub2api:0.2.16-custom.12'
$StableID = 'sha256:e6a53ec608abd463bfa41ccc6e939dca0175756adeb40e957ebe31a6b68c3610'
$CandidateTag = 'sub2api-custom:key-account-routing-priority-ui'
$CandidateID = 'sha256:ed9f20d39f18a0c0f626f5a89b0cdc0a7bfbe8c10471a34ee4a178a75c15f7aa'
$ProjectPrefix = 'key-route-stable-rollback-'
if (-not $Execute) {
  Write-Output 'Read-only preflight only. Use -Execute to pg_dump the synthetic COS acceptance database into a new private clone and run six fake-upstream requests through candidate → stable → candidate.'
  return
}

function Check([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Docker([string[]]$DockerArgs) {
  $result = & docker.exe @DockerArgs 2>&1
  if ($LASTEXITCODE -ne 0) { throw "Docker command failed: $($DockerArgs[0]) $($DockerArgs[1])" }
  return $result
}
function InspectOne([string]$Name) {
  $parsed = ((Docker @('inspect', $Name)) -join "`n") | ConvertFrom-Json -AsHashtable -NoEnumerate
  if ($parsed -is [array]) { return $parsed[0] }
  return $parsed
}
function Save {
  $evidence.updated_at = [DateTime]::UtcNow.ToString('o')
  $evidence | ConvertTo-Json -Depth 24 | Set-Content -LiteralPath (Join-Path $OutputDirectory 'evidence.json') -Encoding utf8
}
function CloneSql([string]$Query) {
  $raw = $Query | & docker.exe exec -i $dbName psql -X -qAt -U acceptance -d rollbackdata --set=ON_ERROR_STOP=1
  if ($LASTEXITCODE -ne 0) { throw 'Cloned database read-only SQL failed' }
  return ($raw -join "`n").Trim()
}
function CloneWrite([string]$Query) {
  # This helper is reachable only after pg_restore, and only for the private
  # clone. The source acceptance database has no write path in this script.
  $raw = $Query | & docker.exe exec -i $dbName psql -X -qAt -U acceptance -d rollbackdata --set=ON_ERROR_STOP=1
  if ($LASTEXITCODE -ne 0) { throw 'Private clone preparation SQL failed' }
}
function WaitPg {
  $deadline = [DateTime]::UtcNow.AddSeconds(60)
  do {
    $null = & docker.exe exec $dbName pg_isready -U acceptance -d sub2api 2>$null
    if ($LASTEXITCODE -eq 0) { return }
    Start-Sleep -Milliseconds 500
  } while ([DateTime]::UtcNow -lt $deadline)
  throw 'Private PostgreSQL did not become ready'
}
function WaitApp([string]$ImageID) {
  $deadline = [DateTime]::UtcNow.AddSeconds(120)
  do {
    $c = InspectOne $appName
    if (-not $c.State.Running) { throw "Application exited before health: $ImageID" }
    if ($c.State.Health.Status -eq 'healthy') { return }
    Start-Sleep -Seconds 1
  } while ([DateTime]::UtcNow -lt $deadline)
  throw "Application health timeout: $ImageID"
}

$run = [Guid]::NewGuid().ToString('N').Substring(0, 12)
$Project = "$ProjectPrefix$run"
$network = "$Project-net"
$dbVolume = "$Project-pg-data"
$redisVolume = "$Project-redis-data"
$dbName = "$Project-pg"
$redisName = "$Project-redis"
$mockName = "$Project-mock"
$appName = "$Project-app"
$runLabel = 'moshu.acceptance.rollback-run'
$fixture = (Resolve-Path (Join-Path $PSScriptRoot '../acceptance/mock-failover-upstream.mjs')).Path
if (-not $OutputDirectory) { $OutputDirectory = "D:/projects/$Project" }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
Check (-not (Test-Path -LiteralPath $OutputDirectory)) 'Evidence directory must be new'
$null = New-Item -ItemType Directory -Path $OutputDirectory
$dumpPath = Join-Path $OutputDirectory 'synthetic-cos-clone.sql'
$created = [Collections.Generic.List[object]]::new()
$appLogs = [Collections.Generic.List[string]]::new()
$sourceSnapshot = @{}
$gatewayCount = 0
$evidence = [ordered]@{
  status='preflight'; run=$run; project=$Project; started_at=[DateTime]::UtcNow.ToString('o')
  source_project=$SourceProject; source_site='cos'; stable_tag=$StableTag; stable_image_id=$StableID
  candidate_tag=$CandidateTag; candidate_image_id=$CandidateID; gateway_limit=6
  synthetic_only=$true; real_model_calls=0; published_ports=0; original_volumes_written=0
  phases=[Collections.Generic.List[object]]::new(); cleanup=[Collections.Generic.List[object]]::new()
}

try {
  # Validate exact local artifacts and source identities before any resource change.
  Check ($SourceProject -eq 'moshu-reseller-acceptance-key-routing') 'Only the reviewed key-routing acceptance database is permitted as the source'
  Check ((Docker @('info', '--format', '{{.ServerVersion}}')).Count -gt 0) 'Docker daemon is unavailable'
  Check (((Docker @('image','inspect',$StableTag,'--format','{{.Id}}')) -join '').Trim() -eq $StableID) 'Stable tag does not resolve to the approved immutable image'
  Check (((Docker @('image','inspect',$CandidateTag,'--format','{{.Id}}')) -join '').Trim() -eq $CandidateID) 'Candidate tag does not resolve to the approved immutable image'
  $sourceNetwork = "${SourceProject}_default"
  $sourceDbName = "${SourceProject}-cos-db-1"
  $sourceAppName = "${SourceProject}-cos-1"
  $sourceRedisName = "${SourceProject}-cos-redis-1"
  foreach ($name in @($sourceDbName,$sourceAppName,$sourceRedisName)) {
    $c = InspectOne $name
    Check ($c.State.Running -and $c.Config.Labels['com.docker.compose.project'] -eq $SourceProject) "Unexpected or stopped source acceptance service: $name"
    Check ($c.NetworkSettings.Networks.ContainsKey($sourceNetwork)) "Source service is outside the named acceptance network: $name"
    $sourceSnapshot[$name] = @{id=$c.Id;started=$c.State.StartedAt;restarts=$c.RestartCount}
  }
  $sourceApp = InspectOne $sourceAppName
  Check ($sourceApp.Image -eq $CandidateID -and $sourceApp.State.Health.Status -eq 'healthy') 'Source COS must be the reviewed synthetic candidate and healthy'
  $sourceDb = InspectOne $sourceDbName
  Check ($sourceDb.Config.Labels['com.docker.compose.service'] -eq 'cos-db' -and $sourceDb.Mounts.Count -eq 1 -and $sourceDb.Mounts[0].Type -eq 'volume') 'Source COS DB is not the expected volume-backed synthetic DB'
  $sourceVolume = $sourceDb.Mounts[0].Name
  Check ($sourceVolume -eq "${SourceProject}_cos-db-data") 'Source database volume identity differs; refusing dump'
  $sourceRedis = InspectOne $sourceRedisName
  Check ($sourceRedis.Config.Labels['com.docker.compose.service'] -eq 'cos-redis' -and $sourceRedis.Mounts.Count -eq 1 -and $sourceRedis.Mounts[0].Name -eq "${SourceProject}_cos-redis-data") 'Source COS Redis identity differs'
  $sourceNetworkInfo = ((Docker @('network','inspect',$sourceNetwork)) -join "`n") | ConvertFrom-Json -AsHashtable -NoEnumerate | Select-Object -First 1
  Check ($sourceNetworkInfo.Labels['com.docker.compose.project'] -eq $SourceProject) 'Source network owner differs'
  $sourceComposeContainers = @(Docker @('ps','-aq','--filter',"label=com.docker.compose.project=$SourceProject")) | Where-Object { $_ }
  $sourceSnapshot.container_count = $sourceComposeContainers.Count
  Check ($sourceComposeContainers.Count -ge 9) 'Expected acceptance services are missing from the source project'

  $sourcePreflightSql = @"
SELECT json_build_object(
 'non_synthetic_users',(SELECT count(*) FROM users WHERE email NOT LIKE '%@acceptance.invalid'),
 'non_synthetic_accounts',(SELECT count(*) FROM accounts WHERE COALESCE(credentials->>'base_url','') !~ '^http://key-route-existing-[a-f0-9]{12}-mock:8080/v1$'),
 'proxies',(SELECT count(*) FROM proxies),
 'channel_monitors',(SELECT count(*) FROM channel_monitors),
 'accounts',(SELECT count(*) FROM accounts),
 'users',(SELECT count(*) FROM users),
 'migrations',(SELECT count(*) FROM schema_migrations));
"@
  $sourceSql = $sourcePreflightSql | & docker.exe exec -i $sourceDbName psql -X -qAt -U acceptance -d sub2api --set=ON_ERROR_STOP=1
  Check ($LASTEXITCODE -eq 0) 'Read-only source classification query failed'
  $sourceClassification = (($sourceSql -join "`n").Trim()) | ConvertFrom-Json -AsHashtable
  Check ($sourceClassification.non_synthetic_users -eq 0 -and $sourceClassification.non_synthetic_accounts -eq 0 -and $sourceClassification.proxies -eq 0 -and $sourceClassification.channel_monitors -eq 0) 'Source database contains non-synthetic credentials, proxies, or active monitors; stop before dump'
  $evidence.source = @{container_ids=$sourceSnapshot.Clone();volume=$sourceVolume;classification=$sourceClassification;network_internal=[bool]$sourceNetworkInfo.Internal}
  Check (((Docker @('volume','inspect',$sourceVolume,'--format','{{index .Labels "com.docker.compose.project"}}')) -join '').Trim() -eq $SourceProject) 'Source volume ownership differs'
  Save

  $null = Docker @('volume','create','--label',"$runLabel=$run",$dbVolume)
  $created.Add(@{kind='volume';name=$dbVolume})
  $null = Docker @('volume','create','--label',"$runLabel=$run",$redisVolume)
  $created.Add(@{kind='volume';name=$redisVolume})
  $null = Docker @('network','create','--internal','--label',"$runLabel=$run",$network)
  $created.Add(@{kind='network';name=$network})
  $netInfo = ((Docker @('network','inspect',$network)) -join "`n") | ConvertFrom-Json -AsHashtable -NoEnumerate | Select-Object -First 1
  Check ($netInfo.Internal -and $netInfo.Labels[$runLabel] -eq $run) 'Private network is not internal/owned by this run'

  $pgImage='postgres:18-alpine'; $redisImage='redis:8-alpine'; $nodeImage='node:24-alpine'
  foreach($image in @($pgImage,$redisImage,$nodeImage)){ $null = Docker @('image','inspect',$image) }
  $null = Docker @('run','--detach','--pull','never','--name',$dbName,'--label',"$runLabel=$run",'--network',$network,'--network-alias','pg','--env','POSTGRES_USER=acceptance','--env','POSTGRES_PASSWORD=acceptance-only','--env','POSTGRES_DB=sub2api','--mount',"type=volume,source=$dbVolume,target=/var/lib/postgresql",$pgImage)
  $created.Add(@{kind='container';name=$dbName})
  $null = Docker @('run','--detach','--pull','never','--name',$redisName,'--label',"$runLabel=$run",'--network',$network,'--network-alias','redis','--mount',"type=volume,source=$redisVolume,target=/data",$redisImage,'redis-server','--appendonly','yes')
  $created.Add(@{kind='container';name=$redisName})
  WaitPg
  Check ((Docker @('exec',$redisName,'redis-cli','PING') -join '').Trim() -eq 'PONG') 'Private Redis did not answer PING'

  # Raw SQL is streamed from source pg_dump to a new host file, then imported
  # only into the disposable volume. It never targets a source data directory.
  $dumpCommand = "docker.exe exec $sourceDbName pg_dump -U acceptance -d sub2api --format=plain --no-owner --no-privileges > `"$dumpPath`""
  $null = & cmd.exe /d /c $dumpCommand
  Check ($LASTEXITCODE -eq 0 -and (Test-Path -LiteralPath $dumpPath) -and (Get-Item -LiteralPath $dumpPath).Length -gt 10000) 'Read-only source pg_dump failed or was unexpectedly small'
  $dumpHash=(Get-FileHash -LiteralPath $dumpPath -Algorithm SHA256).Hash.ToLowerInvariant()
  $null = Docker @('exec',$dbName,'createdb','-U','acceptance','rollbackdata')
  $null = Docker @('cp',$dumpPath,"${dbName}:/tmp/synthetic-cos-clone.sql")
  $null = Docker @('exec',$dbName,'psql','-X','-q','-U','acceptance','-d','rollbackdata','--set=ON_ERROR_STOP=1','-f','/tmp/synthetic-cos-clone.sql')
  $null = Docker @('exec',$dbName,'rm','-f','/tmp/synthetic-cos-clone.sql')
  Remove-Item -LiteralPath $dumpPath -Force
  $cloneClass = CloneSql "SELECT json_build_object('non_synthetic_users',(SELECT count(*) FROM users WHERE email NOT LIKE '%@acceptance.invalid'),'non_synthetic_accounts',(SELECT count(*) FROM accounts WHERE COALESCE(credentials->>'base_url','') !~ '^http://key-route-existing-[a-f0-9]{12}-mock:8080/v1$'),'accounts',(SELECT count(*) FROM accounts),'users',(SELECT count(*) FROM users),'migrations',(SELECT count(*) FROM schema_migrations));"
  $cloneClass = $cloneClass | ConvertFrom-Json -AsHashtable
  # pg_dump uses its own consistent snapshot. Other acceptance runs may add
  # synthetic rows after the earlier read-only preflight, so audit the restored
  # snapshot itself rather than requiring cross-snapshot row counts to match.
  $evidence.clone_snapshot_audit=$cloneClass
  $evidence.source_counts_match_dump_snapshot=($cloneClass.accounts -eq $sourceClassification.accounts -and $cloneClass.users -eq $sourceClassification.users)
  Save
  Check ($cloneClass.non_synthetic_users -eq 0 -and $cloneClass.non_synthetic_accounts -eq 0 -and $cloneClass.accounts -gt 0 -and $cloneClass.users -gt 0) 'Restored clone contains non-synthetic credentials or no acceptance fixtures'
  CloneWrite "UPDATE accounts SET schedulable = FALSE; UPDATE custom_key_route_site SET site_id = gen_random_uuid();"
  Check ((CloneSql 'SELECT count(*) FROM accounts WHERE schedulable;') -eq '0') 'Clone contains an enabled pre-existing upstream account'
  $migrationSnapshot=CloneSql "SELECT COALESCE(json_agg(json_build_object('filename',filename,'checksum',checksum) ORDER BY filename),'[]'::json) FROM schema_migrations;"
  $evidence.clone = @{classification=$cloneClass;source_dump_sha256=$dumpHash;existing_accounts_disabled=$true;site_namespace_rotated=$true;migrations_before=$migrationSnapshot|ConvertFrom-Json -AsHashtable -NoEnumerate}
  Save

  $null = Docker @('run','--detach','--pull','never','--name',$mockName,'--label',"$runLabel=$run",'--network',$network,'--network-alias','mock','--mount',"type=bind,source=$fixture,target=/fixture/server.mjs,readonly",$nodeImage,'node','/fixture/server.mjs')
  $created.Add(@{kind='container';name=$mockName})
  $null = Docker @('exec',$mockName,'node','-e',"fetch('http://mock:8080/health').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(2))")

  $evidence.status='running'; Save
  $stageResults = @{}
  $runClient = @'
const fs=require('node:fs'),p=JSON.parse(fs.readFileSync(0,'utf8'));
if(!['app','mock'].includes(p.host)||!p.path.startsWith('/')||p.path.startsWith('//'))throw Error('Target rejected');
(async()=>{const h={};if(p.token)h.authorization='Bearer '+p.token;if(p.body!==null)h['content-type']='application/json';
const r=await fetch('http://'+p.host+':8080'+p.path,{method:p.method,headers:h,body:p.body===null?undefined:JSON.stringify(p.body),redirect:'error',signal:AbortSignal.timeout(30000)});
process.stdout.write(JSON.stringify({status:r.status,content:await r.text(),etag:r.headers.get('etag')}));})().catch(e=>{process.stderr.write(e.name+': '+e.message);process.exitCode=1});
'@
  function Call([string]$Method,[string]$Path,[string]$Token='',[object]$Body=$null,[string]$Target='app',[int]$Expected=200) {
    $payload=@{method=$Method;path=$Path;token=$Token;body=$Body;host=$Target}|ConvertTo-Json -Depth 18 -Compress
    $raw=$payload | & docker.exe exec -i $mockName node -e $runClient
    if($LASTEXITCODE -ne 0){throw "Synthetic HTTP client failed: $Method $Path"}
    $result=($raw -join "`n")|ConvertFrom-Json -AsHashtable
    if($result.status -ne $Expected){throw "HTTP $Method $Path expected $Expected, got $($result.status)"}
    $json=$null; if($result.content){try{$json=$result.content|ConvertFrom-Json -AsHashtable -NoEnumerate}catch{}}
    return @{Status=[int]$result.status;Content=[string]$result.content;Json=$json;ETag=[string]$result.etag}
  }
  function AdminCall([string]$Method,[string]$Path,[object]$Body=$null,[int]$Expected=200){return Call $Method "/api/v1$Path" $script:adminToken $Body 'app' $Expected}
  function AppEnv {
    return @(
      'AUTO_SETUP=true','SERVER_HOST=0.0.0.0','SERVER_PORT=8080','SERVER_MODE=release','RUN_MODE=standard',
      'DATABASE_HOST=pg','DATABASE_PORT=5432','DATABASE_USER=acceptance','DATABASE_PASSWORD=acceptance-only','DATABASE_DBNAME=rollbackdata','DATABASE_SSLMODE=disable',
      'REDIS_HOST=redis','REDIS_PORT=6379','REDIS_DB=0','ADMIN_EMAIL=admin-cos@acceptance.invalid','ADMIN_PASSWORD=acceptance-admin-password',
      ('JWT_SECRET='+'5'*64),('TOTP_ENCRYPTION_KEY='+'6'*64),
      'BATCH_IMAGE_ENABLED=true','BATCH_IMAGE_QUEUE_ENABLED=true','BATCH_IMAGE_DEFAULT_IMAGE_SIZE=2K'
    )
  }
  function StartApp([string]$Tag,[string]$ExpectedID) {
    Check (((Docker @('image','inspect',$Tag,'--format','{{.Id}}')) -join '').Trim() -eq $ExpectedID) "Image changed before start: $Tag"
    $dockerRunArgs=@('run','--detach','--pull','never','--name',$appName,'--label',"$runLabel=$run",'--network',$network,'--network-alias','app')
    foreach($entry in (AppEnv)){$dockerRunArgs+=@('--env',$entry)}
    $dockerRunArgs+=$Tag
    $null=Docker $dockerRunArgs
    $created.Add(@{kind='container';name=$appName;image=$ExpectedID})
    WaitApp $ExpectedID
    $running=InspectOne $appName
    Check ($running.Image -eq $ExpectedID -and $running.Config.Labels[$runLabel] -eq $run) 'Application image or ownership label changed'
    Check (((Docker @('inspect',$appName,'--format','{{json .HostConfig.PortBindings}}')) -join '').Trim() -eq 'null') 'Application unexpectedly published a host port'
    Check ((Call 'GET' '/health').Status -eq 200) 'Application health endpoint failed'
    return @{container_id=$running.Id;image_id=$running.Image;health=$running.State.Health.Status;restart_count=$running.RestartCount;published_ports=0}
  }
  function CaptureLogs([string]$Label) {
    $logs=(Docker @('logs','--timestamps',$appName)) -join "`n"
    foreach($secret in @($script:apiKey,$script:adminToken,$script:userToken)){if($secret){$logs=$logs.Replace($secret,'<redacted-test-token>')}}
    $path=Join-Path $OutputDirectory "$Label.application.log"
    [IO.File]::WriteAllText($path,$logs,[Text.UTF8Encoding]::new($false))
    $appLogs.Add($path)
  }
  function ReadLedger {
    $q="SELECT json_build_object('balance',u.balance,'quota_used',k.quota_used,'usage_count',(SELECT count(*) FROM usage_logs WHERE api_key_id=k.id),'dedup_count',(SELECT count(*) FROM usage_billing_dedup WHERE api_key_id=k.id),'last_usage',(SELECT row_to_json(x) FROM (SELECT id,group_id,account_id,input_tokens,output_tokens,total_cost,actual_cost,rate_multiplier FROM usage_logs WHERE api_key_id=k.id ORDER BY id DESC LIMIT 1)x)) FROM users u JOIN api_keys k ON k.user_id=u.id WHERE u.id=$userId AND k.id=$keyId;"
    $raw=$q | & docker.exe exec -i $dbName psql -X -qAt -U acceptance -d rollbackdata --set=ON_ERROR_STOP=1
    Check ($LASTEXITCODE -eq 0) 'Ledger query failed'
    return (($raw -join "`n").Trim() | ConvertFrom-Json -AsHashtable)
  }
  function Request([string]$Label,[string]$Model,[long]$Group,[long]$Account,[decimal]$Rate,[string]$Slot) {
    Check ($script:gatewayCount -lt 6) 'Six-request budget exhausted'
    $before=ReadLedger
    $statsBefore=(Call 'GET' '/stats' '' $null 'mock').Json
    $script:gatewayCount++
    $started=[DateTime]::UtcNow.ToString('o')
    $response=Call 'POST' '/v1/responses' $apiKey @{model=$Model;input="synthetic rollback $Label";stream=$false} 'app' 200
    Check ($response.Content.Contains('backup-ok')) "$Label response did not match the fake upstream completion"
    $statsAfter=(Call 'GET' '/stats' '' $null 'mock').Json
    $newAttempts=@($statsAfter | Select-Object -Skip @($statsBefore).Count)
    Check ($newAttempts.Count -eq 1 -and $newAttempts[0].slot -eq $Slot -and $newAttempts[0].model -eq $Model) "$Label contacted an unexpected upstream account"
    $deadline=[DateTime]::UtcNow.AddSeconds(20)
    do{$after=ReadLedger;if($after.usage_count -ge $before.usage_count+1){break};Start-Sleep -Milliseconds 250}while([DateTime]::UtcNow -lt $deadline)
    $u=$after.last_usage
    Check ($after.usage_count -eq $before.usage_count+1 -and $after.dedup_count -eq $before.dedup_count+1) "$Label was not settled exactly once"
    Check ($u.group_id -eq $Group -and $u.account_id -eq $Account -and $u.input_tokens -eq 10 -and $u.output_tokens -eq 2) "$Label ledger group/account/tokens differ"
    Check ([decimal]$u.rate_multiplier -eq $Rate -and [decimal]$u.total_cost -gt 0) "$Label did not use the actual group's pricing multiplier"
    $charge=[Math]::Round(([decimal]$u.total_cost*$Rate),8,[MidpointRounding]::AwayFromZero)
    Check ([decimal]$before.balance-[decimal]$after.balance -eq $charge -and [decimal]$after.quota_used-[decimal]$before.quota_used -eq $charge) "$Label balance or Key quota debit differs"
    Check ([Math]::Abs([decimal]$u.actual_cost-$charge) -le [decimal]'0.00000001') "$Label recorded cost differs from native charge"
    return @{label=$Label;model=$Model;status=$response.Status;request_started=$started;upstream=@($newAttempts);before=$before;after=$after;usage=$u;charged=$charge;settled_once=$true}
  }

  function PrepareSyntheticRoute {
    $health=(Call 'GET' '/health')
    $login=(Call 'POST' '/api/v1/auth/login' '' @{email='admin-cos@acceptance.invalid';password='acceptance-admin-password'})
    $script:adminToken=[string]$login.Json.data.access_token
    Check ([bool]$adminToken) 'Synthetic admin login did not return a token'
    $compliance=(AdminCall 'GET' '/admin/compliance').Json.data
    if($compliance.required){$null=AdminCall 'POST' '/admin/compliance/accept' @{phrase=$compliance.ack_phrase_en;language='en'}}
    $email="rollback-$run@acceptance.invalid"
    $user=(AdminCall 'POST' '/admin/users' @{email=$email;password='Synthetic-Rollback-2026!';role='user';balance=10;concurrency=5}).Json.data
    $script:userId=[long]$user.id
    $userLogin=(Call 'POST' '/api/v1/auth/login' '' @{email=$email;password='Synthetic-Rollback-2026!'})
    $script:userToken=[string]$userLogin.Json.data.access_token
    Check ($userId -gt 0 -and [bool]$userToken) 'Synthetic route user was not created'
    $primary=(AdminCall 'POST' '/admin/groups' @{name="rollback-$run-primary";platform='openai';subscription_type='standard';rate_multiplier=2}).Json.data
    $backup=(AdminCall 'POST' '/admin/groups' @{name="rollback-$run-backup";platform='openai';subscription_type='standard';rate_multiplier=0.5}).Json.data
    $script:primaryGroup=[long]$primary.id;$script:backupGroup=[long]$backup.id
    Check ($primaryGroup -gt 0 -and $backupGroup -gt 0) 'Synthetic route groups were not created'
    $pricing1=@(@{platform='openai';models=@('gpt-4o-mini');billing_mode='token';input_price=0.000001;output_price=0.000002})
    $pricing2=@(@{platform='openai';models=@('gpt-4o-mini','rollback-backup-model');billing_mode='token';input_price=0.000001;output_price=0.000002})
    $null=AdminCall 'PUT' "/admin/groups/$primaryGroup" @{model_pricing=$pricing1}
    $null=AdminCall 'PUT' "/admin/groups/$backupGroup" @{model_pricing=$pricing2}
    $makeCredentials={param($slot,$modelList) @{api_key="success:$slot";base_url='http://mock:8080/v1';model_mapping=$modelList}}
    $acct1=(AdminCall 'POST' '/admin/accounts' @{name="rollback-$run-primary-account";platform='openai';type='apikey';concurrency=5;priority=0;rate_multiplier=1;group_ids=@($primaryGroup);extra=@{openai_passthrough=$false;openai_responses_mode='force_responses'};credentials=(& $makeCredentials 'primary' @{'gpt-4o-mini'='gpt-4o-mini'})}).Json.data
    $acct2=(AdminCall 'POST' '/admin/accounts' @{name="rollback-$run-backup-account";platform='openai';type='apikey';concurrency=5;priority=0;rate_multiplier=1;group_ids=@($backupGroup);extra=@{openai_passthrough=$false;openai_responses_mode='force_responses'};credentials=(& $makeCredentials 'backup' @{'gpt-4o-mini'='gpt-4o-mini';'rollback-backup-model'='rollback-backup-model'})}).Json.data
    $script:primaryAccount=[long]$acct1.id;$script:backupAccount=[long]$acct2.id
    Check ($primaryAccount -gt 0 -and $backupAccount -gt 0) 'Synthetic route accounts were not created'
    Start-Sleep -Seconds 3
    $key=(Call 'POST' '/api/v1/keys' $userToken @{name="rollback-$run-key";group_id=$primaryGroup;backup_group_ids=@($backupGroup);key_route_scope='account_v2';key_route_failure_policy=@{rules=@()};quota=1}).Json.data
    $script:keyId=[long]$key.id;$script:apiKey=[string]$key.key
    Check ($keyId -gt 0 -and [bool]$apiKey) 'Synthetic multi-group API key was not created'
    $loaded=(Call 'GET' "/api/v1/keys/$keyId" $userToken).Json.data
    Check ($loaded.group_id -eq $primaryGroup -and @($loaded.backup_group_ids).Count -eq 1 -and $loaded.backup_group_ids[0] -eq $backupGroup) 'Initial API key group order was not retained'
    if($loaded.key_route_scope){Check ($loaded.key_route_scope -eq 'account_v2') 'Initial account route scope differs'}
    $policy=$loaded.key_route_failure_policy
    Check (($null -eq $policy) -or @($policy.rules).Count -eq 0) 'Initial key failure rules must be explicitly empty'
    $script:routeConfig=@{key_id=$keyId;scope=$loaded.key_route_scope;group_id=$loaded.group_id;backup_group_ids=@($loaded.backup_group_ids);failure_rules=@($policy.rules);primary_group=$primaryGroup;backup_group=$backupGroup;primary_account=$primaryAccount;backup_account=$backupAccount}
  }

  # Each binary runs alone against the same private DB+Redis clone, never the source.
  $stageResults.candidate_start=StartApp $CandidateTag $CandidateID
  PrepareSyntheticRoute
  $phase0=ReadLedger
  $stageResults.candidate_before=@($phase0)
  $stageResults.candidate_initial_primary=Request 'candidate-primary' 'gpt-4o-mini' $primaryGroup $primaryAccount 2 'primary'
  $stageResults.candidate_initial_backup=Request 'candidate-model-on-backup' 'rollback-backup-model' $backupGroup $backupAccount 0.5 'backup'
  CaptureLogs 'candidate-before-stable'
  $script:appSnapshot=InspectOne $appName
  Check ($script:appSnapshot.Image -eq $CandidateID -and $script:appSnapshot.Config.Labels[$runLabel] -eq $run) 'Candidate container identity changed'
  $null=Docker @('stop','--time','10',$appName)
  $null=Docker @('rm',$appName)
  $created.RemoveAt($created.Count-1)

  $stageResults.stable_start=StartApp $StableTag $StableID
  $stableKey=(Call 'GET' "/api/v1/keys/$keyId" $userToken).Json.data
  Check ($stableKey.group_id -eq $primaryGroup -and @($stableKey.backup_group_ids).Count -eq 1 -and $stableKey.backup_group_ids[0] -eq $backupGroup) 'Stable image did not read original ordered group configuration'
  $stablePolicy=$stableKey.key_route_failure_policy
  Check (($null -eq $stablePolicy) -or @($stablePolicy.rules).Count -eq 0) 'Stable image sees a non-empty key failure policy'
  $meta=CloneSql "SELECT json_build_object('scope',scope,'revision',revision,'updated_at',updated_at) FROM custom_api_key_account_routes WHERE api_key_id=$keyId;" | ConvertFrom-Json -AsHashtable
  Check ($meta.scope -eq 'account_v2') 'Stable image modified/dropped the additive key route metadata'
  $migrationStable=CloneSql "SELECT COALESCE(json_agg(json_build_object('filename',filename,'checksum',checksum) ORDER BY filename),'[]'::json) FROM schema_migrations;" | ConvertFrom-Json -AsHashtable -NoEnumerate
  Check ((ConvertTo-Json -Compress -Depth 8 @($migrationStable)) -eq (ConvertTo-Json -Compress -Depth 8 @($evidence.clone.migrations_before))) 'Stable startup changed migration records or checksums'
  $evidence.clone.migrations_after_stable=$migrationStable
  $stageResults.stable_primary=Request 'stable-primary' 'gpt-4o-mini' $primaryGroup $primaryAccount 2 'primary'
  $stageResults.stable_model_on_backup=Request 'stable-model-on-backup' 'rollback-backup-model' $backupGroup $backupAccount 0.5 'backup'
  CaptureLogs 'stable-rollback'
  $null=Docker @('stop','--time','10',$appName)
  $null=Docker @('rm',$appName)
  $created.RemoveAt($created.Count-1)

  $stageResults.candidate_return_start=StartApp $CandidateTag $CandidateID
  $candidateKeyAfter=(Call 'GET' "/api/v1/keys/$keyId" $userToken).Json.data
  Check ($candidateKeyAfter.group_id -eq $primaryGroup -and @($candidateKeyAfter.backup_group_ids).Count -eq 1 -and $candidateKeyAfter.backup_group_ids[0] -eq $backupGroup) 'Candidate did not reload ordered group configuration after rollback round-trip'
  Check ($candidateKeyAfter.key_route_scope -eq 'account_v2' -and @($candidateKeyAfter.key_route_failure_policy.rules).Count -eq 0) 'Candidate did not reload account metadata with rules disabled'
  $metadataAfter=CloneSql "SELECT json_build_object('scope',scope,'revision',revision) FROM custom_api_key_account_routes WHERE api_key_id=$keyId;" | ConvertFrom-Json -AsHashtable
  Check ($metadataAfter.scope -eq 'account_v2') 'Candidate metadata changed during stable interval'
  $migrationsAfterRoundTrip=CloneSql "SELECT COALESCE(json_agg(json_build_object('filename',filename,'checksum',checksum) ORDER BY filename),'[]'::json) FROM schema_migrations;" | ConvertFrom-Json -AsHashtable -NoEnumerate
  Check ((ConvertTo-Json -Compress -Depth 8 @($migrationsAfterRoundTrip)) -eq (ConvertTo-Json -Compress -Depth 8 @($evidence.clone.migrations_before))) 'Candidate round-trip changed migration records/checksums'
  $evidence.clone.migrations_after_candidate_roundtrip=$migrationsAfterRoundTrip
  $stageResults.candidate_after_stable_primary=Request 'candidate-return-primary' 'gpt-4o-mini' $primaryGroup $primaryAccount 2 'primary'
  $stageResults.candidate_after_stable_backup=Request 'candidate-return-model-on-backup' 'rollback-backup-model' $backupGroup $backupAccount 0.5 'backup'
  CaptureLogs 'candidate-after-stable'
  $evidence.route_config=$routeConfig
  $evidence.phases=@($stageResults.GetEnumerator() | ForEach-Object { if($_.Key -like '*primary*' -or $_.Key -like '*backup*'){$_.Value} })
  $evidence.application_logs=@($appLogs)
  $evidence.custom_migrations_stable_preserved=$true
  $evidence.roundtrip_without_restore=$true
  $evidence.total_gateway_requests=$gatewayCount
  $evidence.status='passed'
  Save
} catch {
  $evidence.status='failed'
  $evidence.failure=$_.Exception.Message
  if($appName -and (docker ps -aq --filter "name=^/$appName$") ) {
    try {
      $tail=(Docker @('logs','--timestamps','--tail','250',$appName)) -join "`n"
      foreach($secret in @($apiKey,$adminToken,$userToken)){if($secret){$tail=$tail.Replace($secret,'<redacted-test-token>')}}
      $tail | Set-Content -LiteralPath (Join-Path $OutputDirectory 'failure.application-tail.log') -Encoding utf8
    } catch {}
  }
  try { Save } catch {}
  throw
} finally {
  # Exact run-label verification is required before each own-resource cleanup.
  $cleanupEntries=@($created.ToArray())
  [array]::Reverse($cleanupEntries)
  foreach($entry in $cleanupEntries) {
    try {
      if($entry.kind -eq 'container') {
        $c=InspectOne $entry.name
        if($c.Config.Labels[$runLabel] -ne $run){throw "Refusing to remove unowned container $($entry.name)"}
        if($c.State.Running){$null=Docker @('stop','--time','10',$entry.name)}
        $null=Docker @('rm',$entry.name)
      } elseif($entry.kind -eq 'network') {
        $n=((Docker @('network','inspect',$entry.name)) -join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate|Select-Object -First 1
        if($n.Labels[$runLabel] -ne $run){throw "Refusing to remove unowned network $($entry.name)"}
        $null=Docker @('network','rm',$entry.name)
      } elseif($entry.kind -eq 'volume') {
        $v=((Docker @('volume','inspect',$entry.name)) -join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate|Select-Object -First 1
        if($v.Labels[$runLabel] -ne $run){throw "Refusing to remove unowned volume $($entry.name)"}
        $null=Docker @('volume','rm',$entry.name)
      }
      $evidence.cleanup.Add(@{kind=$entry.kind;name=$entry.name;removed=$true;ownership_verified=$true})
    } catch {
      $evidence.cleanup.Add(@{kind=$entry.kind;name=$entry.name;removed=$false;error=$_.Exception.Message})
      if($evidence.status -eq 'passed'){$evidence.status='passed-with-cleanup-warning'}
    }
  }
  if(Test-Path -LiteralPath $dumpPath){Remove-Item -LiteralPath $dumpPath -Force}
  $evidence.total_gateway_requests=$gatewayCount
  $evidence.application_logs=@($appLogs)
  $sourceUnchanged=$true
  foreach($name in $sourceSnapshot.Keys) {
    if($name -eq 'container_count'){continue}
    try{$current=InspectOne $name;if($current.Id -ne $sourceSnapshot[$name].id -or $current.State.StartedAt -ne $sourceSnapshot[$name].started -or $current.RestartCount -ne $sourceSnapshot[$name].restarts){$sourceUnchanged=$false}}catch{$sourceUnchanged=$false}
  }
  $evidence.source_containers_unchanged=$sourceUnchanged
  if(-not $sourceUnchanged -and $evidence.status -eq 'passed'){$evidence.status='failed-source-container-changed'}
  try{Save}catch{}
}
