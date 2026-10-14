package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newAccountOverloadCacheTest(t *testing.T) (*miniredis.Miniredis, *tempUnschedCache, time.Time) {
	t.Helper()
	server := miniredis.RunT(t)
	now := time.UnixMilli(1_700_000_000_123)
	server.SetTime(now)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return server, &tempUnschedCache{rdb: client}, now
}

func TestAccountOverloadCacheTripsAcrossInstancesAtSameMillisecond(t *testing.T) {
	server, store, now := newAccountOverloadCacheTest(t)
	otherClient := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = otherClient.Close() })
	other := &tempUnschedCache{rdb: otherClient}
	type result struct {
		until time.Time
		claim bool
		err   error
	}
	results := make(chan result, 12)
	var wg sync.WaitGroup
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cache := store
			if i%2 == 1 {
				cache = other
			}
			until, claim, err := cache.RecordAccountOverloadFailure(context.Background(), 42, 5*time.Minute, 3, time.Minute)
			results <- result{until, claim, err}
		}(i)
	}
	wg.Wait()
	close(results)
	claims, untripped := 0, 0
	for result := range results {
		require.NoError(t, result.err)
		if result.until.IsZero() {
			untripped++
		} else {
			require.Equal(t, now.Add(time.Minute), result.until)
		}
		if result.claim {
			claims++
		}
	}
	require.Equal(t, 2, untripped)
	require.Equal(t, 1, claims)
	keys := accountOverloadFailureKeys(42)
	require.False(t, server.Exists(keys[0]))
	require.False(t, server.Exists(keys[1]))
	require.Equal(t, time.Minute, server.TTL(keys[2]))
	for _, key := range keys {
		require.Contains(t, key, "{42}")
	}
}

func TestAccountOverloadCacheSlidingWindowDropsBoundaryFailures(t *testing.T) {
	server, store, now := newAccountOverloadCacheTest(t)
	ctx := context.Background()
	for _, offset := range []time.Duration{0, 4 * time.Minute, 5 * time.Minute} {
		server.SetTime(now.Add(offset))
		until, claim, err := store.RecordAccountOverloadFailure(ctx, 42, 5*time.Minute, 3, time.Minute)
		require.NoError(t, err)
		require.True(t, until.IsZero(), "failure exactly at the window boundary must expire")
		require.False(t, claim)
	}
	server.SetTime(now.Add(5*time.Minute + time.Millisecond))
	until, claim, err := store.RecordAccountOverloadFailure(ctx, 42, 5*time.Minute, 3, time.Minute)
	require.NoError(t, err)
	require.True(t, claim)
	require.Equal(t, now.Add(6*time.Minute+time.Millisecond), until)
	otherUntil, otherClaim, err := store.RecordAccountOverloadFailure(ctx, 43, 5*time.Minute, 3, time.Minute)
	require.NoError(t, err)
	require.True(t, otherUntil.IsZero())
	require.False(t, otherClaim)
}

func TestAccountOverloadCacheFixedDeadlineAndFreshWindowAfterCooldown(t *testing.T) {
	server, store, now := newAccountOverloadCacheTest(t)
	ctx := context.Background()
	until, claim, err := store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 1, time.Minute)
	require.NoError(t, err)
	require.True(t, claim)
	require.NoError(t, store.CompleteAccountOverloadCooldown(ctx, 42, until, true))
	server.FastForward(45 * time.Second)
	server.SetTime(now.Add(45 * time.Second))
	for i := 0; i < 4; i++ {
		nextUntil, nextClaim, err := store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 3, 10*time.Minute)
		require.NoError(t, err)
		require.Equal(t, until, nextUntil, "active cooldown must ignore later policy changes")
		require.False(t, nextClaim)
	}
	require.Equal(t, 15*time.Second, server.TTL(accountOverloadFailureKeys(42)[2]))
	server.FastForward(15 * time.Second)
	server.SetTime(until)
	require.Empty(t, server.Keys())
	for i := 1; i <= 3; i++ {
		nextUntil, nextClaim, err := store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 3, time.Minute)
		require.NoError(t, err)
		require.Equal(t, i == 3, nextClaim)
		if i < 3 {
			require.True(t, nextUntil.IsZero(), "failures during cooldown must not seed the next window")
		} else {
			require.Equal(t, until.Add(time.Minute), nextUntil)
		}
	}
}

func TestAccountOverloadCachePersistenceLeaseCompensation(t *testing.T) {
	server, store, now := newAccountOverloadCacheTest(t)
	ctx := context.Background()
	until, claim, err := store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 1, time.Minute)
	require.NoError(t, err)
	require.True(t, claim)
	_, claim, err = store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 1, time.Minute)
	require.NoError(t, err)
	require.False(t, claim)
	require.NoError(t, store.CompleteAccountOverloadCooldown(ctx, 42, until, false))
	_, claim, err = store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 1, time.Minute)
	require.NoError(t, err)
	require.True(t, claim, "failed persistence must release the lease immediately")
	server.SetTime(now.Add(accountOverloadPersistLease - time.Millisecond))
	_, claim, err = store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 1, time.Minute)
	require.NoError(t, err)
	require.False(t, claim)
	server.SetTime(now.Add(accountOverloadPersistLease))
	gotUntil, claim, err := store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 1, time.Minute)
	require.NoError(t, err)
	require.True(t, claim, "crashed owners must be replaceable when their lease expires")
	require.Equal(t, until, gotUntil)
	require.NoError(t, store.CompleteAccountOverloadCooldown(ctx, 42, until, true))
	require.NoError(t, store.CompleteAccountOverloadCooldown(ctx, 42, until, false))
	server.SetTime(now.Add(30 * time.Second))
	_, claim, err = store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 1, time.Minute)
	require.NoError(t, err)
	require.False(t, claim, "late failures cannot erase successful persistence")
}

