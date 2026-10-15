package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGroupFailureCacheIsolationRecoveryAndSharedCooldown(t *testing.T) {
	_, cache, now := newAccountOverloadCacheTest(t)
	ctx := context.Background()
	revision := "0123456789abcdef"
	for _, g := range []int64{7, 8} {
		for _, kind := range []string{"overload", "timeout"} {
			until, claim, err := cache.RecordAccountGroupFailure(ctx, 49, g, kind, revision, time.Minute, 2, time.Minute)
			require.NoError(t, err)
			require.False(t, claim)
			require.True(t, until.IsZero())
		}
	}
	until, claim, err := cache.RecordAccountGroupFailure(ctx, 49, 7, "timeout", revision, time.Minute, 2, time.Minute)
	require.NoError(t, err)
	require.True(t, claim)
	require.Equal(t, now.Add(time.Minute), until)
	require.NoError(t, cache.CompleteAccountOverloadCooldown(ctx, 49, until, true))
	other, otherClaim, err := cache.RecordAccountGroupFailure(ctx, 49, 8, "overload", revision, time.Minute, 2, 2*time.Minute)
	require.NoError(t, err)
	require.False(t, otherClaim)
	require.Equal(t, until, other)
	legacy, legacyClaim, err := cache.RecordAccountOverloadFailure(ctx, 49, time.Minute, 3, 2*time.Minute)
	require.NoError(t, err)
	require.False(t, legacyClaim)
	require.Equal(t, until, legacy)
	require.NoError(t, cache.ClearAccountGroupFailures(ctx, 49))
	require.NoError(t, cache.CompleteAccountOverloadCooldown(ctx, 49, until, true))
	after, claim, err := cache.RecordAccountGroupFailure(ctx, 49, 8, "overload", revision, time.Minute, 2, time.Minute)
	require.NoError(t, err)
	require.False(t, claim)
	require.True(t, after.IsZero())
}

func TestGroupFailureCacheRollingWindowRevisionAndOtherAccounts(t *testing.T) {
	server, cache, now := newAccountOverloadCacheTest(t)
	ctx := context.Background()
	call := func(account int64, revision string) time.Time {
		t.Helper()
		until, _, err := cache.RecordAccountGroupFailure(ctx, account, 7, "server_error", revision, time.Minute, 2, time.Minute)
		require.NoError(t, err)
		return until
	}
	require.True(t, call(49, "0123456789abcdef").IsZero())
	server.SetTime(now.Add(time.Minute))
	require.True(t, call(49, "0123456789abcdef").IsZero())
	require.True(t, call(49, "1123456789abcdef").IsZero())
	require.True(t, call(50, "0123456789abcdef").IsZero())
	require.False(t, call(49, "0123456789abcdef").IsZero())
	require.NoError(t, cache.ClearAccountGroupFailures(ctx, 49))
	require.False(t, call(50, "0123456789abcdef").IsZero(), "manual recovery affects only target account")
}

func TestGroupFailureCacheValidation(t *testing.T) {
	_, cache, _ := newAccountOverloadCacheTest(t)
	for _, kind := range []string{"credential", "safety", "unknown"} {
		_, _, err := cache.RecordAccountGroupFailure(context.Background(), 49, 7, kind, "0123456789abcdef", time.Minute, 2, time.Minute)
		require.Error(t, err)
	}
	_, _, err := cache.RecordAccountGroupFailure(context.Background(), 49, 7, "timeout", "bad", time.Minute, 2, time.Minute)
	require.Error(t, err)
	_, _, err = cache.RecordAccountGroupFailure(context.Background(), 49, 7, "timeout", "0123456789abcdef", time.Minute, 101, time.Minute)
	require.Error(t, err)
}

func TestGroupFailureCacheConcurrentTripLeaseAndFixedDeadline(t *testing.T) {
	server, cache, now := newAccountOverloadCacheTest(t)
	ctx := context.Background()
	type observation struct {
		until time.Time
		claim bool
		err   error
	}
	results := make(chan observation, 32)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			until, claim, err := cache.RecordAccountGroupFailure(ctx, 49, 7, "timeout", "0123456789abcdef", time.Minute, 3, time.Minute)
			results <- observation{until, claim, err}
		}()
	}
	wg.Wait()
	close(results)
	claims, below := 0, 0
	for result := range results {
		require.NoError(t, result.err)
		if result.claim {
			claims++
		}
		if result.until.IsZero() {
			below++
		} else {
			require.Equal(t, now.Add(time.Minute), result.until)
		}
	}
	require.Equal(t, 1, claims)
	require.Equal(t, 2, below)
	server.SetTime(now.Add(accountOverloadPersistLease + time.Millisecond))
	until, claim, err := cache.RecordAccountGroupFailure(ctx, 49, 8, "server_error", "0123456789abcdef", time.Minute, 3, 2*time.Minute)
	require.NoError(t, err)
	require.True(t, claim, "expired persistence lease can be reclaimed")
	require.Equal(t, now.Add(time.Minute), until, "another group must not extend cooldown")
	require.NoError(t, cache.CompleteAccountOverloadCooldown(ctx, 49, until, true))
	server.SetTime(now.Add(time.Minute))
	until, claim, err = cache.RecordAccountGroupFailure(ctx, 49, 7, "timeout", "0123456789abcdef", time.Minute, 3, time.Minute)
	require.NoError(t, err)
	require.False(t, claim)
	require.True(t, until.IsZero(), "expired cooldown starts a fresh window")
}
