[CmdletBinding()]
param(
  [switch]$Execute,
  [string]$Project = 'moshu-reseller-acceptance-c',
  [ValidateSet('main','l1','cos')][string[]]$Sites = @('main','l1','cos'),
  [switch]$AcceptSyntheticAdminCompliance,
  [switch]$ExtendedKeyRules,
  [switch]$RemainingSafetyCasesOnly,
  [switch]$SLACasesOnly,
  [switch]$AccountPriorityFallbackOnly,
  [switch]$SharedStateFaultOnly,
  [switch]$LongConnectionOnly,
  [switch]$CatalogueCasesOnly,
  [switch]$CatalogueMetadataOnly,
  [string]$ExpectedImageId = '',
  [string]$OutputDirectory = ''
)

# Existing synthetic three-site project only. No application/data-service restart,
# SQL writes, pulls, builds, published mock port, or external model requests.
# SharedStateFaultOnly additionally creates one private main peer application
# and briefly pauses/unpauses only the recorded synthetic main Redis container.
# LongConnectionOnly sends one bounded 660-second SSE request to each isolated site.
# A fresh labelled mock and API-created resources are removed by exact recorded ID.
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 required' }
if (-not $Execute) { Write-Output 'Use -Execute after reviewing this script. Reuses an acceptance-only project; ten synthetic gateway requests per selected site (sixteen with -ExtendedKeyRules, three with -RemainingSafetyCasesOnly, eleven with -SLACasesOnly, four with -AccountPriorityFallbackOnly, eleven with main-only -SharedStateFaultOnly, three total with three-site -LongConnectionOnly, two with -CatalogueCasesOnly; zero with -CatalogueCasesOnly -CatalogueMetadataOnly). Shared-state mode briefly pauses/unpauses its exact synthetic main Redis and removes its private peer. Long-connection mode runs three concurrent synthetic 660-second SSE requests. Includes exact-ID cleanup and redacted evidence. Compliance remains untouched unless explicitly opted in.'; return }
function Check([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Docker {
  $result = & docker.exe @args
  if ($LASTEXITCODE -ne 0) { throw "Docker failed: $($args[0])" }
  return $result
}
Check ($Project -match '^moshu-reseller-acceptance(?:-[a-zA-Z0-9_-]+)?$') 'Only dedicated local acceptance projects are allowed'
Check ($ExpectedImageId -match '^sha256:[a-f0-9]{64}$') 'Exact application image ID required'
Docker image inspect $ExpectedImageId | Out-Null
Check ($Sites.Count -eq @($Sites | Select-Object -Unique).Count) 'Each site may only be selected once'
Check (-not ($SLACasesOnly -and ($ExtendedKeyRules -or $RemainingSafetyCasesOnly -or $AccountPriorityFallbackOnly))) 'SLA-only mode cannot be combined with another gateway suite'
Check (-not ($AccountPriorityFallbackOnly -and ($ExtendedKeyRules -or $RemainingSafetyCasesOnly -or $CatalogueCasesOnly))) 'Account-priority-only mode cannot be combined with another gateway suite'
Check (-not ($CatalogueCasesOnly -and ($SLACasesOnly -or $ExtendedKeyRules -or $RemainingSafetyCasesOnly))) 'Catalogue-only mode cannot be combined with another gateway suite'
Check (-not $CatalogueMetadataOnly -or $CatalogueCasesOnly) 'Metadata-only mode requires -CatalogueCasesOnly'
if($LongConnectionOnly) {
  Check ($Project -eq 'moshu-reseller-acceptance-key-routing' -and ($Sites -join ',') -eq 'main,l1,cos') 'Long-connection suite is limited to all three sites in the named synthetic project'
  Check (-not ($ExtendedKeyRules -or $RemainingSafetyCasesOnly -or $SLACasesOnly -or $AccountPriorityFallbackOnly -or $SharedStateFaultOnly -or $CatalogueCasesOnly -or $CatalogueMetadataOnly)) 'Long-connection suite cannot be combined with another suite'
}
if($SharedStateFaultOnly) {
  Check ($Project -eq 'moshu-reseller-acceptance-key-routing' -and $Sites.Count -eq 1 -and $Sites[0] -eq 'main') 'Shared-state fault suite is limited to the named synthetic project and main site'
  Check (-not ($ExtendedKeyRules -or $RemainingSafetyCasesOnly -or $SLACasesOnly -or $AccountPriorityFallbackOnly -or $CatalogueCasesOnly)) 'Shared-state fault suite cannot be combined with another suite'
}
$run = [Guid]::NewGuid().ToString('N').Substring(0,12)
$prefix = "key-route-existing-$run"
$network = "${Project}_default"
$mockName = "$prefix-mock"
$peerName = "$prefix-peer"; $peerId = ''; $redisPauseAttempted = $false
$fixture = (Resolve-Path (Join-Path $PSScriptRoot '../acceptance/mock-failover-upstream.mjs')).Path
if (-not $OutputDirectory) { $OutputDirectory = "D:/projects/$prefix" }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
Check (-not (Test-Path -LiteralPath $OutputDirectory)) 'Report directory must be new'
$containers = @{}; $initial = @{}; $mockId = ''; $site = ''; $token = ''; $userToken = ''; $apiKey = ''; $userId = 0; $keyId = 0
$resources = @{}; $gatewayCalls = 0; $cleanupStandalone = $false; $longFixtures=@{}; $longProcess=$null; $mockRemovedEarly=$false
if ($RemainingSafetyCasesOnly) { $ExtendedKeyRules = $true }
$gatewayLimit=$(if($LongConnectionOnly){3}elseif($SharedStateFaultOnly){11}elseif($CatalogueMetadataOnly){0}elseif($CatalogueCasesOnly){2}elseif($SLACasesOnly){11}elseif($AccountPriorityFallbackOnly){4}elseif($RemainingSafetyCasesOnly){3}elseif($ExtendedKeyRules){16}else{10})*$(if($LongConnectionOnly){1}else{$Sites.Count})
$evidence = [ordered]@{status='running';run=$run;project=$Project;image=$ExpectedImageId;started_at=[DateTime]::UtcNow.ToString('o');sites=@{};findings=[Collections.Generic.List[object]]::new();cleanup=[Collections.Generic.List[object]]::new();fixture_sha256=(Get-FileHash -LiteralPath $fixture -Algorithm SHA256).Hash.ToLowerInvariant();limits=@{gateway_requests=$gatewayLimit;paid_models=$false;application_restarts=$false;data_service_changes=$false;mock_published_ports=$false}}
$httpClient = @'
const fs=require('node:fs'),p=JSON.parse(fs.readFileSync(0,'utf8'));
if((!['main','l1','cos','localhost'].includes(p.host)&&!(p.host===p.peer&&/^key-route-existing-[a-f0-9]{12}-peer$/.test(p.peer)))||!p.path.startsWith('/')||p.path.startsWith('//'))throw Error('Non-fixture target');
(async()=>{const h={};if(p.token)h.authorization='Bearer '+p.token;if(p.body!==null)h['content-type']='application/json';
if(p.etag)h['if-none-match']=p.etag;
const r=await fetch('http://'+(p.host==='localhost'?'127.0.0.1':p.host)+':8080'+p.path,{method:p.method,headers:h,body:p.body===null?undefined:JSON.stringify(p.body),redirect:'error',signal:AbortSignal.timeout(p.longConnection?800000:45000)});
process.stdout.write(JSON.stringify({status:r.status,content:await r.text(),etag:r.headers.get('etag')}));})().catch(e=>{process.stderr.write(e.message);process.exitCode=1});
'@
function Save { $evidence.gateway_requests=$script:gatewayCalls; $evidence | ConvertTo-Json -Depth 25 | Set-Content -LiteralPath (Join-Path $OutputDirectory 'evidence.json') -Encoding utf8 }
function Http([string]$Method,[string]$Path,[string]$Auth='',[object]$Body=$null,[string]$HostName=$script:site,[string]$ETag='') {
  Check ($HostName -in @('main','l1','cos','localhost') -or ($SharedStateFaultOnly -and $peerId -and $HostName -eq $peerName)) 'Non-fixture HTTP target'
  $payload=@{method=$Method;path=$Path;token=$Auth;body=$Body;host=$HostName;etag=$ETag;longConnection=[bool]$LongConnectionOnly;peer=$(if($SharedStateFaultOnly){$peerName}else{''})}|ConvertTo-Json -Depth 20 -Compress
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
  return @{Status=[int]$r.status;Content=[string]$r.content;Json=$data;ETag=[string]$r.etag}
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
function Redis-SlotSnapshot([string]$SiteName) {
  $f=$longFixtures[$SiteName];$redisId=$containers["$SiteName-redis"]
  $slots=@{account="concurrency:account:$($f.account_id)";user="concurrency:user:$($f.user_id)";api_key="concurrency:api_key:$($f.key_id)"};$counts=@{}
  foreach($kind in $slots.Keys){$counts[$kind]=[int]((Docker exec $redisId redis-cli --raw ZCARD $slots[$kind])-join '').Trim()}
  return @{site=$SiteName;container_id=$redisId;slot_keys=$slots;counts=$counts}
}
function Invoke-LongSSEClient {
  return @'
const fs=require('node:fs'),p=JSON.parse(fs.readFileSync(0,'utf8'));
if(p.targets.length!==3||p.targets.map(x=>x.host).join(',')!=='main,l1,cos'||p.targets.some(x=>!x.key))throw Error('Unexpected long-SSE fixture targets');
async function one(t){const started=Date.now(),signal=AbortSignal.timeout(800000);let chunks=0,bytes=0,lastChunk=null,maxChunkGap=0,heartbeats=0,lastBeat=null,maxBeatGap=0,firstDelta=null,completed=0,usage=null,buffer='',frame=[];
const result={host:t.host,status:0,elapsed_ms:0,chunks:0,bytes:0,heartbeats:0,max_chunk_gap_ms:0,max_heartbeat_gap_ms:0,first_delta_ms:null,completed_events:0,usage:null};
function consume(line){line=line.replace(/\r$/,'');if(line===''){if(!frame.length)return;const data=frame.filter(x=>x.startsWith('data:')).map(x=>x.slice(5).trimStart()).join('\n');for(const x of frame){if(x.startsWith(':')){const now=Date.now();if(lastBeat!==null)maxBeatGap=Math.max(maxBeatGap,now-lastBeat);lastBeat=now;heartbeats++;}}frame=[];if(data&&data!=='[DONE]'){try{const v=JSON.parse(data);if(v.type==='response.output_text.delta'&&v.delta&&firstDelta===null)firstDelta=Date.now()-started;if(v.type==='response.completed'){completed++;usage=v.response?.usage||v.usage||null;}}catch{}}return;}frame.push(line)}
try{const r=await fetch('http://'+t.host+':8080/v1/responses',{method:'POST',headers:{authorization:'Bearer '+t.key,'content-type':'application/json'},body:JSON.stringify({model:'gpt-4o-mini',input:'bounded synthetic long SSE '+t.host,stream:true}),redirect:'error',signal});result.status=r.status;if(r.status!==200)throw Error('http_status_'+r.status);if(!r.body)throw Error('missing_stream_body');const rd=r.body.getReader(),dec=new TextDecoder();for(;;){const x=await rd.read();if(x.done)break;const now=Date.now();chunks++;bytes+=x.value.byteLength;if(lastChunk!==null)maxChunkGap=Math.max(maxChunkGap,now-lastChunk);lastChunk=now;buffer+=dec.decode(x.value,{stream:true});let nl;while((nl=buffer.indexOf('\n'))>=0){consume(buffer.slice(0,nl));buffer=buffer.slice(nl+1);}}buffer+=dec.decode();if(buffer)consume(buffer);consume('');result.elapsed_ms=Date.now()-started;result.chunks=chunks;result.bytes=bytes;result.heartbeats=heartbeats;result.max_chunk_gap_ms=maxChunkGap;result.max_heartbeat_gap_ms=maxBeatGap;result.first_delta_ms=firstDelta;result.completed_events=completed;result.usage=usage;result.pass=result.elapsed_ms>=650000&&result.elapsed_ms<=750000&&chunks>=30&&heartbeats>=30&&maxChunkGap<=25000&&maxBeatGap<=25000&&firstDelta!==null&&firstDelta<=10000&&completed===1&&usage?.input_tokens===10&&usage?.output_tokens===2;if(!result.pass)result.error='stream_assertion_failed';}
catch(e){result.elapsed_ms=Date.now()-started;result.chunks=chunks;result.bytes=bytes;result.heartbeats=heartbeats;result.max_chunk_gap_ms=maxChunkGap;result.max_heartbeat_gap_ms=maxBeatGap;result.first_delta_ms=firstDelta;result.completed_events=completed;result.usage=usage;result.pass=false;result.error=e.name==='TimeoutError'?'client_timeout_800s':String(e.message).slice(0,100);}return result;}
(async()=>{const results=await Promise.all(p.targets.map(one));process.stdout.write(JSON.stringify(results));if(results.some(x=>!x.pass))process.exitCode=2;})().catch(e=>{process.stderr.write(String(e.message).slice(0,120));process.exitCode=1});
'@
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
function Rotate-SLAPrimary([string]$Scenario) {
  # Isolate group-level SLA from native per-account transient-error cooldowns.
  # Keep Key/group/model/endpoint unchanged; never clear native runtime state.
  $old=(Api POST "/admin/accounts/$($accountIds[0])/schedulable" $token @{schedulable=$false}).Json.data
  Check ($old.schedulable -eq $false) 'Old SLA fixture account was not disabled'
  $next=[long](Api POST '/admin/accounts' $token @{name="$prefix-$site-sla-$($resources[$site].accounts.Count)";platform='openai';type='apikey';concurrency=5;priority=0;rate_multiplier=1;group_ids=@($primaryGroup);extra=@{openai_passthrough=$false;openai_responses_mode='force_responses'};credentials=(Credentials $Scenario 'primary')}).Json.data.id
  Check ($next -gt 0) 'Fresh SLA fixture account was not created'
  $resources[$site].accounts.Add($next);$script:accountIds[0]=$next;Save
  Start-Sleep -Seconds 3
}
function Gateway([string]$Label,[string]$ExpectedSlot,[long]$ExpectedGroup,[decimal]$Rate,[bool]$Stream=$false,[string]$Mode='success',[string]$Model='gpt-4o-mini',[long]$ExpectedAccountId=0) {
  Check ($script:gatewayCalls -lt $gatewayLimit) 'Fixed gateway request budget exhausted'
  $before=Ledger; $statsBefore=Stats; $script:gatewayCalls++; Save
  $clock=[Diagnostics.Stopwatch]::StartNew()
  $r=Http POST '/v1/responses' $apiKey @{model=$Model;input="synthetic $prefix $Label";stream=$Stream}
  $clock.Stop()
  $statsAfter=Stats; $calls=@($statsAfter|Select-Object -Skip $statsBefore.Count)
  $phase=@{label=$Label;model=$Model;status=$r.Status;upstream=$calls;elapsed_ms=$clock.ElapsedMilliseconds}; $evidence.sites[$site].last_attempt=$phase; Save
  if ($Mode -eq 'success') {
    Check ($r.Status -eq 200 -and $r.Content.Contains('backup-ok')) "$site $Label did not complete successfully"
    $selected=@($calls|Where-Object {$_.slot -eq $ExpectedSlot})
    Check ($selected.Count -eq 1 -and $selected[0].stream -eq $Stream) "$site $Label selected/replayed wrong target"
    Check (@($calls|Where-Object {$_.slot -ne $ExpectedSlot -and $_.slot -ne 'primary'}).Count -eq 0) "$site $Label contacted an unexpected backup"
    $deadline=[DateTime]::UtcNow.AddSeconds(20)
    do { $after=Ledger; if($after.usage_count -gt $before.usage_count){break};Start-Sleep -Milliseconds 250 } while([DateTime]::UtcNow -lt $deadline)
    $u=$after.last_usage
    Check ($after.usage_count -eq $before.usage_count+1 -and $after.dedup_count -eq $before.dedup_count+1) "$site $Label did not settle exactly once"
    $expectedAccount=if($ExpectedAccountId -gt 0){$ExpectedAccountId}else{$accountIds[$slots.IndexOf($ExpectedSlot)]}
    Check ($u.group_id -eq $ExpectedGroup -and $u.account_id -eq $expectedAccount -and $u.input_tokens -eq 10 -and $u.output_tokens -eq 2) "$site $Label usage identity/tokens differ"
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
    if($Mode -eq 'failure'){Check ($r.Status -eq 502 -and $calls.Count -gt 0) "$site $Label did not preserve the expected upstream failure"}
    Check ($after.balance -eq $before.balance -and $after.quota_used -eq $before.quota_used) "$site $Label unexpectedly charged failed fixture"
    $phase.debit=0
  }
  $evidence.sites[$site].phases.Add($phase); Save
}

# A small bounded batch is separate from Gateway: concurrent requests must use
# one batch-level ledger snapshot rather than racing per-request deltas.
function Shared-Batch([string]$Label,[string[]]$Hosts,[long]$ExpectedAccount,[string]$ExpectedSlot,[bool]$Fault=$false) {
  Check ($SharedStateFaultOnly -and $Hosts.Count -ge 1 -and $Hosts.Count -le 2) 'Unexpected shared-state batch size'
  Check ($gatewayCalls+$Hosts.Count -le $gatewayLimit) 'Fixed shared-state request budget exhausted'
  foreach($hostTarget in $Hosts){Check ($hostTarget -eq 'main' -or $hostTarget -eq $peerName) 'Unexpected shared-state target'}
  $before=Ledger;$statsBefore=Stats;$script:gatewayCalls += $Hosts.Count;Save
  $batchClient=@'
const fs=require('node:fs'),p=JSON.parse(fs.readFileSync(0,'utf8'));
if(!/^key-route-existing-[a-f0-9]{12}-peer$/.test(p.peer)||p.hosts.length<1||p.hosts.length>2||p.hosts.some(h=>h!=='main'&&h!==p.peer))throw Error('Non-fixture batch target');
(async()=>{const results=await Promise.all(p.hosts.map(async(host,index)=>{const started=Date.now();try{
const r=await fetch('http://'+host+':8080/v1/responses',{method:'POST',headers:{authorization:'Bearer '+p.token,'content-type':'application/json'},body:JSON.stringify({model:'gpt-4o-mini',input:'synthetic '+p.label+' '+index,stream:false}),redirect:'error',signal:AbortSignal.timeout(30000)});
return {host,status:r.status,content:await r.text(),request_id:r.headers.get('x-request-id'),started_ms:started,finished_ms:Date.now(),elapsed_ms:Date.now()-started};
}catch(e){return {host,status:0,error:e.name,started_ms:started,finished_ms:Date.now(),elapsed_ms:Date.now()-started}}}));process.stdout.write(JSON.stringify(results));})().catch(e=>{process.stderr.write(e.message);process.exitCode=1});
'@
  $payload=@{hosts=$Hosts;peer=$peerName;token=$apiKey;label="$prefix-$Label"}|ConvertTo-Json -Compress
  $raw=$payload | & docker.exe exec -i $mockId node -e $batchClient
  Check ($LASTEXITCODE -eq 0) 'Bounded synthetic batch transport failed'
  $responses=($raw -join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate
  Check ($responses.Count -eq $Hosts.Count) 'Synthetic batch response count differs'
  foreach($response in $responses){
    Check ($response.elapsed_ms -lt 35000) 'Synthetic batch exceeded its 30-second client timeout bound'
    if(-not $Fault){Check ($response.status -eq 200 -and $response.content.Contains('backup-ok')) "Shared-state $Label did not succeed"}
  }
  $overlap=$true
  if($Hosts.Count -gt 1){$overlap=($responses.finished_ms|Measure-Object -Minimum).Minimum -gt ($responses.started_ms|Measure-Object -Maximum).Maximum;Check $overlap 'Synthetic batch did not overlap'}
  $phase=@{label=$Label;responses=$responses;before=$before;stats_before=$statsBefore.Count;upstream=@((Stats)|Select-Object -Skip $statsBefore.Count);overlapped=$overlap;fault=$Fault;expected_account=$ExpectedAccount;expected_slot=$ExpectedSlot}
  $evidence.sites[$site].phases.Add($phase);Save
  if(-not $Fault){Assert-SharedSettlement $phase $Hosts.Count}
  Save
  return $phase
}
function Assert-SharedSettlement([hashtable]$Phase,[int]$ExpectedCount) {
  $before=$Phase.before;$deadline=[DateTime]::UtcNow.AddSeconds(20)
  do {$after=Ledger;if($after.usage_count -ge $before.usage_count+$ExpectedCount){break};Start-Sleep -Milliseconds 250}while([DateTime]::UtcNow -lt $deadline)
  Check ($after.usage_count-$before.usage_count -eq $ExpectedCount -and $after.dedup_count-$before.dedup_count -eq $ExpectedCount) "Shared-state $($Phase.label) did not settle exactly once per completed synthetic request"
  $lastId=if($before.last_usage){[long]$before.last_usage.id}else{0L}
  $rows=(Sql "SELECT COALESCE(json_agg(x),'[]'::json) FROM (SELECT id,group_id,account_id,input_tokens,output_tokens,total_cost,actual_cost,rate_multiplier FROM usage_logs WHERE api_key_id=$keyId AND id>$lastId ORDER BY id)x;")|ConvertFrom-Json -AsHashtable -NoEnumerate
  Check ($rows.Count -eq $ExpectedCount) 'Shared-state settlement row count differs'
  $charged=[decimal]0
  foreach($row in $rows){
    Check ($row.group_id -eq $primaryGroup -and $row.input_tokens -eq 10 -and $row.output_tokens -eq 2 -and [decimal]$row.rate_multiplier -eq 2) 'Shared-state actual group/tokens/rate differs'
    if($Phase.expected_account -gt 0){Check ($row.account_id -eq $Phase.expected_account) 'Shared-state settlement used an unexpected account'}
    $cost=[Math]::Round(([decimal]$row.total_cost*2),8,[MidpointRounding]::AwayFromZero)
    Check ([Math]::Abs([decimal]$row.actual_cost-$cost) -le [decimal]'0.00000001') 'Shared-state row cost differs'
    $charged += $cost
  }
  Check ([decimal]$before.balance-[decimal]$after.balance -eq $charged -and [decimal]$after.quota_used-[decimal]$before.quota_used -eq $charged) 'Shared-state batch balance/quota debit differs'
  $Phase.after=$after;$Phase.usage=$rows;$Phase.charged=$charged
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
  if($SharedStateFaultOnly) {
    $compose=(Resolve-Path (Join-Path $PSScriptRoot '../acceptance/docker-compose.isolation.yml')).Path
    $savedImage=$env:SUB2API_IMAGE
    try {
      $env:SUB2API_IMAGE=$ExpectedImageId
      $peerId=(Docker compose -p $Project -f $compose run --detach --no-deps --pull never --name $peerName --label "moshu.acceptance.run=$run" main).Trim()
    } finally {
      if($null -eq $savedImage){Remove-Item Env:SUB2API_IMAGE -ErrorAction SilentlyContinue}else{$env:SUB2API_IMAGE=$savedImage}
    }
    $peer=(((Docker inspect $peerId)-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
    Check ($peer.Config.Labels['moshu.acceptance.run'] -eq $run -and $peer.Config.Labels['com.docker.compose.project'] -eq $Project -and $peer.Image -eq $ExpectedImageId) 'Unexpected private peer ownership/image'
    Check ($peer.HostConfig.PortBindings.Count -eq 0 -and $peer.Mounts.Count -eq 0 -and $peer.NetworkSettings.Networks.Count -eq 1 -and $peer.NetworkSettings.Networks.ContainsKey($network)) 'Private peer published ports, mounted data, or joined an unexpected network'
    Check ($peer.Config.Env.Contains('DATABASE_HOST=main-db') -and $peer.Config.Env.Contains('REDIS_HOST=main-redis') -and $peer.Config.Env.Contains('DATABASE_PASSWORD=acceptance-only')) 'Private peer does not share the synthetic main data services'
    $deadline=[DateTime]::UtcNow.AddSeconds(60)
    do {
      $peer=(((Docker inspect $peerId)-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
      if($peer.State.Health.Status -eq 'healthy'){break}
      Check ($peer.State.Running) 'Private peer exited before becoming healthy'
      Start-Sleep -Seconds 1
    } while([DateTime]::UtcNow -lt $deadline)
    Check ($peer.State.Health.Status -eq 'healthy' -and (Http GET '/health' '' $null $peerName).Status -eq 200) 'Private peer did not become healthy'
    $evidence.shared_peer=@{id=$peerId;name=$peerName;image=$peer.Image;ports=0;mounts=0;database='main-db';redis='main-redis';health='healthy'};Save
  }
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
      $fixturePlatform=if($CatalogueCasesOnly){switch($slot){'backup'{'deepseek'}'third'{'kimi'}default{'openai'}}}else{'openai'}
      $g=[long](Api POST '/admin/groups' $token @{name="$prefix-$site-$slot";platform=$fixturePlatform;subscription_type='standard';rate_multiplier=$rate}).Json.data.id
      $groupIds+=$g;$resources[$site].groups.Add($g);Save
      if($CatalogueCasesOnly) {
        # Unique discovered IDs must also be callable. Use explicit synthetic
        # per-token prices; never rely on unknown-model fallback pricing.
        $null=Api PUT "/admin/groups/$g" $token @{model_pricing=@(@{platform=$fixturePlatform;models=@("$slot-model");billing_mode='token';input_price=0.000001;output_price=0.000002})}
      }
      $a=[long](Api POST '/admin/accounts' $token @{name="$prefix-$site-$slot";platform=$fixturePlatform;type='apikey';concurrency=5;priority=0;rate_multiplier=1;group_ids=@($g);extra=@{openai_passthrough=$false;openai_responses_mode='force_responses'};credentials=(Credentials 'success' $slot ($slot -ne 'primary'))}).Json.data.id
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
    if($LongConnectionOnly) {
      $null=Api PUT "/admin/accounts/$($accountIds[0])" $token @{credentials=(Credentials 'long_connection' 'primary')}
      Start-Sleep -Seconds 3
      $longFixtures[$site]=@{host=$site;api_key=$apiKey;key_id=$keyId;user_id=$userId;account_id=$accountIds[0];group_id=$primaryGroup;before=Ledger}
      $beforeSnapshot=Redis-SlotSnapshot $site
      Check (@($beforeSnapshot.counts.Values|Where-Object {$_ -ne 0}).Count -eq 0) "$site exact Redis request slots were not empty before SSE"
      $evidence.sites[$site].suite='long-connection-only';$evidence.sites[$site].redis_before=$beforeSnapshot
      $evidence.sites[$site].ledger_before=$longFixtures[$site].before;Save
      continue
    } elseif($SharedStateFaultOnly) {
      $evidence.sites[$site].suite='shared-state-fault-only'
      $null=Api POST "/admin/accounts/$($accountIds[0])/schedulable" $token @{schedulable=$false}
      $accountA=[long](Api POST '/admin/accounts' $token @{name="$prefix-$site-shared-a";platform='openai';type='apikey';concurrency=5;priority=0;rate_multiplier=1;group_ids=@($primaryGroup);extra=@{openai_passthrough=$false;openai_responses_mode='force_responses'};credentials=(Credentials 'server' 'primary')}).Json.data.id
      $resources[$site].accounts.Add($accountA);Save
      $accountB=[long](Api POST '/admin/accounts' $token @{name="$prefix-$site-shared-b";platform='openai';type='apikey';concurrency=5;priority=1;rate_multiplier=1;group_ids=@($primaryGroup);extra=@{openai_passthrough=$false;openai_responses_mode='force_responses'};credentials=(Credentials 'success' 'primary-b')}).Json.data.id
      $resources[$site].accounts.Add($accountB);Save
      $sharedPolicy=@{rules=@(@{type='status_code';status_codes=@(502);threshold=3;window_seconds=60;cooldown_seconds=120})}
      $created=(Api POST '/keys' $userToken @{name="$prefix-$site-shared-health";group_id=$primaryGroup;backup_group_ids=@($backupGroup,$thirdGroup);key_route_scope='account_v2';quota=1;key_route_failure_policy=$sharedPolicy}).Json.data
      $keyId=[long]$created.id;$apiKey=[string]$created.key;$resources[$site].keys.Add($keyId);Save
      Check ($accountA -gt 0 -and $accountB -gt 0 -and $keyId -gt 0 -and [bool]$apiKey) 'Shared-state resources were not created'
      Start-Sleep -Seconds 3
      $seed=Shared-Batch 'shared-threshold-first' @('main') $accountB 'primary-b'
      Check (@($seed.upstream|Where-Object {$_.slot -eq 'primary' -and $_.scenario -eq 'server'}).Count -eq 1) 'Native retry behavior did not produce exactly one seed failure; do not claim a three-attempt threshold test'
      $threshold=Shared-Batch 'shared-threshold-concurrent' @('main',$peerName) $accountB 'primary-b'
      Check (@($threshold.upstream|Where-Object {$_.slot -eq 'primary' -and $_.scenario -eq 'server'}).Count -eq 2) 'Concurrent requests did not contribute exactly two additional account failures'
      Check (@($threshold.upstream|Where-Object {$_.slot -ne 'primary' -and $_.slot -ne 'primary-b'}).Count -eq 0) 'Shared threshold left the healthy primary group'
      $cooled=Shared-Batch 'shared-cooldown-both-apps' @('main',$peerName) $accountB 'primary-b'
      Check ($cooled.upstream.Count -eq 2 -and @($cooled.upstream|Where-Object {$_.slot -ne 'primary-b'}).Count -eq 0) 'Both full applications did not observe the shared account cooldown'
      $firstKey=$keyId;$firstSecret=$apiKey
      $second=(Api POST '/keys' $userToken @{name="$prefix-$site-independent-health";group_id=$primaryGroup;backup_group_ids=@($backupGroup,$thirdGroup);key_route_scope='account_v2';quota=1;key_route_failure_policy=$sharedPolicy}).Json.data
      $keyId=[long]$second.id;$apiKey=[string]$second.key;$resources[$site].keys.Add($keyId);Save
      Check ($keyId -gt 0 -and $keyId -ne $firstKey -and [bool]$apiKey) 'Independent shared-account Key missing'
      Start-Sleep -Seconds 3
      $isolated=Shared-Batch 'shared-second-key-isolated' @('main',$peerName) $accountB 'primary-b'
      # Native scheduler choices need not select A for every overlapping request.
      # At least one real A attempt proves Key 2 did not inherit Key 1's cooldown.
      $isolatedAAttempts=@($isolated.upstream|Where-Object {$_.slot -eq 'primary' -and $_.scenario -eq 'server'}).Count
      Check ($isolatedAAttempts -ge 1 -and $isolatedAAttempts -le 2 -and @($isolated.upstream|Where-Object {$_.slot -eq 'primary-b' -and $_.scenario -eq 'success'}).Count -eq 2) 'Second Key did not demonstrate independent access to A with successful same-group completion'
      $keyId=$firstKey;$apiKey=$firstSecret

      $redisId=$containers['main-redis']
      $redis=(((Docker inspect $redisId)-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
      Check ($redis.Id -eq $redisId -and $redis.Config.Labels['com.docker.compose.project'] -eq $Project -and $redis.Config.Labels['com.docker.compose.service'] -eq 'main-redis' -and -not $redis.State.Paused) 'Refusing to pause an unexpected or already-paused Redis'
      $evidence.limits.data_service_changes=$true;$evidence.limits.data_service_pause_only=$true
      $redisPauseAttempted=$true
      try {
        $null=Docker pause $redisId
        $fault=Shared-Batch 'whole-redis-paused-bounded' @('main',$peerName) 0 '' $true
      } finally {
        $redis=(((Docker inspect $redisId)-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
        Check ($redis.Id -eq $redisId) 'Recorded Redis identity changed before unpause'
        if($redis.State.Paused){$null=Docker unpause $redisId}
      }
      Check ((Docker exec $redisId redis-cli PING).Trim() -eq 'PONG') 'Redis did not recover after unpause'
      Check ((Http GET '/health').Status -eq 200 -and (Http GET '/health' '' $null $peerName).Status -eq 200) 'Applications did not recover after unpause'
      Start-Sleep -Seconds 2
      $fault.upstream=@((Stats)|Select-Object -Skip $fault.stats_before)
      $faultSuccess=@($fault.upstream|Where-Object {$_.slot -eq 'primary-b' -and $_.scenario -eq 'success'}).Count
      $faultErrors=@($fault.upstream|Where-Object {$_.slot -eq 'primary' -and $_.scenario -eq 'server'}).Count
      Check ($faultSuccess -le 2 -and $faultErrors -le 2 -and $faultSuccess+$faultErrors -eq $fault.upstream.Count) 'Paused Redis caused duplicate or unexpected dispatches'
      Assert-SharedSettlement $fault $faultSuccess
      $evidence.sites[$site].redis_fault=@{container_id=$redisId;pause_only=$true;client_timeout_ms=30000;results=$fault.responses;completed_upstream_requests=$faultSuccess;settlements=$fault.usage.Count;restored_ping='PONG';scope='Whole shared Redis was paused, including native auth/limits/slots; bounded failures are allowed. This does not isolate only the custom health store.'};Save
      $keyId=$firstKey;$apiKey=$firstSecret
      $recovered=Shared-Batch 'redis-recovered-shared-cooldown' @('main',$peerName) $accountB 'primary-b'
      Check ($recovered.upstream.Count -eq 2 -and @($recovered.upstream|Where-Object {$_.slot -ne 'primary-b'}).Count -eq 0) 'Shared cooldown or healthy in-group service did not survive Redis pause/recovery'
      $evidence.sites[$site].shared_state=@{full_application_instances=2;max_batch_concurrency=2;threshold=3;window_seconds=60;cooldown_seconds=120;observed_primary_failures=3;both_apps_observed_cooldown=$true;key_isolation=$true;independent_key_a_attempts=$isolatedAAttempts;redis_pause_recovery=$true;exactly_once_synthetic_settlement=$true;strict_simultaneous_cas_barrier=$false};Save
    } elseif($CatalogueCasesOnly) {
      $evidence.sites[$site].suite=if($CatalogueMetadataOnly){'catalogue-metadata-only'}else{'catalogue-only'}
      Check ($backupModelsVisible -and @($models.Json.data.id|Where-Object {$_ -eq 'gpt-4o-mini'}).Count -eq 1) 'Catalogue did not merge and deduplicate the authorized groups'
      Check ([bool]$models.ETag) 'Merged catalogue has no final ETag'
      $beforeMetadata=Ledger;$beforeMetadataStats=Stats
      $cached=Http GET '/v1/models' $apiKey $null $site $models.ETag
      Check ($cached.Status -eq 304 -and -not $cached.Content) 'Final catalogue ETag was not respected'
      $detail=Http GET '/v1/models/backup-model' $apiKey
      Check ($detail.Status -eq 200 -and $detail.Json.id -eq 'backup-model') 'Backup model detail is missing'
      $null=Api PUT "/admin/groups/$backupGroup" $token @{status='inactive'}
      $inactive=Http GET '/v1/models' $apiKey $null $site $models.ETag
      Check ($inactive.Status -eq 200 -and $inactive.ETag -ne $models.ETag -and -not @($inactive.Json.data.id).Contains('backup-model') -and @($inactive.Json.data.id).Contains('third-model')) 'Disabled backup leaked through the old catalogue ETag'
      $null=Api PUT "/admin/groups/$backupGroup" $token @{status='active';is_exclusive=$true}
      $unauthorized=Http GET '/v1/models' $apiKey
      Check ($unauthorized.Status -eq 200 -and -not @($unauthorized.Json.data.id).Contains('backup-model') -and -not @($unauthorized.Json.data.id).Contains('third-model') -and @($unauthorized.Json.data.id).Contains('unrelated-model')) 'Catalogue advertised a later group beyond the native authorization barrier'
      $null=Api PUT "/admin/groups/$backupGroup" $token @{is_exclusive=$false}
      $null=Api POST "/admin/accounts/$($accountIds[1])/schedulable" $token @{schedulable=$false}
      $emptyPool=Http GET '/v1/models' $apiKey
      Check ($emptyPool.Status -eq 200 -and -not @($emptyPool.Json.data.id).Contains('backup-model') -and -not ($emptyPool.Content -match 'claude-')) 'Empty backup pool invented a default catalogue'
      $null=Api POST "/admin/accounts/$($accountIds[1])/schedulable" $token @{schedulable=$true}
      $null=Api PUT "/keys/$keyId" $userToken @{key_model_policy=@{enabled=$true;models=@('backup-model','third-model')}}
      $restricted=Http GET '/v1/models' $apiKey
      Check ($restricted.Status -eq 200 -and @($restricted.Json.data).Count -eq 2 -and @($restricted.Json.data.id).Contains('backup-model') -and @($restricted.Json.data.id).Contains('third-model')) 'Key model policy did not narrow the merged catalogue'
      $hidden=Http GET '/v1/models/gpt-4o-mini' $apiKey
      Check ($hidden.Status -eq 404) 'Restricted model detail leaked'
      $null=Api PUT "/keys/$keyId" $userToken @{key_model_policy=$null}
      $afterMetadata=Ledger;$afterMetadataStats=Stats
      Check ($afterMetadata.balance -eq $beforeMetadata.balance -and $afterMetadata.quota_used -eq $beforeMetadata.quota_used -and $afterMetadata.usage_count -eq $beforeMetadata.usage_count -and $afterMetadataStats.Count -eq $beforeMetadataStats.Count) 'Model discovery invoked a model or changed billing'
      if(-not $CatalogueMetadataOnly) {
        Start-Sleep -Seconds 3
        Gateway 'catalogue-backup-model-http' 'backup' $backupGroup 0.5 $false 'success' 'backup-model'
        Gateway 'catalogue-third-model-sse' 'third' $thirdGroup 0.75 $true 'success' 'third-model'
      }
      $evidence.sites[$site].catalogue=@{union=$true;deduplicated=$true;final_etag=$true;backup_detail=$true;disabled_hidden=$true;unauthorized_hidden=$true;authorization_barrier=$true;empty_pool_no_defaults=$true;key_allowlist=$true;metadata_zero_debit=$true;deepseek_http=(-not $CatalogueMetadataOnly);kimi_sse=(-not $CatalogueMetadataOnly)}
      Save
    } elseif($SLACasesOnly) {
      # No new application code or image: exercise SLA through the real gateway.
      # One group result counts once, even when its native account retries.
      $evidence.sites[$site].suite='sla-only'
      $sla=@{rules=@(@{type='sla_success_rate';threshold=50;sample_size=2;cooldown_seconds=120})}
      $null=Api PUT "/keys/$keyId" $userToken @{key_route_failure_policy=$sla}
      $saved=(Api GET "/keys/$keyId" $userToken).Json.data
      Check ($saved.key_route_failure_policy.rules[0].type -eq 'sla_success_rate' -and $saved.key_route_failure_policy.rules[0].threshold -eq 50 -and $saved.key_route_failure_policy.rules[0].sample_size -eq 2) 'SLA rule did not persist'
      $startLedger=Ledger
      Rotate-SLAPrimary 'server'
      Gateway 'sla-incomplete-window-no-fallback' '' 0 0 $false 'failure'
      Rotate-SLAPrimary 'success'
      Gateway 'sla-equal-threshold-success-stays-primary' 'primary' $primaryGroup 2
      Gateway 'sla-equal-threshold-next-still-primary' 'primary' $primaryGroup 2
      Rotate-SLAPrimary 'server'
      Gateway 'sla-equal-threshold-failure-no-fallback' '' 0 0 $false 'failure'
      Rotate-SLAPrimary 'server'
      Gateway 'sla-below-threshold-failure-switch' 'backup' $backupGroup 0.5
      Check (@($evidence.sites[$site].last_attempt.upstream|Where-Object {$_.slot -eq 'primary'}).Count -gt 0) 'SLA threshold failure did not actually traverse the primary'
      Rotate-SLAPrimary 'success'
      Gateway 'sla-cooling-next-skips-primary' 'backup' $backupGroup 0.5
      Check (@($evidence.sites[$site].last_attempt.upstream|Where-Object {$_.slot -eq 'primary'}).Count -eq 0) 'SLA cooldown retried primary instead of skipping it'
      $null=Api PUT "/keys/$keyId" $userToken @{key_route_failure_policy=@{rules=@()}}
      Check ((Api GET "/keys/$keyId" $userToken).Json.data.key_route_failure_policy.rules.Count -eq 0) 'SLA clearing did not persist'
      Gateway 'sla-remove-rules-restores-primary' 'primary' $primaryGroup 2

      # Success can complete a bad window, but must only affect the NEXT request.
      $sla.rules[0].threshold=75;$sla.rules[0].sample_size=3
      $null=Api PUT "/keys/$keyId" $userToken @{key_route_failure_policy=$sla}
      Rotate-SLAPrimary 'server'
      Gateway 'sla-success-trigger-incomplete-one' '' 0 0 $false 'failure'
      Rotate-SLAPrimary 'server'
      Gateway 'sla-success-trigger-incomplete-two' '' 0 0 $false 'failure'
      Rotate-SLAPrimary 'success'
      Gateway 'sla-success-triggers-next-only' 'primary' $primaryGroup 2
      Gateway 'sla-success-trigger-next-skips-primary' 'backup' $backupGroup 0.5
      Check (@($evidence.sites[$site].last_attempt.upstream|Where-Object {$_.slot -eq 'primary'}).Count -eq 0) 'Successful SLA sample did not cool the primary for the next request'
      $finalLedger=Ledger
      $charged=[decimal]0
      foreach($phase in $evidence.sites[$site].phases){if($phase.ContainsKey('charged')){$charged += [decimal]$phase.charged}}
      Check ($finalLedger.usage_count - $startLedger.usage_count -eq 7 -and $finalLedger.dedup_count - $startLedger.dedup_count -eq 7) 'SLA matrix did not produce exactly seven successful settlements'
      Check ([decimal]$startLedger.balance-[decimal]$finalLedger.balance -eq $charged -and [decimal]$finalLedger.quota_used-[decimal]$startLedger.quota_used -eq $charged) 'SLA cumulative ledger differs from phase settlements'
      $evidence.sites[$site].sla=@{complete_window=$true;equality_does_not_cool=$true;failure_switch=$true;cooldown_skips_primary=$true;rule_removal_restores_primary=$true;success_not_replayed=$true;success_cools_next_request=$true;success_settlements=7;charged=$charged}
      Save
    } elseif($AccountPriorityFallbackOnly) {
      $evidence.sites[$site].suite='account-priority-fallback-only'
      $originalPrimaryId=[long]$accountIds[0]
      $originalPrimarySchedulable=[bool](Api GET "/admin/accounts/$originalPrimaryId" $token).Json.data.schedulable
      $disabledOriginal=(Api POST "/admin/accounts/$originalPrimaryId/schedulable" $token @{schedulable=$false}).Json.data
      Check (-not $disabledOriginal.schedulable) 'Could not isolate the new A/B accounts from the baseline primary account'
      Start-Sleep -Seconds 3
      try {
      $accountA=[long](Api POST '/admin/accounts' $token @{name="$prefix-$site-priority-a";platform='openai';type='apikey';concurrency=5;priority=0;rate_multiplier=1;group_ids=@($primaryGroup);extra=@{openai_passthrough=$false;openai_responses_mode='force_responses'};credentials=(Credentials 'success' 'primary')}).Json.data.id
      $resources[$site].accounts.Add($accountA);Save
      $accountB=[long](Api POST '/admin/accounts' $token @{name="$prefix-$site-priority-b";platform='openai';type='apikey';concurrency=5;priority=1;rate_multiplier=1;group_ids=@($primaryGroup);extra=@{openai_passthrough=$false;openai_responses_mode='force_responses'};credentials=(Credentials 'success' 'primary-b')}).Json.data.id
      $resources[$site].accounts.Add($accountB);Save
      Check ($accountA -gt 0 -and $accountB -gt 0 -and $accountA -ne $accountB) 'Priority sequence A/B accounts were not created'
      $priorityKey=(Api POST '/keys' $userToken @{name="$prefix-$site-priority-sequence";group_id=$primaryGroup;backup_group_ids=@($backupGroup,$thirdGroup);key_route_scope='account_v2';quota=1;key_route_failure_policy=@{rules=@()}}).Json.data
      $keyId=[long]$priorityKey.id;$apiKey=[string]$priorityKey.key;$resources[$site].keys.Add($keyId);Save
      Check ($keyId -gt 0 -and [bool]$apiKey) 'Priority sequence Key was not created'
      $aState=(Api GET "/admin/accounts/$accountA" $token).Json.data
      $bState=(Api GET "/admin/accounts/$accountB" $token).Json.data
      Check ($aState.schedulable -and $bState.schedulable) 'Both group 1 accounts must be schedulable before the sequence starts'
      $sequenceStart=Ledger

      Gateway 'priority-sequence-initial-a' 'primary' $primaryGroup 2 $false 'success' 'gpt-4o-mini' $accountA
      $firstCalls=@($evidence.sites[$site].last_attempt.upstream)
      Check ($firstCalls.Count -eq 1 -and $firstCalls[0].slot -eq 'primary') 'First request did not select account A'
      Gateway 'priority-sequence-repeat-a' 'primary' $primaryGroup 2 $false 'success' 'gpt-4o-mini' $accountA
      $repeatCalls=@($evidence.sites[$site].last_attempt.upstream)
      Check ($repeatCalls.Count -eq 1 -and $repeatCalls[0].slot -eq 'primary') 'Repeated request did not remain on account A while it was healthy'

      $disabledA=(Api POST "/admin/accounts/$accountA/schedulable" $token @{schedulable=$false}).Json.data
      Check (-not $disabledA.schedulable) 'Account A did not become unschedulable for fault injection'
      Start-Sleep -Seconds 3
      Gateway 'priority-sequence-disabled-a-uses-b' 'primary-b' $primaryGroup 2 $false 'success' 'gpt-4o-mini' $accountB
      $bCalls=@($evidence.sites[$site].last_attempt.upstream)
      Check ($bCalls.Count -eq 1 -and $bCalls[0].slot -eq 'primary-b' -and $bCalls[0].scenario -eq 'success') 'With A disabled, the request did not use B in group 1'

      $disabledB=(Api POST "/admin/accounts/$accountB/schedulable" $token @{schedulable=$false}).Json.data
      Check (-not $disabledB.schedulable) 'Account B did not become unschedulable for fault injection'
      Start-Sleep -Seconds 3
      Gateway 'priority-sequence-disabled-a-b-uses-group-2' 'backup' $backupGroup 0.5 $false 'success' 'gpt-4o-mini' $accountIds[1]
      $group2Calls=@($evidence.sites[$site].last_attempt.upstream)
      Check ($group2Calls.Count -eq 1 -and $group2Calls[0].slot -eq 'backup' -and $group2Calls[0].scenario -eq 'success') 'With A and B disabled, the request did not use priority group 2'

      $sequenceEnd=Ledger
      $sequencePhases=@($evidence.sites[$site].phases)
      $sequenceCharge=[decimal]0
      foreach($phase in $sequencePhases){if($phase.ContainsKey('charged')){$sequenceCharge += [decimal]$phase.charged}}
      $group1Settlements=@($sequencePhases|Where-Object {$_.usage.group_id -eq $primaryGroup})
      $group2Settlements=@($sequencePhases|Where-Object {$_.usage.group_id -eq $backupGroup})
      Check ($sequencePhases.Count -eq 4 -and $group1Settlements.Count -eq 3 -and $group2Settlements.Count -eq 1) 'Sequence did not settle three group 1 requests and one group 2 request'
      Check ($sequenceEnd.usage_count - $sequenceStart.usage_count -eq 4 -and $sequenceEnd.dedup_count - $sequenceStart.dedup_count -eq 4) 'Priority sequence did not create exactly four unique settlements'
      Check ([decimal]$sequenceStart.balance-[decimal]$sequenceEnd.balance -eq $sequenceCharge -and [decimal]$sequenceEnd.quota_used-[decimal]$sequenceStart.quota_used -eq $sequenceCharge) 'Priority sequence cumulative balance/quota debit differs from its four settlements'
      $evidence.sites[$site].account_priority_sequence=@{initial_account_a=$accountA;same_group_account_b=$accountB;first_two_requests_stayed_on_a=$true;disabled_a_routed_to_b=$true;disabled_a_and_b_routed_to_group_2=$true;group_1_settlements=$group1Settlements.Count;group_2_settlements=$group2Settlements.Count;exactly_once_settlement=$true;charged=$sequenceCharge;start_balance=$sequenceStart.balance;end_balance=$sequenceEnd.balance}
      Save
      } finally {
        $restoredPrimary=(Api POST "/admin/accounts/$originalPrimaryId/schedulable" $token @{schedulable=$originalPrimarySchedulable}).Json.data
        Check ($restoredPrimary.schedulable -eq $originalPrimarySchedulable) 'Baseline primary account schedulability was not restored'
        Start-Sleep -Seconds 3
      }
    } else {
    if(-not $RemainingSafetyCasesOnly) {
    Start-Sleep -Seconds 3
    # Use a fresh dedicated Key and fresh A/B accounts so earlier synthetic
    # failures cannot pre-cool the fixture. Disable the original primary while
    # these accounts are active, and always restore the original Key variables
    # and account schedulability even if an assertion fails.
    $originalKeyId=$keyId;$originalAPIKey=$apiKey
    $originalPrimaryId=[long]$accountIds[0]
    $originalPrimary=[bool](Api GET "/admin/accounts/$originalPrimaryId" $token).Json.data.schedulable
    $routeTestAccountA=0L;$routeTestAccountB=0L;$routeTestKeyId=0L;$sharedAccountKeyId=0L
    $routeTestAPIKey='';$sharedAccountAPIKey=''
    try {
      $routeTestAccountA=[long](Api POST '/admin/accounts' $token @{name="$prefix-$site-cooldown-a";platform='openai';type='apikey';concurrency=5;priority=0;rate_multiplier=1;group_ids=@($primaryGroup);extra=@{openai_passthrough=$false;openai_responses_mode='force_responses'};credentials=(Credentials 'server' 'primary')}).Json.data.id
      $resources[$site].accounts.Add($routeTestAccountA);Save
      $routeTestAccountB=[long](Api POST '/admin/accounts' $token @{name="$prefix-$site-cooldown-b";platform='openai';type='apikey';concurrency=5;priority=1;rate_multiplier=1;group_ids=@($primaryGroup);extra=@{openai_passthrough=$false;openai_responses_mode='force_responses'};credentials=(Credentials 'success' 'primary-b')}).Json.data.id
      $resources[$site].accounts.Add($routeTestAccountB);Save
      Check ($routeTestAccountA -gt 0 -and $routeTestAccountB -gt 0 -and $routeTestAccountA -ne $routeTestAccountB) 'Fresh same-group A/B accounts were not created'
      $routeTestKey=(Api POST '/keys' $userToken @{name="$prefix-$site-cooldown-key";group_id=$primaryGroup;backup_group_ids=@($backupGroup,$thirdGroup);key_route_scope='account_v2';quota=1;key_route_failure_policy=@{rules=@(@{type='status_code';status_codes=@(502);threshold=1;window_seconds=60;cooldown_seconds=120})}}).Json.data
      $routeTestKeyId=[long]$routeTestKey.id;$routeTestAPIKey=[string]$routeTestKey.key
      $resources[$site].keys.Add($routeTestKeyId);Save
      Check ($routeTestKeyId -gt 0 -and [bool]$routeTestAPIKey -and $routeTestKeyId -ne $originalKeyId) 'Dedicated cooldown Key was not created'
      $cooldownPolicy=@{rules=@(@{type='status_code';status_codes=@(502);threshold=1;window_seconds=60;cooldown_seconds=120})}
      $null=Api PUT "/keys/$routeTestKeyId" $userToken @{key_route_failure_policy=$cooldownPolicy}
      Check ((Api GET "/keys/$routeTestKeyId" $userToken).Json.data.key_route_failure_policy.rules[0].status_codes[0] -eq 502) 'Dedicated cooldown Key policy did not persist'
      $null=Api POST "/admin/accounts/$originalPrimaryId/schedulable" $token @{schedulable=$false}
      Start-Sleep -Seconds 3
      $keyId=$routeTestKeyId;$apiKey=$routeTestAPIKey

      # A's first request returns a synthetic 502; B must finish the same
      # request, and the following request must stay in this group on B.
      Gateway 'key-account-cooldown-trigger-same-group-b' 'primary-b' $primaryGroup 2 $false 'success' 'gpt-4o-mini' $routeTestAccountB
      $triggerAttempts=@($evidence.sites[$site].last_attempt.upstream)
      Check ($triggerAttempts.Count -ge 2 -and $triggerAttempts[0].slot -eq 'primary' -and $triggerAttempts[0].scenario -eq 'server' -and $triggerAttempts[-1].slot -eq 'primary-b' -and $triggerAttempts[-1].scenario -eq 'success' -and @($triggerAttempts|Where-Object {$_.slot -ne 'primary' -and $_.slot -ne 'primary-b'}).Count -eq 0 -and @($triggerAttempts|Where-Object {$_.slot -eq 'primary' -and $_.scenario -ne 'server'}).Count -eq 0) '502 did not cool only account A and retry healthy account B in the same group'
      Gateway 'key-account-cooldown-next-request-stays-primary-b' 'primary-b' $primaryGroup 2 $false 'success' 'gpt-4o-mini' $routeTestAccountB
      $nextAttempts=@($evidence.sites[$site].last_attempt.upstream)
      Check ($nextAttempts.Count -eq 1 -and $nextAttempts[0].slot -eq 'primary-b') 'Cooled account A was retried, or routing left the group despite healthy B'

      # Disable B, then prove a second Key sharing A does not inherit Key 1's
      # cooldown. Restore A's mock to success so any request through it succeeds.
      $null=Api POST "/admin/accounts/$routeTestAccountB/schedulable" $token @{schedulable=$false}
      $null=Api PUT "/admin/accounts/$routeTestAccountA" $token @{credentials=(Credentials 'success' 'primary')}
      $sharedAccountKey=(Api POST '/keys' $userToken @{name="$prefix-$site-shared-account-key";group_id=$primaryGroup;backup_group_ids=@($backupGroup,$thirdGroup);key_route_scope='account_v2';quota=1;key_route_failure_policy=@{rules=@(@{type='status_code';status_codes=@(502);threshold=1;window_seconds=60;cooldown_seconds=120})}}).Json.data
      $sharedAccountKeyId=[long]$sharedAccountKey.id;$sharedAccountAPIKey=[string]$sharedAccountKey.key
      $resources[$site].keys.Add($sharedAccountKeyId);Save
      Check ($sharedAccountKeyId -gt 0 -and [bool]$sharedAccountAPIKey -and $sharedAccountKeyId -ne $routeTestKeyId) 'Second Key sharing account A was not created'
      $null=Api PUT "/keys/$sharedAccountKeyId" $userToken @{key_route_failure_policy=$cooldownPolicy}
      Check ((Api GET "/keys/$sharedAccountKeyId" $userToken).Json.data.key_route_failure_policy.rules[0].status_codes[0] -eq 502) 'Second Key cooldown policy did not persist'
      $keyId=$sharedAccountKeyId;$apiKey=$sharedAccountAPIKey
      Gateway 'second-key-shared-account-a-is-not-cooled' 'primary' $primaryGroup 2 $false 'success' 'gpt-4o-mini' $routeTestAccountA
      $secondKeyAttempts=@($evidence.sites[$site].last_attempt.upstream)
      Check ($secondKeyAttempts.Count -eq 1 -and $secondKeyAttempts[0].slot -eq 'primary' -and $secondKeyAttempts[0].scenario -eq 'success') 'Second Key inherited account A cooldown from the first Key'
      $evidence.sites[$site].account_cooldown=@{same_group_account_b_used=$true;key_local_cooldown_persisted=$true;shared_account_isolated_between_keys=$true;account_a_id=$routeTestAccountA;account_b_id=$routeTestAccountB;second_key_id=$sharedAccountKeyId}
      Save
    } finally {
      $keyId=$originalKeyId;$apiKey=$originalAPIKey
      try {
        if($routeTestAccountA -gt 0){$null=Api POST "/admin/accounts/$routeTestAccountA/schedulable" $token @{schedulable=$false}}
        if($routeTestAccountB -gt 0){$null=Api POST "/admin/accounts/$routeTestAccountB/schedulable" $token @{schedulable=$false}}
      } finally {
        $restored=(Api POST "/admin/accounts/$originalPrimaryId/schedulable" $token @{schedulable=$originalPrimary}).Json.data
        Check ($restored.schedulable -eq $originalPrimary) 'Original primary schedulability was not restored after account cooldown acceptance'
        Start-Sleep -Seconds 3
      }
    }
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
    }
    $state=(Sql "SELECT json_agg(json_build_object('id',id,'temp',temp_unschedulable_until,'overload',overload_until)) FROM accounts WHERE id IN ($($resources[$site].accounts -join ','));")|ConvertFrom-Json
    Check (@($state|Where-Object {$_.temp -or $_.overload}).Count -eq 0) "$site custom rules globally paused an account"
    $evidence.sites[$site].no_global_pause=$true;$evidence.sites[$site].final_ledger=Ledger;Save
  }
  if($LongConnectionOnly) {
    Check (@($Sites|Where-Object {-not $longFixtures.ContainsKey($_)}).Count -eq 0) 'Long-SSE fixture setup is incomplete'
    $statsBefore=Stats;$evidence.long_sse=@{duration_seconds=660;heartbeat_seconds=20;client_timeout_ms=800000;sites=@('main','l1','cos');mock_calls_before=$statsBefore.Count};$gatewayCalls=3;Save
    $longClient=Invoke-LongSSEClient
    $psi=[Diagnostics.ProcessStartInfo]::new();$psi.FileName='docker.exe';$psi.UseShellExecute=$false;$psi.CreateNoWindow=$true;$psi.RedirectStandardInput=$true;$psi.RedirectStandardOutput=$true;$psi.RedirectStandardError=$true
    foreach($arg in @('exec','-i',$mockId,'node','-e',$longClient)){$psi.ArgumentList.Add($arg)}
    $longProcess=[Diagnostics.Process]::new();$longProcess.StartInfo=$psi
    Check ($longProcess.Start()) 'Long-SSE client process did not start'
    $stdoutTask=$longProcess.StandardOutput.ReadToEndAsync();$stderrTask=$longProcess.StandardError.ReadToEndAsync()
    $payload=@{targets=@(@{host='main';key=$longFixtures.main.api_key},@{host='l1';key=$longFixtures.l1.api_key},@{host='cos';key=$longFixtures.cos.api_key})}|ConvertTo-Json -Depth 5 -Compress
    $longProcess.StandardInput.WriteLine($payload);$longProcess.StandardInput.Close()
    $activeObserved=$false;$longStart=[DateTime]::UtcNow;$redisDuring=[Collections.Generic.List[object]]::new();$nextSample=$longStart
    while(-not $longProcess.HasExited) {
      if([DateTime]::UtcNow -ge $nextSample) {
        $sample=@{at=[DateTime]::UtcNow.ToString('o');sites=@{}};$allActive=$true
        foreach($s in $Sites){$snap=Redis-SlotSnapshot $s;$sample.sites[$s]=$snap;$values=@($snap.counts.Values);if(@($values|Where-Object {$_ -ne 1}).Count -gt 0){$allActive=$false}}
        if($allActive){$activeObserved=$true;$redisDuring.Add($sample);$evidence.long_sse.redis_during=$redisDuring;$evidence.long_sse.redis_during_samples=$redisDuring.Count;Save}
        elseif($activeObserved -and ([DateTime]::UtcNow-$longStart).TotalSeconds -lt 640){Check $false 'An exact account/user/API-key Redis slot disappeared before the 660-second SSE completion window'}
        elseif(([DateTime]::UtcNow-$longStart).TotalSeconds -gt 45){Check $false 'The three long SSE requests did not acquire all exact Redis slots within 45 seconds'}
        $nextSample=[DateTime]::UtcNow.AddSeconds(15)
        Write-Output "LongConnectionOnly active for $([int]([DateTime]::UtcNow-$longStart).TotalSeconds)s; three-site SSE still bounded and running."
      }
      $null=$longProcess.WaitForExit(1000)
    }
    $longProcess.WaitForExit();$stdout=$stdoutTask.GetAwaiter().GetResult();$stderr=$stderrTask.GetAwaiter().GetResult()
    $results=$stdout|ConvertFrom-Json -AsHashtable -NoEnumerate
    $evidence.long_sse.client_results=$results;$evidence.long_sse.client_exit_code=$longProcess.ExitCode;Save
    Check ($longProcess.ExitCode -eq 0 -and $results.Count -eq 3) "Long-SSE client failed: $($stderr.Trim())"
    Check $activeObserved 'Long SSE did not demonstrate active Redis request slots'
    $mockStatsAfter=Stats;$longCalls=@($mockStatsAfter|Select-Object -Skip $statsBefore.Count)
    Check ($longCalls.Count -eq 3 -and @($longCalls|Where-Object {$_.scenario -ne 'long_connection' -or $_.slot -ne 'primary' -or -not $_.stream -or $_.path -ne '/v1/responses'}).Count -eq 0) 'Long SSE did not make exactly one upstream stream call per fixture request'
    foreach($s in $Sites) {
      $site=$s;$f=$longFixtures[$site];$userId=$f.user_id;$keyId=$f.key_id;$apiKey=$f.api_key
      $result=@($results|Where-Object {$_.host -eq $site})[0];Check ([bool]$result.pass) "$site long SSE delivery/heartbeat/usage assertion failed"
      $deadline=[DateTime]::UtcNow.AddSeconds(30)
      do{$after=Ledger;if($after.usage_count -ge $f.before.usage_count+1){break};Start-Sleep -Milliseconds 250}while([DateTime]::UtcNow -lt $deadline)
      Check ($after.usage_count -eq $f.before.usage_count+1 -and $after.dedup_count -eq $f.before.dedup_count+1) "$site long SSE did not settle exactly once"
      $lastId=if($f.before.last_usage){[long]$f.before.last_usage.id}else{0L}
      $rows=(Sql "SELECT COALESCE(json_agg(x),'[]'::json) FROM (SELECT id,group_id,account_id,input_tokens,output_tokens,total_cost,actual_cost,rate_multiplier FROM usage_logs WHERE api_key_id=$keyId AND id>$lastId ORDER BY id)x;")|ConvertFrom-Json -AsHashtable -NoEnumerate
      Check ($rows.Count -eq 1) "$site long SSE usage row/dedup count differs"
      $u=$rows[0];Check ($u.group_id -eq $f.group_id -and $u.account_id -eq $f.account_id -and $u.input_tokens -eq 10 -and $u.output_tokens -eq 2 -and [decimal]$u.rate_multiplier -eq 2) "$site long SSE usage identity/tokens/group rate differs"
      $cost=[Math]::Round(([decimal]$u.total_cost*2),8,[MidpointRounding]::AwayFromZero)
      Check ([decimal]$u.total_cost -gt 0 -and [Math]::Abs([decimal]$u.actual_cost-$cost) -le [decimal]'0.00000001' -and [decimal]$f.before.balance-[decimal]$after.balance -eq $cost -and [decimal]$after.quota_used-[decimal]$f.before.quota_used -eq $cost) "$site long SSE cost/balance/quota was not charged exactly once"
      $zeroDeadline=[DateTime]::UtcNow.AddSeconds(45);$afterSlots=$null
      do{$afterSlots=Redis-SlotSnapshot $site;if(@($afterSlots.counts.Values|Where-Object {$_ -ne 0}).Count -eq 0){break};Start-Sleep -Seconds 1}while([DateTime]::UtcNow -lt $zeroDeadline)
      Check (@($afterSlots.counts.Values|Where-Object {$_ -ne 0}).Count -eq 0) "$site exact Redis request slots were not released after SSE"
      $evidence.sites[$site].long_connection=@{delivery=$result;upstream_calls=1;redis_before=$evidence.sites[$site].redis_before;redis_during_samples=@($redisDuring|ForEach-Object {$_.sites[$site]});redis_after=$afterSlots;usage=$u;charged=$cost;ledger_after=$after}
      Save
    }
    $evidence.long_sse.mock_calls_after=$mockStatsAfter.Count;$evidence.long_sse.mock_calls_delta=$longCalls.Count;Save
  }
  $evidence.status=if($evidence.findings.Count){'passed-with-findings'}else{'passed'}
} catch {
  $evidence.status='failed';$evidence.error=$_.Exception.Message
  if($mockId){$evidence.mock_logs=(Docker logs --tail 20 $mockId)-join "`n"}
  throw
} finally {
  if($LongConnectionOnly -and $longProcess -and -not $longProcess.HasExited -and $mockId) {
    $pendingMock=(((Docker inspect $mockId)-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
    Check ($pendingMock.Id -eq $mockId -and $pendingMock.Config.Labels['moshu.acceptance.run'] -eq $run -and $pendingMock.NetworkSettings.Networks.Count -eq 1 -and $pendingMock.NetworkSettings.Networks.ContainsKey($network)) 'Refusing early stop of an unowned long-SSE mock'
    $null=Docker rm --force $mockId;$mockRemovedEarly=$true
    if(-not $longProcess.WaitForExit(10000)){ $longProcess.Kill();$longProcess.WaitForExit() }
    $evidence.long_sse.client_stopped_for_cleanup=$true;Save
  }
  if($LongConnectionOnly -and $longFixtures.Count -gt 0) {
    foreach($s in $longFixtures.Keys) {
      $site=$s;$userId=$longFixtures[$s].user_id;$keyId=$longFixtures[$s].key_id
      $deadline=[DateTime]::UtcNow.AddSeconds(45);$snapshot=$null
      do{$snapshot=Redis-SlotSnapshot $s;if(@($snapshot.counts.Values|Where-Object {$_ -ne 0}).Count -eq 0){break};Start-Sleep -Seconds 1}while([DateTime]::UtcNow -lt $deadline)
      $evidence.sites[$s].redis_finally_after_cancel=$snapshot;Save
    }
  }
  if($SharedStateFaultOnly -and $redisPauseAttempted) {
    # Restore Redis before API cleanup even when a bounded batch/assertion fails.
    $restoreRedis=(((Docker inspect $containers['main-redis'])-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
    Check ($restoreRedis.Id -eq $containers['main-redis'] -and $restoreRedis.Config.Labels['com.docker.compose.project'] -eq $Project -and $restoreRedis.Config.Labels['com.docker.compose.service'] -eq 'main-redis') 'Refusing restoration of an unexpected Redis'
    if($restoreRedis.State.Paused){$null=Docker unpause $restoreRedis.Id}
    $evidence.redis_unpaused_in_finally=$true;Save
  }
  if($mockId) {
    $cleanupStandalone=$mockRemovedEarly -or ((& docker.exe inspect --format '{{.State.Running}}' $mockId) -ne 'true')
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
    if($peerId) {
      $peer=(((Docker inspect $peerId)-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
      Check ($peer.Id -eq $peerId -and $peer.Config.Labels['moshu.acceptance.run'] -eq $run -and $peer.Config.Labels['com.docker.compose.project'] -eq $Project) 'Refusing cleanup of an unowned private peer'
      $null=Docker rm --force $peerId
      $evidence.cleanup.Add(@{site='main';kind='private-peer';id=$peerId;status='removed'})
    }
    if(-not $mockRemovedEarly) {
      $mock=(((Docker inspect $mockId)-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
      Check ($mock.Id -eq $mockId -and $mock.Config.Labels['moshu.acceptance.run'] -eq $run) 'Refusing cleanup of unowned mock'
      $null=Docker rm --force $mockId
    }
  }
  foreach($s in $initial.Keys) {
    $c=(((Docker inspect $containers[$s])-join "`n")|ConvertFrom-Json -AsHashtable -NoEnumerate)[0]
    Check ($c.Id -eq $initial[$s].id -and $c.State.StartedAt -eq $initial[$s].started -and $c.RestartCount -eq $initial[$s].restarts) "Existing service unexpectedly restarted: $s"
    Check (-not $c.State.Paused) "Existing service left paused: $s"
  }
  $evidence.existing_services_unchanged=$true;$evidence.finished_at=[DateTime]::UtcNow.ToString('o');Save
  Write-Output "Acceptance $($evidence.status): $gatewayCalls/$gatewayLimit gateway requests. Redacted evidence: $(Join-Path $OutputDirectory 'evidence.json')"
}