func TestAccountOverloadCacheStaleCompletionCannotModifyNewCooldown(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		t.Run(fmt.Sprintf("persisted=%t", persisted), func(t *testing.T) {
			server, store, now := newAccountOverloadCacheTest(t)
			ctx := context.Background()
			oldUntil, _, err := store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 1, time.Minute)
			require.NoError(t, err)
			server.SetTime(now.Add(2 * time.Minute))
			newUntil, claim, err := store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 1, time.Minute)
			require.NoError(t, err)
			require.True(t, claim)
			require.NoError(t, store.CompleteAccountOverloadCooldown(ctx, 42, oldUntil, persisted))
			gotUntil, claim, err := store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 1, time.Minute)
			require.NoError(t, err)
			require.Equal(t, newUntil, gotUntil)
			require.False(t, claim, "stale completion cannot release the new lease")
			server.SetTime(now.Add(2*time.Minute + accountOverloadPersistLease))
			_, claim, err = store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 1, time.Minute)
			require.NoError(t, err)
			require.True(t, claim, "stale success cannot mark the new cooldown persisted")
		})
	}
}

func TestAccountOverloadCacheExpirationAndCompletionDoNotLeakKeys(t *testing.T) {
	server, store, now := newAccountOverloadCacheTest(t)
	ctx := context.Background()
	_, _, err := store.RecordAccountOverloadFailure(ctx, 42, 5*time.Minute, 3, time.Minute)
	require.NoError(t, err)
	for _, key := range server.Keys() {
		require.Equal(t, 5*time.Minute, server.TTL(key))
	}
	server.FastForward(5 * time.Minute)
	require.Empty(t, server.Keys())
	require.NoError(t, store.CompleteAccountOverloadCooldown(ctx, 42, now.Add(time.Minute), true))
	require.Empty(t, server.Keys(), "completion must not recreate expired state")
}

func TestAccountOverloadCacheRejectsInvalidInputAndUnavailableRedis(t *testing.T) {
	_, store, now := newAccountOverloadCacheTest(t)
	ctx := context.Background()
	for _, cache := range []*tempUnschedCache{nil, {}} {
		_, _, err := cache.RecordAccountOverloadFailure(ctx, 42, time.Minute, 3, time.Minute)
		require.Error(t, err)
		require.Error(t, cache.CompleteAccountOverloadCooldown(ctx, 42, now, true))
		require.Error(t, cache.ClearAccountOverloadFailures(ctx, 42))
	}
	for _, test := range []struct {
		id        int64
		window    time.Duration
		threshold int
		cooldown  time.Duration
	}{
		{0, time.Minute, 3, time.Minute},
		{42, 0, 3, time.Minute},
		{42, time.Minute, 0, time.Minute},
		{42, time.Minute, 3, 0},
	} {
		_, _, err := store.RecordAccountOverloadFailure(ctx, test.id, test.window, test.threshold, test.cooldown)
		require.Error(t, err)
	}
	require.Error(t, store.CompleteAccountOverloadCooldown(ctx, 0, now, true))
	require.Error(t, store.CompleteAccountOverloadCooldown(ctx, 42, time.Time{}, true))
	require.Error(t, store.ClearAccountOverloadFailures(ctx, 0))
	require.NoError(t, store.rdb.Close())
	_, _, err := store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 3, time.Minute)
	require.Error(t, err)
	require.Error(t, store.CompleteAccountOverloadCooldown(ctx, 42, now, true))
	require.Error(t, store.ClearAccountOverloadFailures(ctx, 42))
}

func TestAccountOverloadCacheExplicitClearResetsOnlySelectedAccount(t *testing.T) {
	server, store, now := newAccountOverloadCacheTest(t)
	ctx := context.Background()
	for _, accountID := range []int64{42, 43} {
		_, _, err := store.RecordAccountOverloadFailure(ctx, accountID, time.Minute, 3, time.Minute)
		require.NoError(t, err)
	}
	require.NoError(t, store.ClearAccountOverloadFailures(ctx, 42))
	for _, key := range accountOverloadFailureKeys(42) {
		require.False(t, server.Exists(key))
	}
	require.True(t, server.Exists(accountOverloadFailureKeys(43)[0]))
	until, claim, err := store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 1, time.Minute)
	require.NoError(t, err)
	require.True(t, claim)
	require.Equal(t, now.Add(time.Minute), until)
	require.NoError(t, store.ClearAccountOverloadFailures(ctx, 42))
	require.NoError(t, store.CompleteAccountOverloadCooldown(ctx, 42, until, true))
	for _, key := range accountOverloadFailureKeys(42) {
		require.False(t, server.Exists(key))
	}
	for i := 1; i <= 3; i++ {
		gotUntil, gotClaim, err := store.RecordAccountOverloadFailure(ctx, 42, time.Minute, 3, time.Minute)
		require.NoError(t, err)
		require.Equal(t, i == 3, gotClaim)
		require.Equal(t, i < 3, gotUntil.IsZero())
	}
}

func TestAccountOverloadCacheRoundsSubMillisecondDurationsUp(t *testing.T) {
	server, store, now := newAccountOverloadCacheTest(t)
	until, claim, err := store.RecordAccountOverloadFailure(context.Background(), 42, time.Nanosecond, 1, time.Nanosecond)
	require.NoError(t, err)
	require.True(t, claim)
	require.Equal(t, now.Add(time.Millisecond), until)
	require.Equal(t, time.Millisecond, server.TTL(accountOverloadFailureKeys(42)[2]))
}
