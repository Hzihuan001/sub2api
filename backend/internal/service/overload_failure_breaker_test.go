package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type overloadBreakerRepo struct {
	AccountRepository
	mu         sync.Mutex
	calls      int
	failures   int
	until      time.Time
	contextErr error
	clearErr   error
	clears     int
	operations []string
}

func (r *overloadBreakerRepo) SetOverloaded(ctx context.Context, _ int64, until time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.operations = append(r.operations, "set")
	r.until = until
	r.contextErr = ctx.Err()
	if r.failures > 0 {
		r.failures--
		return errors.New("database unavailable")
	}
	return nil
}
func (r *overloadBreakerRepo) ClearRateLimit(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.clearErr != nil {
		return r.clearErr
	}
	r.clears++
	r.until = time.Time{}
	r.operations = append(r.operations, "clear")
	return nil
}
func (r *overloadBreakerRepo) ClearAntigravityQuotaScopes(context.Context, int64) error { return nil }
func (r *overloadBreakerRepo) ClearModelRateLimits(context.Context, int64) error        { return nil }
func (r *overloadBreakerRepo) ClearTempUnschedulable(context.Context, int64) error      { return nil }

type overloadBreakerBlocker struct {
	mu    sync.Mutex
	until time.Time
	calls int
}

func (b *overloadBreakerBlocker) BlockAccountScheduling(_ *Account, until time.Time, _ string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.until = until
	b.calls++
}
func (b *overloadBreakerBlocker) ClearAccountSchedulingBlock(int64) {}

type overloadBreakerSettings struct {
	SettingRepository
	value string
	err   error
}

func (r *overloadBreakerSettings) GetValue(context.Context, string) (string, error) {
	return r.value, r.err
}

type overloadBreakerCache struct {
	TempUnschedCache
	count     int
	until     time.Time
	persisted bool
	recordErr error
	ackErr    error
	clearErr  error
	clears    int
}

func (c *overloadBreakerCache) RecordAccountOverloadFailure(_ context.Context, _ int64, _ time.Duration, threshold int, cooldown time.Duration) (time.Time, bool, error) {
	if c.recordErr != nil {
		return time.Time{}, false, c.recordErr
	}
	c.count++
	if c.count < threshold {
		return time.Time{}, false, nil
	}
	if c.until.IsZero() {
		c.until = time.Now().Add(cooldown).Truncate(time.Millisecond)
	}
	return c.until, !c.persisted, nil
}
func (c *overloadBreakerCache) CompleteAccountOverloadCooldown(_ context.Context, _ int64, _ time.Time, persisted bool) error {
	if c.ackErr != nil {
		return c.ackErr
	}
	c.persisted = persisted
	return nil
}
func (c *overloadBreakerCache) ClearAccountOverloadFailures(context.Context, int64) error {
	c.clears++
	if c.clearErr != nil {
		return c.clearErr
	}
	c.count, c.until, c.persisted = 0, time.Time{}, false
	return nil
}
func (c *overloadBreakerCache) DeleteTempUnsched(context.Context, int64) error { return nil }

func TestAccountOverloadBreakerThirdFailureCoolsWithoutExtendingOrMutatingAccount(t *testing.T) {
	repo := &overloadBreakerRepo{}
	blocker := &overloadBreakerBlocker{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	svc.SetAccountRuntimeBlocker(blocker)
	account := &Account{ID: 49, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for i := 0; i < 2; i++ {
		require.True(t, svc.RecordAccountOverload(context.Background(), account).IsZero())
	}
	require.Zero(t, repo.calls)
	until := svc.RecordAccountOverload(context.Background(), account)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), until, time.Second)
	require.True(t, svc.IsAccountOverloadCooling(account.ID, time.Now()))
	for i := 0; i < 5; i++ {
		require.Equal(t, until, svc.RecordAccountOverload(context.Background(), account))
	}
	require.Equal(t, 1, repo.calls)
	require.Equal(t, until, blocker.until)
	require.Nil(t, account.OverloadUntil)
	require.False(t, svc.IsAccountOverloadCooling(account.ID, until))
}

func TestAccountOverloadBreakerConcurrentFailuresPersistOnce(t *testing.T) {
	repo := &overloadBreakerRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	svc.SetAccountRuntimeBlocker(&overloadBreakerBlocker{})
	account := &Account{ID: 49}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); svc.RecordAccountOverload(context.Background(), account) }()
	}
	wg.Wait()
	require.Equal(t, 1, repo.calls)
	require.True(t, svc.IsAccountOverloadCooling(account.ID, time.Now()))
}

