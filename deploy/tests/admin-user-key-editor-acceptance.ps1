[CmdletBinding()]
param(
  [switch]$Execute,
  [ValidateSet('http://127.0.0.1:18280')][string]$BaseUrl = 'http://127.0.0.1:18280',
  [string]$ExpectedImageId = '',
  [string]$OutputDirectory = ''
)

# Local synthetic admin CRUD only. Never sends a gateway/model request, writes
# SQL, restarts a service, accepts compliance, or changes existing user data.
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion.Major -lt 7) { throw 'PowerShell 7 required' }
if (-not $Execute) { Write-Output 'Use -Execute -ExpectedImageId sha256:<candidate>. Limited to the labelled local project at 127.0.0.1:18280; synthetic user/key/group CRUD only, zero model calls; exact-ID cleanup and redacted evidence.'; return }
function Check([bool]$Condition,[string]$Message) { if (-not $Condition) { throw $Message } }
Check ($ExpectedImageId -match '^sha256:[a-f0-9]{64}$') 'Exact candidate image ID required'
$project = 'moshu-reseller-acceptance-key-routing'
$run = [Guid]::NewGuid().ToString('N').Substring(0,12)
$prefix = "admin-key-edit-$run"
$containerName = "$project-main-1"
$raw = & docker.exe inspect $containerName
Check ($LASTEXITCODE -eq 0) 'Cannot inspect local acceptance main container'
$container = @($raw -join "`n" | ConvertFrom-Json -AsHashtable)[0]
Check ($container.Config.Labels['com.docker.compose.project'] -eq $project -and $container.Config.Labels['com.docker.compose.service'] -eq 'main') 'Refusing non-acceptance container'
Check ($container.State.Running -and $container.State.Health.Status -eq 'healthy') 'Acceptance main must be healthy'
Check ($container.Image -eq $ExpectedImageId) 'Application image does not match candidate'
$bindings = @($container.NetworkSettings.Ports['8080/tcp'])
Check (@($bindings | Where-Object { $_.HostPort -eq '18280' -and $_.HostIp -eq '127.0.0.1' }).Count -eq 1) 'Expected loopback-only 18280 mapping not found'
if (-not $OutputDirectory) { $OutputDirectory = "D:/projects/$prefix" }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
Check (-not (Test-Path -LiteralPath $OutputDirectory)) 'Report directory must be new'
New-Item -ItemType Directory -Path $OutputDirectory | Out-Null
$evidence = [ordered]@{
  status='running'; run=$run; started_at=[DateTime]::UtcNow.ToString('o'); base_url=$BaseUrl;
  project=$project; application_container=$container.Id; image_id=$ExpectedImageId;
  real_model_calls=0; sql_writes=0; existing_data_modified=$false;
  script_sha256=(Get-FileHash -LiteralPath $PSCommandPath -Algorithm SHA256).Hash.ToLowerInvariant();
  checks=[Collections.Generic.List[object]]::new(); cleanup=[Collections.Generic.List[object]]::new()
}
$resources = @{groups=[Collections.Generic.List[long]]::new(); users=[Collections.Generic.List[long]]::new(); keys=[Collections.Generic.List[long]]::new()}
$adminToken=''; $userToken=''; $testUser=0; $testKey=0; $failed=$false
function Save { $evidence | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath (Join-Path $OutputDirectory 'evidence.json') -Encoding utf8 }
function Http([string]$Method,[string]$Path,[string]$Token='',[object]$Body=$null) {
  Check ($Path.StartsWith('/api/v1/') -and -not $Path.Contains('://') -and -not $Path.Contains('..')) 'Non-panel path rejected'
  $headers=@{}; if ($Token) { $headers.Authorization="Bearer $Token" }
  $params=@{Uri="$BaseUrl$Path";Method=$Method;Headers=$headers;SkipHttpErrorCheck=$true;MaximumRedirection=0;TimeoutSec=30}
  if ($null -ne $Body) { $params.ContentType='application/json'; $params.Body=$Body|ConvertTo-Json -Compress -Depth 15 }
  try { $response=Invoke-WebRequest @params } catch { throw "Local HTTP transport failed: $Method $Path" }
  $json=$null; try { $json=$response.Content|ConvertFrom-Json -AsHashtable } catch {}
  return @{Status=[int]$response.StatusCode;Json=$json}
}
function Api([string]$Method,[string]$Path,[string]$Token=$script:adminToken,[object]$Body=$null,[int]$Expected=200) {
  $r=Http $Method "/api/v1$Path" $Token $Body
  Check ($r.Status -eq $Expected) "$Method $Path expected $Expected, got $($r.Status)"
  return $r.Json.data
}
function Record([string]$Name,[object]$Details) { $evidence.checks.Add(@{name=$Name;passed=$true;details=$Details}); Save }
function Key([long]$Owner,[long]$ID) {
  $page=Api GET "/admin/users/$Owner/api-keys?page=1&page_size=100"
  $found=@($page.items | Where-Object { [long]$_.id -eq $ID })
  Check ($found.Count -eq 1) 'Synthetic key not found in owner list'
  return $found[0]
}
function Near([double]$Actual,[double]$Expected,[string]$Label) { Check ([Math]::Abs($Actual-$Expected) -lt 0.000000001) "$Label differs" }
Save
try {
  $login=Api POST '/auth/login' '' @{email='admin-main@acceptance.invalid';password='acceptance-admin-password'}
  $adminToken=[string]$login.access_token
  Check ($adminToken.Length -gt 10) 'Missing synthetic admin session'
  $user=Api POST '/admin/users' $adminToken @{email="$prefix@acceptance.invalid";password='Synthetic-Key-Editor-2026!';role='user';balance=10;concurrency=2;allowed_groups=@()}
  $testUser=[long]$user.id; Check ($testUser -gt 0) 'Invalid created user'; $resources.users.Add($testUser)
  foreach ($slot in @('one','two')) {
    $group=Api POST '/admin/groups' $adminToken @{name="$prefix-$slot";platform='openai';subscription_type='standard';rate_multiplier=1;is_exclusive=$true}
    $id=[long]$group.id; Check ($id -gt 0) 'Invalid created group'; $resources.groups.Add($id)
    Check ($group.status -eq 'active' -and $group.is_exclusive) 'Fixture group must be exclusive and active'
  }
  $keyResponse=Api POST "/admin/users/$testUser/api-keys" $adminToken @{name="$prefix-key";quota=12}
  $testKey=[long]$keyResponse.api_key.id; Check ($testKey -gt 0) 'Invalid created key'; $resources.keys.Add($testKey)
  $before=Key $testUser $testKey
  Check ($null -eq $before.group_id -and @($before.backup_group_ids).Count -eq 0) 'Fixture key must start unbound'
  $owner=Api GET "/admin/users/$testUser"
  Check (@($owner.allowed_groups | Where-Object { $null -ne $_ }).Count -eq 0) 'Fixture owner must start without allowed groups'

  $duplicate=Http PUT "/api/v1/admin/api-keys/$testKey" $adminToken @{name="$prefix-duplicate";group_id=$resources.groups[0];backup_group_ids=@($resources.groups[0]);key_route_scope='account_v2';quota=30}
  Check ($duplicate.Status -eq 400) 'Duplicate route must be rejected'
  $owner=Api GET "/admin/users/$testUser"; $unchanged=Key $testUser $testKey
  Check (@($owner.allowed_groups | Where-Object { $null -ne $_ }).Count -eq 0 -and $null -eq $unchanged.group_id -and $unchanged.name -eq $before.name) 'Failed validation must not leave grants or key changes'
  Near $unchanged.quota $before.quota 'Failed edit quota'
  Record 'duplicate-route-no-side-effects' @{status=400;owner_groups=0;key_unchanged=$true}

  $expiry=[DateTime]::UtcNow.AddDays(2).ToString('yyyy-MM-ddTHH:mm:ssZ')
  $payload=@{
    name="$prefix-complete"; group_id=$resources.groups[0]; backup_group_ids=@($resources.groups[1]); key_route_scope='account_v2';
    key_route_failure_policy=@{rules=@(@{type='consecutive_failures';threshold=3;cooldown_seconds=120})};
    key_model_policy=@{enabled=$true;models=@('gpt-4o-mini')}; status='active';
    ip_whitelist=@('192.0.2.1','198.51.100.0/24');ip_blacklist=@('203.0.113.9');quota=30;expires_at=$expiry;
    rate_limit_5h=3;rate_limit_1d=5;rate_limit_7d=9
  }
  $saved=Api PUT "/admin/api-keys/$testKey" $adminToken $payload
  Check ($saved.auto_granted_group_access) 'Admin save must grant selected exclusive groups'
  $got=Key $testUser $testKey; $owner=Api GET "/admin/users/$testUser"
  Check ((@($owner.allowed_groups | Sort-Object) -join ',') -eq (@($resources.groups | Sort-Object) -join ',')) 'Owner group grants did not persist'
  Check ($got.name -eq $payload.name -and [long]$got.group_id -eq $resources.groups[0]) 'Name/priority-one group did not persist'
  Check ((@($got.backup_group_ids) -join ',') -eq [string]$resources.groups[1]) 'Later-priority group did not persist'
  Check ($got.key_route_scope -eq 'account_v2' -and [long]$got.key_route_revision -gt 0) 'Account routing metadata missing'
  Check ($got.key_route_failure_policy.rules[0].type -eq 'consecutive_failures' -and $got.key_route_failure_policy.rules[0].threshold -eq 3 -and $got.key_route_failure_policy.rules[0].cooldown_seconds -eq 120) 'Failure rule did not persist'
  Check ($got.key_model_policy.enabled -and ($got.key_model_policy.models -join ',') -eq 'gpt-4o-mini') 'Model policy did not persist'
  Check (($got.ip_whitelist -join ',') -eq ($payload.ip_whitelist -join ',') -and ($got.ip_blacklist -join ',') -eq ($payload.ip_blacklist -join ',')) 'IP restrictions did not persist'
  Check (([DateTimeOffset]$got.expires_at).ToUniversalTime() -eq [DateTimeOffset]::Parse($expiry).ToUniversalTime()) 'Expiration did not persist'
  foreach($field in @('quota','rate_limit_5h','rate_limit_1d','rate_limit_7d')) { Near $got[$field] $payload[$field] $field }
  Near $got.quota_used $before.quota_used 'Unrequested quota usage reset'
  Record 'full-edit-and-atomic-exclusive-grants' @{group_id=$got.group_id;backup_group_ids=$got.backup_group_ids;owner_granted_groups=$owner.allowed_groups;route_scope=$got.key_route_scope;route_revision=$got.key_route_revision;policy_persisted=$true;ip_persisted=$true;expiry_persisted=$true;quota_used_unchanged=$true;quota_used_initial=$before.quota_used;usage_fixture_note='No inference was sent; usage starts at zero. Native nonzero usage preservation is covered by backend write-mask tests.'}

  $userLogin=Api POST '/auth/login' '' @{email="$prefix@acceptance.invalid";password='Synthetic-Key-Editor-2026!'}
  $userToken=[string]$userLogin.access_token
  $denied=Http PUT "/api/v1/admin/api-keys/$testKey" $userToken @{name='not-authorized'}
  Check ($denied.Status -eq 403) 'Ordinary user must not access admin key editor'
  Record 'ordinary-user-admin-edit-denied' @{status=$denied.Status}

  $other=Api POST '/admin/users' $adminToken @{email="$prefix-other@acceptance.invalid";password='Synthetic-Key-Editor-2026!';role='user';balance=0;concurrency=1}
  $otherID=[long]$other.id;Check ($otherID -gt 0) 'Invalid other user';$resources.users.Add($otherID)
  $otherKey=Api POST "/admin/users/$otherID/api-keys" $adminToken @{name="$prefix-other-key"}
  $otherKeyID=[long]$otherKey.api_key.id;Check ($otherKeyID -gt 0) 'Invalid other key';$resources.keys.Add($otherKeyID)
  $crossOwner=Http PUT "/api/v1/keys/$otherKeyID" $userToken @{name='not-owner'}
  Check ($crossOwner.Status -eq 403) 'Self-service must reject another owner key update'
  $otherUnchanged=Key $otherID $otherKeyID
  Check ($otherUnchanged.name -eq "$prefix-other-key") 'Denied owner edit changed key'
  Record 'self-service-owner-isolation' @{status=$crossOwner.Status;target_unchanged=$true}

  $null=Api PUT "/admin/api-keys/$testKey" $adminToken @{name="$prefix-cleared";group_id=0;backup_group_ids=@();key_route_scope='account_v2';key_route_failure_policy=$null;key_model_policy=$null;ip_whitelist=@();ip_blacklist=@();expires_at='';quota=0;rate_limit_5h=0;rate_limit_1d=0;rate_limit_7d=0;status='inactive'}
  $cleared=Key $testUser $testKey
  Check ($null -eq $cleared.group_id -and @($cleared.backup_group_ids).Count -eq 0) 'Unbind did not persist'
  Check ($null -eq $cleared.key_model_policy -and $null -eq $cleared.key_route_failure_policy -and $null -eq $cleared.expires_at) 'Explicit null/empty expiration must clear policies/expiry'
  Check (@($cleared.ip_whitelist | Where-Object { $null -ne $_ }).Count -eq 0 -and @($cleared.ip_blacklist | Where-Object { $null -ne $_ }).Count -eq 0 -and $cleared.status -eq 'inactive') 'Clear IP/status did not persist'
  foreach($field in @('quota','rate_limit_5h','rate_limit_1d','rate_limit_7d')) { Near $cleared[$field] 0 $field }
  Near $cleared.quota_used $before.quota_used 'Clear edit quota usage'
  Record 'clear-policies-expiry-and-unbind' @{group_unbound=$true;policies_cleared=$true;ip_cleared=$true;expiry_cleared=$true;status='inactive';quota_used_unchanged=$true}
  $evidence.status='passed'
} catch {
  $failed=$true;$evidence.status='failed';$evidence.error=$_.Exception.Message
} finally {
  # Only IDs returned by this run's create operations can reach deletion.
  foreach($kind in @('keys','users','groups')) {
    foreach($id in $resources[$kind]) {
      try {
        Check ($id -gt 0 -and $adminToken.Length -gt 10) 'Invalid cleanup ID/session'
        $path=if($kind -eq 'keys'){"/admin/api-keys/$id"}else{"/admin/$kind/$id"}
        if($kind -eq 'users') { $target=Api GET $path;Check ($target.email.StartsWith($prefix)) 'User cleanup ownership mismatch' }
        if($kind -eq 'groups') { $target=Api GET $path;Check ($target.name.StartsWith($prefix)) 'Group cleanup ownership mismatch' }
        $r=Http DELETE "/api/v1$path" $adminToken
        Check ($r.Status -eq 200) 'Fixture cleanup rejected'
        $evidence.cleanup.Add(@{kind=$kind;id=$id;status=$r.Status})
      } catch { $evidence.cleanup.Add(@{kind=$kind;id=$id;error=$_.Exception.Message});$failed=$true;$evidence.status='failed' }
    }
  }
  $evidence.finished_at=[DateTime]::UtcNow.ToString('o');Save
  $adminToken='';$userToken='';$login=$null;$userLogin=$null;$keyResponse=$null;$otherKey=$null
}
Write-Output "Admin key editor acceptance $($evidence.status); evidence: $(Join-Path $OutputDirectory 'evidence.json')"
if($failed){throw 'Admin key editor acceptance failed; see redacted evidence'}
