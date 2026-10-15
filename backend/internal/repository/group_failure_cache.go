package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// One hash per account permits atomic manual recovery without SCAN or dynamic
// script keys. It shares the existing account-wide cooldown and Cluster slot.
var recordAccountGroupFailureScript = redis.NewScript(`
local clock = redis.call('TIME')
local now = tonumber(clock[1])*1000 + math.floor(tonumber(clock[2])/1000)
local until_ms = tonumber(redis.call('HGET', KEYS[2], 'until_ms')) or 0
if until_ms > now then
  if redis.call('HGET', KEYS[2], 'persisted') == '1' then return {until_ms,0} end
  local lease = tonumber(redis.call('HGET', KEYS[2], 'lease_until_ms')) or 0
  if lease > now then return {until_ms,0} end
  redis.call('HSET', KEYS[2], 'lease_until_ms', now+tonumber(ARGV[5]))
  return {until_ms,1}
end
local field = ARGV[1]
local window = tonumber(ARGV[2])
local threshold = tonumber(ARGV[3])
local cooldown = tonumber(ARGV[4])
local raw = redis.call('HGET', KEYS[1], field)
if not raw and redis.call('HLEN', KEYS[1]) >= 256 then
  local all = redis.call('HGETALL', KEYS[1])
  for i=1,#all,2 do
    local item = cjson.decode(all[i+1])
    if tonumber(item.expires) <= now then redis.call('HDEL', KEYS[1], all[i]) end
  end
  if redis.call('HLEN', KEYS[1]) >= 256 then return redis.error_reply('group failure counter capacity reached') end
end
local times = {}
if raw then
  local item = cjson.decode(raw)
  for _,t in ipairs(item.failures) do if t > now-window then times[#times+1]=t end end
end
times[#times+1]=now
if #times < threshold then
  redis.call('HSET', KEYS[1], field, cjson.encode({expires=now+window,failures=times}))
  local ttl = redis.call('PTTL', KEYS[1])
  redis.call('PEXPIRE', KEYS[1], math.max(ttl,window))
  return {0,0}
end
until_ms=now+cooldown
redis.call('HSET', KEYS[2], 'until_ms',until_ms,'lease_until_ms',now+tonumber(ARGV[5]),'persisted',0)
redis.call('PEXPIRE', KEYS[2],cooldown)
redis.call('DEL', KEYS[1])
return {until_ms,1}
`)

func accountGroupFailureKey(accountID int64) string {
	return fmt.Sprintf("account_group_failure:{%d}:counters", accountID)
}

func (c *tempUnschedCache) RecordAccountGroupFailure(ctx context.Context, accountID, groupID int64, kind, revision string, window time.Duration, threshold int, cooldown time.Duration) (time.Time, bool, error) {
	if c == nil || c.rdb == nil || accountID <= 0 || groupID <= 0 || threshold < 1 || threshold > 100 ||
		window < time.Second || window > time.Hour || cooldown < time.Second || cooldown > 2*time.Hour {
		return time.Time{}, false, fmt.Errorf("invalid group failure counter arguments")
	}
	switch kind {
	case "overload", "server_error", "timeout", "rate_limit":
	default:
		return time.Time{}, false, fmt.Errorf("invalid group failure kind")
	}
	if len(revision) != 16 {
		return time.Time{}, false, fmt.Errorf("invalid group failure revision")
	}
	for _, r := range revision {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return time.Time{}, false, fmt.Errorf("invalid group failure revision")
		}
	}
	field := strconv.FormatInt(groupID, 10) + ":" + kind + ":" + revision
	result, err := recordAccountGroupFailureScript.Run(ctx, c.rdb, []string{accountGroupFailureKey(accountID), accountOverloadFailureKeys(accountID)[2]},
		field, window.Milliseconds(), threshold, cooldown.Milliseconds(), accountOverloadPersistLease.Milliseconds()).Slice()
	if err != nil {
		return time.Time{}, false, fmt.Errorf("record group failure: %w", err)
	}
	if len(result) != 2 {
		return time.Time{}, false, fmt.Errorf("invalid group failure result")
	}
	until, ok := result[0].(int64)
	claim, claimOK := result[1].(int64)
	if !ok || !claimOK {
		return time.Time{}, false, fmt.Errorf("invalid group failure result types")
	}
	if until == 0 {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(until), claim == 1, nil
}

func (c *tempUnschedCache) ClearAccountGroupFailures(ctx context.Context, accountID int64) error {
	if c == nil || c.rdb == nil || accountID <= 0 {
		return fmt.Errorf("invalid group failure recovery arguments")
	}
	// Include the shared cooldown: successful manual recovery must remove both
	// counters and cooldown, and stale completion ACKs cannot recreate either.
	return c.rdb.Del(ctx, accountGroupFailureKey(accountID), accountOverloadFailureKeys(accountID)[2]).Err()
}