func TestAccountOverloadBreakerPersistenceFailureCanBeRetried(t *testing.T) {
	repo := &overloadBreakerRepo{failures: 1}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	blocker := &overloadBreakerBlocker{}
	svc.SetAccountRuntimeBlocker(blocker)
	account := &Account{ID: 49}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 2; i++ {
		svc.RecordAccountOverload(ctx, account)
	}
	until := svc.RecordAccountOverload(ctx, account)
	require.False(t, until.IsZero())
	require.Equal(t, 1, repo.calls)
	require.True(t, svc.IsAccountOverloadCooling(account.ID, time.Now()), "DB failure must not remove the immediate block")
	require.Equal(t, until, blocker.until)
	require.Equal(t, until, svc.RecordAccountOverload(ctx, account))
	require.Equal(t, 2, repo.calls)
	require.NoError(t, repo.contextErr)
	require.Equal(t, until, svc.RecordAccountOverload(ctx, account))
	require.Equal(t, 2, repo.calls)
}

func TestAccountOverloadBreakerSharedCacheAndLostAcknowledgement(t *testing.T) {
	repo := &overloadBreakerRepo{}
	cache := &overloadBreakerCache{ackErr: errors.New("redis unavailable")}
	svc := NewRateLimitService(repo, nil, nil, nil, cache)
	account := &Account{ID: 49}
	for i := 0; i < 2; i++ {
		require.True(t, svc.RecordAccountOverload(context.Background(), account).IsZero())
	}
	until := svc.RecordAccountOverload(context.Background(), account)
	require.False(t, until.IsZero())
	require.True(t, svc.IsAccountOverloadCooling(account.ID, time.Now()))
	cache.ackErr = nil
	require.Equal(t, until, svc.RecordAccountOverload(context.Background(), account))
	require.True(t, cache.persisted)
	require.Equal(t, 1, repo.calls, "lost Redis ACK must not repeat a successful local DB write")
}

func TestAccountOverloadBreakerSharedCooldownBlocksWithoutAnotherDatabaseWrite(t *testing.T) {
	repo := &overloadBreakerRepo{}
	until := time.Now().Add(time.Minute).Truncate(time.Millisecond)
	cache := &overloadBreakerCache{count: 3, until: until, persisted: true}
	svc := NewRateLimitService(repo, nil, nil, nil, cache)
	blocker := &overloadBreakerBlocker{}
	svc.SetAccountRuntimeBlocker(blocker)
	require.Equal(t, until, svc.RecordAccountOverload(context.Background(), &Account{ID: 49}))
	require.True(t, svc.IsAccountOverloadCooling(49, time.Now()))
	require.Equal(t, until, blocker.until)
	require.Zero(t, repo.calls)
}

func TestAccountOverloadBreakerCacheFailureUsesPreviousLocalObservations(t *testing.T) {
	repo := &overloadBreakerRepo{}
	cache := &overloadBreakerCache{}
	svc := NewRateLimitService(repo, nil, nil, nil, cache)
	account := &Account{ID: 49}
	for i := 0; i < 2; i++ {
		require.True(t, svc.RecordAccountOverload(context.Background(), account).IsZero())
	}
	cache.recordErr = errors.New("redis unavailable")
	until := svc.RecordAccountOverload(context.Background(), account)
	require.False(t, until.IsZero())
	require.Equal(t, 1, repo.calls)
	cache.recordErr = nil
	cache.count = 0 // Redis restarted without the in-flight local cooldown.
	for i := 0; i < 6; i++ {
		require.Equal(t, until, svc.RecordAccountOverload(context.Background(), account), "cache recovery must not reopen retries or extend a local cooldown")
	}
	require.Zero(t, cache.count)
	require.Equal(t, 1, repo.calls)
}

func TestAccountOverloadBreakerSettingsAndNilDependencies(t *testing.T) {
	var absent *RateLimitService
	require.True(t, absent.RecordAccountOverload(nil, &Account{ID: 1}).IsZero())
	require.False(t, absent.IsAccountOverloadCooling(1, time.Now()))
	require.NoError(t, absent.ClearAccountOverload(nil, 1))
	for _, tc := range []struct {
		name, value string
		readErr     error
		wantMinutes int
	}{
		{name: "disabled", value: `{"enabled":false,"cooldown_minutes":15}`},
		{name: "configured", value: `{"enabled":true,"cooldown_minutes":15}`, wantMinutes: 15},
		{name: "read failure uses config", readErr: errors.New("settings unavailable"), wantMinutes: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &overloadBreakerRepo{}
			cfg := &config.Config{}
			cfg.RateLimit.OverloadCooldownMinutes = 7
			cache := &overloadBreakerCache{}
			svc := NewRateLimitService(repo, nil, cfg, nil, cache)
			svc.SetSettingService(&SettingService{settingRepo: &overloadBreakerSettings{value: tc.value, err: tc.readErr}})
			var until time.Time
			for i := 0; i < 3; i++ {
				until = svc.RecordAccountOverload(nil, &Account{ID: 1})
			}
			if tc.wantMinutes == 0 {
				require.True(t, until.IsZero())
				require.Zero(t, repo.calls)
				require.Zero(t, cache.count)
				require.Empty(t, svc.overloadFailures.entries)
			} else {
				require.WithinDuration(t, time.Now().Add(time.Duration(tc.wantMinutes)*time.Minute), until, time.Second)
			}
		})
	}
	svc := &RateLimitService{}
	require.True(t, svc.RecordAccountOverload(nil, nil).IsZero())
	require.True(t, svc.RecordAccountOverload(nil, &Account{}).IsZero())
	for i := 0; i < 3; i++ {
		svc.RecordAccountOverload(nil, &Account{ID: 1})
	}
	require.True(t, svc.IsAccountOverloadCooling(1, time.Now()), "missing repository is safe and retains local cooldown")
}

