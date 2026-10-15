//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type groupFailureRecoveryCache struct {
	tempUnschedCacheRecorder
	err    error
	clears int
	count  int
}

func (*groupFailureRecoveryCache) RecordAccountGroupFailure(context.Context, int64, int64, string, string, time.Duration, int, time.Duration) (time.Time, bool, error) {
	panic("unexpected failure observation")
}

func (c *groupFailureRecoveryCache) ClearAccountGroupFailures(context.Context, int64) error {
	c.clears++
	if c.err != nil {
		return c.err
	}
	c.count = 0
	return nil
}

func groupFailureRecoveryFixture(account *Account) (*RateLimitService, *rateLimitClearRepoStub, *groupFailureRecoveryCache, groupFailureLocalKey) {
	repo := &rateLimitClearRepoStub{getByIDAccount: account}
	cache := &groupFailureRecoveryCache{err: errors.New("redis unavailable"), count: 2}
	svc := NewRateLimitService(repo, nil, nil, nil, cache)
	now := time.Now()
	svc.overloadFailures.rememberCooldown(account.ID, now, now.Add(time.Minute))
	key := groupFailureLocalKey{accountID: account.ID, groupID: 7, kind: GroupFailureTimeout, revision: "0123456789abcdef"}
	svc.groupFailures.observe(key, now, time.Minute, 3)
	return svc, repo, cache, key
}

func TestGroupFailureRecoveryBestEffortPreservesExistingPaths(t *testing.T) {
	for _, path := range []string{"clear overload", "clear rate limit", "successful test", "quota recovery"} {
		t.Run(path, func(t *testing.T) {
			until := time.Now().Add(time.Minute)
			svc, repo, cache, key := groupFailureRecoveryFixture(&Account{ID: 49, Status: StatusError, OverloadUntil: &until})
			var err error
			switch path {
			case "clear overload":
				err = svc.ClearAccountOverload(context.Background(), 49)
			case "clear rate limit":
				err = svc.ClearRateLimit(context.Background(), 49)
			case "successful test":
				_, err = svc.RecoverAccountAfterSuccessfulTest(context.Background(), 49)
			case "quota recovery":
				_, err = svc.RecoverAccountState(context.Background(), 49, AccountRecoveryOptions{InvalidateToken: true})
			}
			require.NoError(t, err, "existing recovery must not depend on group-counter Redis availability")
			require.Equal(t, 1, repo.clearRateLimitCalls)
			require.Equal(t, 1, cache.clears)
			require.False(t, svc.IsAccountOverloadCooling(49, time.Now()))
			require.NotContains(t, svc.groupFailures.entries, key)
			if path != "clear overload" {
				require.Equal(t, 1, repo.clearAntigravityCalls)
				require.Equal(t, 1, repo.clearModelRateLimitCalls)
				require.Equal(t, 1, repo.clearTempUnschedCalls)
			}
		})
	}
}

func TestGroupFailureRecoveryManualFailureLeavesDatabaseAndLocalState(t *testing.T) {
	for _, status := range []string{StatusActive, StatusError} {
		t.Run(status, func(t *testing.T) {
			svc, repo, cache, key := groupFailureRecoveryFixture(&Account{ID: 49, Status: status})
			result, err := svc.RecoverAccountState(context.Background(), 49, AccountRecoveryOptions{RequireGroupCounterReset: true})
			require.ErrorIs(t, err, cache.err)
			require.Nil(t, result)
			require.Equal(t, 1, cache.clears)
			require.Equal(t, 2, cache.count)
			require.Zero(t, repo.clearErrorCalls)
			require.Zero(t, repo.clearRateLimitCalls)
			require.Zero(t, repo.clearAntigravityCalls)
			require.Zero(t, repo.clearModelRateLimitCalls)
			require.Zero(t, repo.clearTempUnschedCalls)
			require.True(t, svc.IsAccountOverloadCooling(49, time.Now()))
			require.Contains(t, svc.groupFailures.entries, key)
		})
	}
}

func TestGroupFailureRecoveryManualSuccessClearsCountersWithoutPersistedCooldown(t *testing.T) {
	svc, repo, cache, key := groupFailureRecoveryFixture(&Account{ID: 49, Status: StatusActive})
	cache.err = nil
	result, err := svc.RecoverAccountState(context.Background(), 49, AccountRecoveryOptions{RequireGroupCounterReset: true})
	require.NoError(t, err)
	require.True(t, result.ClearedRateLimit)
	require.False(t, result.ClearedError)
	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, cache.clears)
	require.Zero(t, cache.count)
	require.False(t, svc.IsAccountOverloadCooling(49, time.Now()))
	require.NotContains(t, svc.groupFailures.entries, key)
}
