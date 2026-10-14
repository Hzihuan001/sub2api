package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const accountOverloadPersistLease = 10 * time.Second

var recordAccountOverloadFailureScript = redis.NewScript(`
	if redis.replicate_commands then redis.replicate_commands() end
	local now = redis.call('TIME')
	local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
	local window_ms = tonumber(ARGV[1])
	local threshold = tonumber(ARGV[2])
	local cooldown_ms = tonumber(ARGV[3])
	local lease_ms = tonumber(ARGV[4])
	local until_ms = tonumber(redis.call('HGET', KEYS[3], 'until_ms')) or 0

	if until_ms > now_ms then
		if redis.call('HGET', KEYS[3], 'persisted') == '1' then
			return {until_ms, 0}
		end
		local lease_until_ms = tonumber(redis.call('HGET', KEYS[3], 'lease_until_ms')) or 0
		if lease_until_ms > now_ms then
			return {until_ms, 0}
		end
		redis.call('HSET', KEYS[3], 'lease_until_ms', now_ms + lease_ms)
		return {until_ms, 1}
	end

	redis.call('DEL', KEYS[3])
	redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now_ms - window_ms)
	local sequence = redis.call('INCR', KEYS[2])
	redis.call('ZADD', KEYS[1], now_ms, tostring(sequence))
	local count = redis.call('ZCARD', KEYS[1])
	redis.call('PEXPIRE', KEYS[1], window_ms)
	redis.call('PEXPIRE', KEYS[2], window_ms)
	if count < threshold then
		return {0, 0}
	end

	until_ms = now_ms + cooldown_ms
	redis.call('HSET', KEYS[3], 'until_ms', until_ms,
		'lease_until_ms', now_ms + lease_ms, 'persisted', 0)
	redis.call('PEXPIRE', KEYS[3], cooldown_ms)
	redis.call('DEL', KEYS[1], KEYS[2])
	return {until_ms, 1}
`)

var completeAccountOverloadCooldownScript = redis.NewScript(`
	if redis.replicate_commands then redis.replicate_commands() end
	local until_ms = tonumber(redis.call('HGET', KEYS[1], 'until_ms')) or 0
	if until_ms ~= tonumber(ARGV[1]) then
		return 0
	end
	local now = redis.call('TIME')
	local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
	if until_ms <= now_ms or redis.call('HGET', KEYS[1], 'persisted') == '1' then
		return 0
	end
	if ARGV[2] == '1' then
		redis.call('HSET', KEYS[1], 'persisted', 1)
	end
	redis.call('HDEL', KEYS[1], 'lease_until_ms')
	return 1
`)

func accountOverloadFailureKeys(accountID int64) []string {
	// All script keys are explicit and share a Redis Cluster hash slot.
	prefix := fmt.Sprintf("account_overload:{%d}:", accountID)
	return []string{prefix + "failures", prefix + "sequence", prefix + "cooldown"}
}

// RecordAccountOverloadFailure accumulates failures in (now-window, now]. Once
// tripped, every caller receives the same deadline without extending it. Only
// one caller leases persistence; a failed or abandoned lease can be reclaimed.
func (c *tempUnschedCache) RecordAccountOverloadFailure(ctx context.Context, accountID int64, window time.Duration, threshold int, cooldown time.Duration) (time.Time, bool, error) {
	if c == nil || c.rdb == nil {
		return time.Time{}, false, fmt.Errorf("record account overload failure: Redis client is unavailable")
	}
	if accountID <= 0 || window <= 0 || threshold <= 0 || cooldown <= 0 {
		return time.Time{}, false, fmt.Errorf("record account overload failure: account, window, threshold and cooldown must be positive")
	}
	result, err := recordAccountOverloadFailureScript.Run(ctx, c.rdb, accountOverloadFailureKeys(accountID),
		overloadDurationMilliseconds(window), threshold, overloadDurationMilliseconds(cooldown), accountOverloadPersistLease.Milliseconds()).Slice()
	if err != nil {
		return time.Time{}, false, fmt.Errorf("record account overload failure: %w", err)
	}
	if len(result) != 2 {
		return time.Time{}, false, fmt.Errorf("record account overload failure: unexpected result length %d", len(result))
	}
	untilMS, untilOK := result[0].(int64)
	claim, claimOK := result[1].(int64)
	if !untilOK || !claimOK {
		return time.Time{}, false, fmt.Errorf("record account overload failure: unexpected result types %T/%T", result[0], result[1])
	}
	if untilMS == 0 {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(untilMS), claim == 1, nil
}

// CompleteAccountOverloadCooldown only updates the matching active deadline.
// Successful persistence is monotonic; a late failure cannot erase it. The
// deadline is also a generation guard against completions from older cooldowns.
func (c *tempUnschedCache) CompleteAccountOverloadCooldown(ctx context.Context, accountID int64, until time.Time, persisted bool) error {
	if c == nil || c.rdb == nil {
		return fmt.Errorf("complete account overload cooldown: Redis client is unavailable")
	}
	if accountID <= 0 || until.IsZero() {
		return fmt.Errorf("complete account overload cooldown: account and deadline must be valid")
	}
	completed := 0
	if persisted {
		completed = 1
	}
	if err := completeAccountOverloadCooldownScript.Run(ctx, c.rdb,
		[]string{accountOverloadFailureKeys(accountID)[2]}, until.UnixMilli(), completed).Err(); err != nil {
		return fmt.Errorf("complete account overload cooldown: %w", err)
	}
	return nil
}

// ClearAccountOverloadFailures resets both the rolling window and cooldown for
// an explicit operator reset. Late completions cannot recreate the deleted hash.
func (c *tempUnschedCache) ClearAccountOverloadFailures(ctx context.Context, accountID int64) error {
	if c == nil || c.rdb == nil {
		return fmt.Errorf("clear account overload failures: Redis client is unavailable")
	}
	if accountID <= 0 {
		return fmt.Errorf("clear account overload failures: account must be positive")
	}
	if err := c.rdb.Del(ctx, accountOverloadFailureKeys(accountID)...).Err(); err != nil {
		return fmt.Errorf("clear account overload failures: %w", err)
	}
	return nil
}

func overloadDurationMilliseconds(duration time.Duration) int64 {
	// Round positive sub-millisecond durations up without overflowing Duration.
	ms := duration.Milliseconds()
	if duration%time.Millisecond != 0 {
		ms++
	}
	return ms
}