func TestAccountOverloadLocalWindowExpiryAndBoundedStorage(t *testing.T) {
	var state accountOverloadLocalState
	now := time.Now().Truncate(time.Millisecond)
	until, _ := state.record(1, now, time.Minute)
	require.True(t, until.IsZero())
	until, _ = state.record(1, now.Add(4*time.Minute), time.Minute)
	require.True(t, until.IsZero())
	until, _ = state.record(1, now.Add(5*time.Minute), time.Minute)
	require.True(t, until.IsZero(), "failure exactly on left boundary expires")
	until, claim := state.record(1, now.Add(5*time.Minute+time.Second), time.Minute)
	require.True(t, claim)
	state.complete(1, until, true)
	next, _ := state.record(1, until, time.Minute)
	require.True(t, next.IsZero(), "expired cooldown starts a new window")
	for id := int64(2); id <= accountOverloadLocalLimit+100; id++ {
		state.record(id, now, time.Minute)
	}
	require.LessOrEqual(t, len(state.entries), accountOverloadLocalLimit)
}

func TestAccountOverloadBreakerExplicitClearAfterDatabaseRecovery(t *testing.T) {
	repo := &overloadBreakerRepo{}
	cache := &overloadBreakerCache{}
	svc := NewRateLimitService(repo, nil, nil, nil, cache)
	account := &Account{ID: 49}
	for i := 0; i < 3; i++ {
		svc.RecordAccountOverload(context.Background(), account)
	}
	repo.clearErr = errors.New("database unavailable")
	require.Error(t, svc.ClearRateLimit(context.Background(), account.ID))
	require.True(t, svc.IsAccountOverloadCooling(account.ID, time.Now()))
	require.Zero(t, cache.clears)
	repo.clearErr = nil
	require.NoError(t, svc.ClearRateLimit(context.Background(), account.ID))
	require.False(t, svc.IsAccountOverloadCooling(account.ID, time.Now()))
	require.Equal(t, 1, cache.clears)
	require.True(t, svc.RecordAccountOverload(context.Background(), account).IsZero())
}

type overloadBreakerClaimBarrierCache struct {
	overloadBreakerCache
	claimed chan struct{}
	release chan struct{}
}

func (c *overloadBreakerClaimBarrierCache) RecordAccountOverloadFailure(ctx context.Context, accountID int64, window time.Duration, threshold int, cooldown time.Duration) (time.Time, bool, error) {
	until, claim, err := c.overloadBreakerCache.RecordAccountOverloadFailure(ctx, accountID, window, threshold, cooldown)
	close(c.claimed)
	<-c.release
	return until, claim, err
}

func TestAccountOverloadBreakerExplicitClearWaitsForInFlightClaim(t *testing.T) {
	for _, direct := range []bool{false, true} {
		name := "ClearRateLimit"
		if direct {
			name = "ClearAccountOverload"
		}
		t.Run(name, func(t *testing.T) {
			repo := &overloadBreakerRepo{}
			cache := &overloadBreakerClaimBarrierCache{
				overloadBreakerCache: overloadBreakerCache{count: 2},
				claimed:              make(chan struct{}), release: make(chan struct{}),
			}
			svc := NewRateLimitService(repo, nil, nil, nil, cache)
			recordDone := make(chan time.Time, 1)
			go func() { recordDone <- svc.RecordAccountOverload(context.Background(), &Account{ID: 49}) }()
			<-cache.claimed // Redis has returned a persistence claim; local/DB writes have not started.
			clearStarted := make(chan struct{})
			clearDone := make(chan error, 1)
			go func() {
				close(clearStarted)
				if direct {
					clearDone <- svc.ClearAccountOverload(context.Background(), 49)
				} else {
					clearDone <- svc.ClearRateLimit(context.Background(), 49)
				}
			}()
			<-clearStarted
			select {
			case err := <-clearDone:
				close(cache.release)
				<-recordDone
				t.Fatalf("recovery overtook an in-flight overload claim: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			close(cache.release)
			require.False(t, (<-recordDone).IsZero())
			require.NoError(t, <-clearDone)
			require.Equal(t, []string{"set", "clear"}, repo.operations)
			require.Equal(t, 1, repo.clears, "recovery must not clear the database twice")
			require.True(t, repo.until.IsZero())
			require.True(t, cache.until.IsZero())
			require.False(t, svc.IsAccountOverloadCooling(49, time.Now()))
			require.NotContains(t, svc.overloadFailures.entries, int64(49))
		})
	}
}
