[CmdletBinding()]
param(
  [switch]$Execute,
  [string]$Project = 'moshu-reseller-acceptance-c',
  [ValidateSet('main','l1','cos')][string[]]$Sites = @('main','l1','cos'),
  [switch]$AcceptSyntheticAdminCompliance,
  [switch]$ExtendedKeyRules,
  [switch]$RemainingSafetyCasesOnly,
  [string]$ExpectedImageId = 'sha256:b54b0e3d2d0dbbd5ac3f9d95a84ee0b16f0162114a6249ca9b101be4ba40f744',
  [string]$OutputDirectory = ''
)

# Existing synthetic three-site project only. No application/data-service restart,
# SQL writes, pulls, builds, published mock port, or external model requests.
# A fresh labelled mock and API-created resources are removed by exact recorded ID.
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 required' }
if (-not $Execute) { Write-Output 'Use -Execute after reviewing this script. Reuses an acceptance-only project; seven synthetic gateway requests per selected site (thirteen with -ExtendedKeyRules, three with -RemainingSafetyCasesOnly); exact-ID cleanup; redacted evidence retained. Compliance remains untouched unless explicitly opted in.'; return }
function Check([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Docker {
  $result = & docker.exe @args
  if ($LASTEXITCODE -ne 0) { throw "Docker failed: $($args[0])" }
  return $result
}
Check ($Project -match '^moshu-reseller-acceptance(?:-[a-zA-Z0-9_-]+)?$') 'Only dedicated local acceptance projects are allowed'
Check ($ExpectedImageId -match '^sha256:[a-f0-9]{64}$') 'Exact application image ID required'
Check ($Sites.Count -eq @($Sites | Select-Object -Unique).Count) 'Each site may only be selected once'
$run = [Guid]::NewGuid().ToString('N').Substring(0,12)
$prefix = "key-route-existing-$run"
$network = "${Project}_default"
$mockName = "$prefix-mock"
$fixture = (Resolve-Path (Join-Path $PSScriptRoot '../acceptance/mock-failover-upstream.mjs')).Path
if (-not $OutputDirectory) { $OutputDirectory = "D:/projects/$prefix" }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
Check (-not (Test-Path -LiteralPath $OutputDirectory)) 'Report directory must be new'
$containers = @{}; $initial = @{}; $mockId = ''; $site = ''; $token = ''; $userToken = ''; $apiKey = ''; $userId = 0; $keyId = 0
$resources = @{}; $gatewayCalls = 0; $cleanupStandalone = $false
if ($RemainingSafetyCasesOnly) { $ExtendedKeyRules = $true }
$gatewayLimit=$(if($RemainingSafetyCasesOnly){3}elseif($ExtendedKeyRules){13}else{7})*$Sites.Count
$evidence = [ordered]@{status='running';run=$run;project=$Project;image=$ExpectedImageId;started_at=[DateTime]::UtcNow.ToString('o');sites=@{};findings=[Collections.Generic.List[object]]::new();cleanup=[Collections.Generic.List[object]]::new();fixture_sha256=(Get-FileHash -LiteralPath $fixture -Algorithm SHA256).Hash.ToLowerInvariant();limits=@{gateway_requests=$gatewayLimit;paid_models=$false;application_restarts=$false;data_service_changes=$false;mock_published_ports=$false}}
$httpClient = @'
const fs=require('node:fs'),p=JSON.parse(fs.readFileSync(0,'utf8'));
if(!['main','l1','cos','localhost'].includes(p.host)||!p.path.startsWith('/')||p.path.startsWith('//'))throw Error('Non-fixture target');
(async()=>{const h={};if(p.token)h.authorization='Bearer '+p.token;if(p.body!==null)h['content-type']='application/json';
const r=await fetch('http://'+(p.host==='localhost'?'127.0.0.1':p.host)+':8080'+p.path,{method:p.method,headers:h,body:p.body===null?undefined:JSON.stringify(p.body),redirect:'error',signal:AbortSignal.timeout(45000)});
process.stdout.write(JSON.stringify({status:r.status,content:await r.text()}));})().catch(e=>{process.stderr.write(e.message);process.exitCode=1});
'@
function Save { $evidence.gateway_requests=$script:gatewayCalls; $evidence | ConvertTo-Json -Depth 25 | Set-Content -LiteralPath (Join-Path $OutputDirectory 'evidence.json') -Encoding utf8 }
function Http([string]$Method,[string]$Path,[string]$Auth='',[object]$Body=$null,[string]$HostName=$script:site) {
  $payload=@{method=$Method;path=$Path;token=$Auth;body=$Body;host=$HostName}|ConvertTo-Json -Depth 20 -Compress
  if ($script:cleanupStandalone) {
    # A crashed fixture must not prevent exact-ID cleanup of synthetic data.
    # This client is temporary, has no ports/volumes and cannot target production.
    $raw=$payload | & docker.exe run --rm -i --pull never --network $network node:24-alpine node -e $httpClient
  } else {
    $raw=$payload | & docker.exe exec -i $script:mockId node -e $httpClient
  }
  if ($LASTEXITCODE -ne 0) { throw "Synthetic HTTP transport failed: $Method $Path; not retried" }
  $r=($raw -join "`n")|ConvertFrom-Json -AsHashtable; $data=$null
  if ($r.content) { try { $data=$r.content|ConvertFrom-Json -AsHashtable -NoEnumerate } catch {} }
  return @{Status=[int]$r.status;Content=[string]$r.content;Json=$data}
}
function Api([string]$Method,[string]$Path,[string]$Auth=$script:token,[object]$Body=$null,[int]$Expected=200) {
  $r=Http $Method "/api/v1$Path" $Auth $Body
  Check ($r.Status -eq $Expected) "$site $Method $Path expected $Expected, got $($r.Status)"
  return $r
}
function Sql([string]$Query) {
  Check ($Query.TrimStart().StartsWith('SELECT ')) 'Only read-only SELECT is allowed'
  $raw=$Query | & docker.exe exec -i $containers["$site-db"] psql -X -qAt -U acceptance -d sub2api --set=ON_ERROR_STOP=1
  if ($LASTEXITCODE -ne 0) { throw 'Acceptance read-only SQL failed' }
  return ($raw -join "`n").Trim()
}
function Ledger {
  return (Sql "SELECT json_build_object('balance',u.balance,'quota_used',k.quota_used,'usage_count',(SELECT COUNT(*) FROM usage_logs WHERE api_key_id=k.id),'dedup_count',(SELECT COUNT(*) FROM usage_billing_dedup WHERE api_key_id=k.id),'last_usage',(SELECT row_to_json(x) FROM (SELECT id,api_key_id,group_id,account_id,input_tokens,output_tokens,total_cost,actual_cost,rate_multiplier FROM usage_logs WHERE api_key_id=k.id ORDER BY id DESC LIMIT 1)x)) FROM users u JOIN api_keys k ON k.user_id=u.id WHERE u.id=$userId AND k.id=$keyId;")|ConvertFrom-Json -AsHashtable
}
function Stats { return ,@((Http GET '/stats' '' $null 'localhost').Json) }
function Credentials([string]$Scenario,[string]$Slot,[bool]$Supports=$true) {
  $mapping=if($Supports){@{'gpt-4o-mini'='gpt-4o-mini';"$Slot-model"="$Slot-model"}}else{@{'unrelated-model'='unrelated-model'}}
  return @{api_key="${Scenario}:$Slot";base_url="http://${mockName}:8080/v1";model_mapping=$mapping}
}
function Set-Policy([int]$Code,[int]$Cooldown=120) {
  $null=Api PUT "/keys/$keyId" $userToken @{key_route_failure_policy=@{rules=@(@{type='status_code';status_codes=@($Code);threshold=1;window_seconds=60;cooldown_seconds=$Cooldown})}}
}
function Set-Primary([string]$Scenario,[bool]$Schedulable=$true) {
  $null=Api PUT "/admin/accounts/$($accountIds[0])" $token @{credentials=(Credentials $Scenario 'primary')}
  $updated=(Api POST "/admin/accounts/$($accountIds[0])/schedulable" $token @{schedulable=$Schedulable}).Json.data
  Check ($updated.schedulable -eq $Schedulable) 'Synthetic schedulable fixture change was not applied'
  Start-Sleep -Seconds 3
}
function Gateway([string]$Label,[string]$ExpectedSlot,[long]$ExpectedGroup,[decimal]$Rate,[bool]$Stream=$false,[string]$Mode='success') {
  Check ($script:gatewayCalls -lt $gatewayLimit) 'Fixed gateway request budget exhausted'
  $before=Ledger; $statsBefore=Stats; $script:gatewayCalls++; Save
  $clock=[Diagnostics.Stopwatch]::StartNew()
  $r=Http POST '/v1/responses' $apiKey @{model='gpt-4o-mini';input="synthetic $prefix $Label";stream=$Stream}
  $clock.Stop()
  $statsAfter=Stats; $calls=@($statsAfter|Select-Object -Skip $statsBefore.Count)
  $phase=@{label=$Label;status=$r.Status;upstream=$calls;elapsed_ms=$clock.ElapsedMilliseconds}; $evidence.sites[$site].last_attempt=$phase; Save
  if ($Mode -eq 'success') {
    Check ($r.Status -eq 200 -and $r.Content.Contains('backup-ok')) "$site $Label did not complete successfully"
    $selected=@($calls|Where-Object {$_.slot -eq $ExpectedSlot})
    Check ($selected.Count -eq 1 -and $selected[0].stream -eq $Stream) "$site $Label selected/replayed wrong target"
    Check (@($calls|Where-Object {$_.slot -ne $ExpectedSlot -and $_.slot -ne 'primary'}).Count -eq 0) "$site $Label contacted an unexpected backup"
    $deadline=[DateTime]::UtcNow.AddSeconds(20)
    do { $after=Ledger; if($after.usage_count -gt $before.usage_count){break};Start-Sleep -Milliseconds 250 } while([DateTime]::UtcNow -lt $deadline)
    $u=$after.last_usage
    Check ($after.usage_count -eq $before.usage_count+1 -and $after.dedup_count -eq $before.dedup_count+1) "$site $Label did not settle exactly once"
    Check ($u.group_id -eq $ExpectedGroup -and $u.account_id -eq $accountIds[$slots.IndexOf($ExpectedSlot)] -and $u.input_tokens -eq 10 -and $u.output_tokens -eq 2) "$site $Label usage identity/tokens differ"
    Check ([decimal]$u.rate_multiplier -eq $Rate -and [decimal]$u.total_cost -gt 0) "$site $Label effective group rate differs"
    $cost=[Math]::Round(([decimal]$u.total_cost*$Rate),8,[MidpointRounding]::AwayFromZero)
    Check ([decimal]$before.balance-[decimal]$after.balance -eq $cost -and [decimal]$after.quota_used-[decimal]$before.quota_used -eq $cost) "$site $Label balance/quota debit differs"
    Check ([Math]::Abs([decimal]$u.actual_cost-$cost) -le [decimal]'0.00000001') "$site $Label usage cost differs"
    $phase.usage=$u;$phase.charged=$cost
  } else {
    Start-Sleep -Milliseconds 500; $after=Ledger
    Check (@($calls|Where-Object {$_.slot -ne 'primary'}).Count -eq 0) "$site $Label replayed after output/rejection"
    if($Mode -eq 'partial'){Check ($r.Status -eq 200 -and $r.Content.Contains('already-output') -and -not $r.Content.Contains('backup-ok')) "$site $Label lost original partial stream"}
    if($Mode -eq 'unavailable'){Check ($r.Status -eq 503 -and $calls.Count -eq 0) "$site $Label reached upstream despite all accounts unavailable"}
    Check ($after.balance -eq $before.balance -and $after.quota_used -eq $before.quota_used) "$site $Label unexpectedly charged failed fixture"
    $phase.debit=0
  }
  $evidence.sites[$site].phases.Add($phase); Save
}

# Fail closed before creating anything if labels, runtime, networking or data
# credentials differ from the dedicated synthetic Compose definition.
$net=((Docker network inspect $network)-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate
Check ($net[0].Labels['com.docker.compose.project'] -eq $Project) 'Unexpected network owner'
$evidence.network_internal=[bool]$net[0].Internal
foreach($s in @('main','l1','cos','main-db','l1-db','cos-db','main-redis','l1-redis','cos-redis')) {
  $ids=@(Docker ps -q --filter "label=com.docker.compose.project=$Project" --filter "label=com.docker.compose.service=$s")
  Check ($ids.Count -eq 1) "Expected exactly one running synthetic $s"
  $c=(((Docker inspect $ids[0])-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
  Check ($c.Config.Labels['com.docker.compose.project'] -eq $Project -and $c.NetworkSettings.Networks.Count -eq 1 -and $c.NetworkSettings.Networks.ContainsKey($network)) "Unexpected container ownership/network: $s"
  if($s -in @('main','l1','cos')) {
    Check ($c.Image -eq $ExpectedImageId -and $c.Config.Env.Contains('DATABASE_PASSWORD=acceptance-only') -and $c.Config.Env.Contains('ADMIN_PASSWORD=acceptance-admin-password')) "Unexpected application image or non-synthetic credentials: $s"
    foreach($bindings in $c.HostConfig.PortBindings.Values){foreach($b in $bindings){Check ($b.HostIp -eq '127.0.0.1') 'Application is publicly published'}}
  }
  $containers[$s]=$c.Id;$initial[$s]=@{id=$c.Id;started=$c.State.StartedAt;restarts=$c.RestartCount}
}
$evidence.initial_containers=$initial.Clone()
$null=Docker image inspect node:24-alpine
$null=New-Item -ItemType Directory -Path $OutputDirectory
try {
  Save
  $mockId=(Docker run --detach --pull never --name $mockName --label "moshu.acceptance.run=$run" --network $network --mount "type=bind,source=$fixture,target=/fixture/server.mjs,readonly" node:24-alpine node /fixture/server.mjs).Trim()
  $mock=(((Docker inspect $mockId)-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
  Check ($mock.Config.Labels['moshu.acceptance.run'] -eq $run -and $mock.HostConfig.PortBindings.Count -eq 0) 'Unexpected temporary mock ownership/published ports'
  Start-Sleep -Seconds 1
  Check ((Http GET '/health' '' $null 'localhost').Status -eq 200) 'Mock is not healthy'
  $slots=[Collections.Generic.List[string]]@('primary','backup','third')
  foreach($site in $Sites) {
    $resources[$site]=@{groups=[Collections.Generic.List[long]]::new();accounts=[Collections.Generic.List[long]]::new();keys=[Collections.Generic.List[long]]::new();users=[Collections.Generic.List[long]]::new()}
    $evidence.sites[$site]=@{phases=[Collections.Generic.List[object]]::new();resources=$resources[$site]}
    Check ((Http GET '/health').Status -eq 200) "$site unhealthy before acceptance"
    $token=[string](Api POST '/auth/login' '' @{email="admin-$site@acceptance.invalid";password='acceptance-admin-password'}).Json.data.access_token
    Check ([bool]$token) "$site login returned no token"
    $compliance=(Api GET '/admin/compliance').Json.data
    if($compliance.required) {
      Check ([bool]$AcceptSyntheticAdminCompliance) "$site compliance requires changing existing admin state; stop"
      $null=Api POST '/admin/compliance/accept' $token @{phrase=$compliance.ack_phrase_en;language='en'}
      $evidence.sites[$site].synthetic_admin_compliance_accepted=$true;Save
    }
    $email="$prefix-$site@acceptance.invalid"
    $userId=[long](Api POST '/admin/users' $token @{email=$email;password='Synthetic-Key-Routing-2026!';role='user';balance=10;concurrency=5}).Json.data.id
    $resources[$site].users.Add($userId)
    $userToken=[string](Api POST '/auth/login' '' @{email=$email;password='Synthetic-Key-Routing-2026!'}).Json.data.access_token
    $groupIds=@();$accountIds=@()
    foreach($slot in $slots) {
      $rate=switch($slot){'primary'{2}'backup'{0.5}'third'{0.75}}
      $g=[long](Api POST '/admin/groups' $token @{name="$prefix-$site-$slot";platform='openai';subscription_type='standard';rate_multiplier=$rate}).Json.data.id
      $groupIds+=$g;$resources[$site].groups.Add($g);Save
      $a=[long](Api POST '/admin/accounts' $token @{name="$prefix-$site-$slot";platform='openai';type='apikey';concurrency=5;priority=0;rate_multiplier=1;group_ids=@($g);extra=@{openai_passthrough=$false;openai_responses_mode='force_responses'};credentials=(Credentials 'success' $slot ($slot -ne 'primary'))}).Json.data.id
      $accountIds+=$a;$resources[$site].accounts.Add($a);Save
    }
    $primaryGroup=$groupIds[0];$backupGroup=$groupIds[1];$thirdGroup=$groupIds[2]
    $created=(Api POST '/keys' $userToken @{name="$prefix-$site";group_id=$primaryGroup;backup_group_ids=@($backupGroup,$thirdGroup);quota=1;key_route_failure_policy=@{rules=@()}}).Json.data
    $keyId=[long]$created.id;$apiKey=[string]$created.key;$resources[$site].keys.Add($keyId);Save
    Check ($keyId -gt 0 -and [bool]$apiKey) 'Synthetic Key missing'
    $null=Api PUT "/keys/$keyId" $token @{name='must-not-change-other-owner'} 403
    $null=Api PUT "/keys/$keyId" $userToken @{backup_group_ids=@($backupGroup,$backupGroup)} 400
    $dup=@{rules=@(@{type='status_code';status_codes=@(502);threshold=1;window_seconds=60;cooldown_seconds=120},@{type='status_code';status_codes=@(502);threshold=1;window_seconds=60;cooldown_seconds=120})}
    $null=Api PUT "/keys/$keyId" $userToken @{key_route_failure_policy=$dup} 400
    $read=(Api GET "/keys/$keyId" $userToken).Json.data
    Check (($read.backup_group_ids -join ',') -eq (@($backupGroup,$thirdGroup)-join ',') -and $read.key_route_failure_policy.rules.Count -eq 0) 'Rejected update modified stored configuration'
    $models=Http GET '/v1/models' $apiKey
    Check ($models.Status -eq 200) "$site model-list HTTP request failed"
    $backupModelsVisible=@($models.Json.data.id).Contains('backup-model') -and @($models.Json.data.id).Contains('third-model')
    if(-not $backupModelsVisible){$evidence.findings.Add(@{site=$site;code='MODELS_PRIMARY_ONLY';observed_ids=@($models.Json.data.id);message='Metadata currently lists primary-group models only, while text requests may route to backup groups.'})}
    $evidence.sites[$site].validation=@{ownership_denied=403;duplicate_backup=400;duplicate_rule=400;backup_models_visible=$backupModelsVisible}
    if(-not $RemainingSafetyCasesOnly) {
    Start-Sleep -Seconds 3
    Gateway 'primary-model-missing-http' 'backup' $backupGroup 0.5
    $null=Api PUT "/keys/$keyId" $userToken @{backup_group_ids=@($thirdGroup,$backupGroup)}
    Gateway 'backup-priority-http' 'third' $thirdGroup 0.75
    $null=Api PUT "/keys/$keyId" $userToken @{backup_group_ids=@($backupGroup,$thirdGroup)}
    Set-Primary 'success' $false
    Gateway 'primary-unschedulable-sse' 'backup' $backupGroup 0.5 $true
    Set-Primary 'server'
    Set-Policy 502
    Gateway 'status502-cross-group-http' 'backup' $backupGroup 0.5
    Set-Primary 'stream'
    Set-Policy 503
    Gateway 'failed-before-content-sse' 'backup' $backupGroup 0.5 $true
    # A new key AND account isolate this safety test from prior route cooldowns
    # and native request-scoped runtime observations. This must actually contact
    # the partial-output primary, not merely skip it at preflight.
    $null=Api POST "/admin/accounts/$($accountIds[0])/schedulable" $token @{schedulable=$false}
    $partialAccount=[long](Api POST '/admin/accounts' $token @{name="$prefix-$site-partial";platform='openai';type='apikey';concurrency=5;priority=0;rate_multiplier=1;group_ids=@($primaryGroup);extra=@{openai_passthrough=$true;openai_responses_mode='force_responses'};credentials=(Credentials 'partial' 'primary')}).Json.data.id
    $accountIds[0]=$partialAccount;$resources[$site].accounts.Add($partialAccount)
    $partialKey=(Api POST '/keys' $userToken @{name="$prefix-$site-partial";group_id=$primaryGroup;backup_group_ids=@($backupGroup,$thirdGroup);quota=1;key_route_failure_policy=@{rules=@(@{type='status_code';status_codes=@(503);threshold=1;window_seconds=60;cooldown_seconds=120})}}).Json.data
    $keyId=[long]$partialKey.id;$apiKey=[string]$partialKey.key;$resources[$site].keys.Add($keyId);Save
    Start-Sleep -Seconds 3
    Gateway 'partial-output-never-replayed' '' 0 0 $true 'partial'
    foreach($id in $accountIds){$null=Api POST "/admin/accounts/$id/schedulable" $token @{schedulable=$false}}
    Start-Sleep -Seconds 3
    Gateway 'all-accounts-unavailable' '' 0 0 $false 'unavailable'
    }
    if($ExtendedKeyRules) {
      foreach($id in $accountIds){$null=Api POST "/admin/accounts/$id/schedulable" $token @{schedulable=$false}}
      $fresh=[long](Api POST '/admin/accounts' $token @{name="$prefix-$site-ttft";platform='openai';type='apikey';concurrency=5;priority=0;rate_multiplier=1;group_ids=@($primaryGroup);extra=@{openai_passthrough=$true;openai_responses_mode='force_responses'};credentials=(Credentials 'success' 'primary')}).Json.data.id
      $accountIds[0]=$fresh;$resources[$site].accounts.Add($fresh);Save
      $null=Api POST "/admin/accounts/$($accountIds[1])/schedulable" $token @{schedulable=$true}
      Set-Primary 'slow_headers'
      $ttft=@{rules=@(@{type='ttft';first_content_seconds=5;threshold=2;sample_size=3;cooldown_seconds=120})}
      $null=Api PUT "/keys/$keyId" $userToken @{key_route_failure_policy=$ttft}
      if(-not $RemainingSafetyCasesOnly) {
      Gateway 'ttft-silent-http' 'backup' $backupGroup 0.5
      Set-Primary 'slow_prelude'
      Gateway 'ttft-prelude-sse' 'backup' $backupGroup 0.5 $true
      Set-Primary 'success'
      Gateway 'ttft-next-request-cooldown' 'backup' $backupGroup 0.5
      }
      # A changed policy invalidates its old cooldown; known usage disarms replay.
      $ttft.rules[0].first_content_seconds=6
      $null=Api PUT "/keys/$keyId" $userToken @{key_route_failure_policy=$ttft}
      Set-Primary 'slow_usage'
      Gateway 'ttft-known-usage-no-replay' 'primary' $primaryGroup 2 $true
      $null=Api POST "/admin/accounts/$($accountIds[0])/schedulable" $token @{schedulable=$false}
      $null=Api PUT "/admin/accounts/$($accountIds[1])" $token @{credentials=(Credentials 'slow_headers' 'backup')}
      Start-Sleep -Seconds 3
      Gateway 'ttft-last-eligible-not-truncated' 'backup' $backupGroup 0.5
      $policy=@{enabled=$true;models=@('gpt-4o-mini')}
      $null=Api PUT "/keys/$keyId" $userToken @{key_model_policy=$policy}
      $saved=(Api GET "/keys/$keyId" $userToken).Json.data
      Check ($saved.key_model_policy.enabled -and ($saved.key_model_policy.models -join ',') -eq 'gpt-4o-mini') 'Key model policy did not persist'
      $catalog=Http GET '/v1/models' $apiKey
      Check ($catalog.Status -eq 200 -and @($catalog.Json.data|Where-Object {$_.id -ne 'gpt-4o-mini'}).Count -eq 0) 'Restricted Key model catalog leaked another model'
      $before=Ledger;$statsBefore=Stats;$gatewayCalls++;Save
      $denied=Http POST '/v1/responses' $apiKey @{model='backup-model';input='must not dispatch';stream=$false}
      $after=Ledger;$statsAfter=Stats
      Check ($denied.Status -eq 404 -and $statsBefore.Count -eq $statsAfter.Count -and $before.balance -eq $after.balance -and $before.quota_used -eq $after.quota_used) 'Restricted model reached upstream or charged'
      $evidence.sites[$site].model_policy=@{persisted=$true;catalog_filtered=$true;denied_status=$denied.Status;denied_upstream_calls=0;denied_debit=0}
    }
    $state=(Sql "SELECT json_agg(json_build_object('id',id,'temp',temp_unschedulable_until,'overload',overload_until)) FROM accounts WHERE id IN ($($resources[$site].accounts -join ','));")|ConvertFrom-Json
    Check (@($state|Where-Object {$_.temp -or $_.overload}).Count -eq 0) "$site custom rules globally paused an account"
    $evidence.sites[$site].no_global_pause=$true;$evidence.sites[$site].final_ledger=Ledger;Save
  }
  $evidence.status=if($evidence.findings.Count){'passed-with-findings'}else{'passed'}
} catch {
  $evidence.status='failed';$evidence.error=$_.Exception.Message
  if($mockId){$evidence.mock_logs=(Docker logs --tail 20 $mockId)-join "`n"}
  throw
} finally {
  if($mockId) {
    $cleanupStandalone=(& docker.exe inspect --format '{{.State.Running}}' $mockId) -ne 'true'
    foreach($site in $resources.Keys) {
      try {
        $token=[string](Api POST '/auth/login' '' @{email="admin-$site@acceptance.invalid";password='acceptance-admin-password'}).Json.data.access_token
        foreach($kind in @('keys','accounts','groups','users')) {
          foreach($id in $resources[$site][$kind]) {
            Check ($id -gt 0) 'Refusing cleanup of invalid ID'
            $path=switch($kind){'keys'{"/admin/api-keys/$id"}default{"/admin/$kind/$id"}}
            $r=Api DELETE $path $token
            $evidence.cleanup.Add(@{site=$site;kind=$kind;id=$id;status=$r.Status})
          }
        }
      }catch{$evidence.cleanup.Add(@{site=$site;error=$_.Exception.Message})}
    }
    $mock=(((Docker inspect $mockId)-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
    Check ($mock.Id -eq $mockId -and $mock.Config.Labels['moshu.acceptance.run'] -eq $run) 'Refusing cleanup of unowned mock'
    $null=Docker rm --force $mockId
  }
  foreach($s in $initial.Keys) {
    $c=(((Docker inspect $containers[$s])-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
    Check ($c.Id -eq $initial[$s].id -and $c.State.StartedAt -eq $initial[$s].started -and $c.RestartCount -eq $initial[$s].restarts) "Existing service unexpectedly restarted: $s"
  }
  $evidence.existing_services_unchanged=$true;$evidence.finished_at=[DateTime]::UtcNow.ToString('o');Save
  Write-Output "Acceptance $($evidence.status): $gatewayCalls/$gatewayLimit gateway requests. Redacted evidence: $(Join-Path $OutputDirectory 'evidence.json')"
}
