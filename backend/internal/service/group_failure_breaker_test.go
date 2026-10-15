package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type groupBreakerSettingRepo struct {
	SettingRepository
	value string
	err   error
}

func (r *groupBreakerSettingRepo) GetValue(context.Context, string) (string, error) {
	return r.value, r.err
}

type groupBreakerReader struct{ GroupRepository }

func (*groupBreakerReader) GetByID(_ context.Context, id int64) (*Group, error) {
	return &Group{ID: id, Status: StatusActive, Hydrated: true}, nil
}

func newGroupBreakerTest(t *testing.T, rules map[GroupFailureKind]GroupFailureRule) (*RateLimitService, *overloadBreakerRepo, context.Context) {
	t.Helper()
	data, err := json.Marshal(GroupFailurePolicy{Rules: rules})
	require.NoError(t, err)
	repo := &overloadBreakerRepo{}
	s := NewRateLimitService(repo, nil, nil, nil, nil)
	s.settingService = &SettingService{settingRepo: &groupBreakerSettingRepo{value: string(data)}, defaultSubGroupReader: &groupBreakerReader{}}
	return s, repo, context.WithValue(context.Background(), ctxkey.Group, &Group{ID: 7, Status: StatusActive, Hydrated: true})
}

func TestGroupFailureBreakerThresholdRecoveryAndAccountWideCooldown(t *testing.T) {
	rule := GroupFailureRule{Threshold: 2, WindowSeconds: 60, CooldownSeconds: 120}
	s, repo, ctx := newGroupBreakerTest(t, map[GroupFailureKind]GroupFailureRule{GroupFailureOverload: rule})
	account := &Account{ID: 49, Platform: PlatformOpenAI}
	handled, until := s.ApplyGroupFailurePolicy(ctx, account, GroupFailureOverload)
	require.True(t, handled)
	require.True(t, until.IsZero())
	require.Zero(t, repo.calls)
	_, until = s.ApplyGroupFailurePolicy(ctx, account, GroupFailureOverload)
	require.WithinDuration(t, time.Now().Add(2*time.Minute), until, time.Second)
	require.True(t, s.IsAccountOverloadCooling(49, time.Now()))
	otherGroup := context.WithValue(ctx, ctxkey.Group, &Group{ID: 8})
	_, otherUntil := s.ApplyGroupFailurePolicy(otherGroup, account, GroupFailureOverload)
	require.Equal(t, until, otherUntil)
	require.Equal(t, 1, repo.calls)
	require.Nil(t, account.OverloadUntil, "never mutate shared scheduling snapshots")
	require.NoError(t, s.ClearAccountOverload(ctx, 49))
	require.False(t, s.IsAccountOverloadCooling(49, time.Now()))
	_, until = s.ApplyGroupFailurePolicy(ctx, account, GroupFailureOverload)
	require.True(t, until.IsZero(), "manual recovery starts a fresh count")
}

func TestGroupFailureBreakerKindsGroupsAndRuleVersionsAreIndependent(t *testing.T) {
	rule := GroupFailureRule{Threshold: 3, WindowSeconds: 60, CooldownSeconds: 60}
	s, repo, ctx := newGroupBreakerTest(t, map[GroupFailureKind]GroupFailureRule{GroupFailureOverload: rule, GroupFailureTimeout: rule})
	a := &Account{ID: 49}
	for _, g := range []int64{7, 8} {
		for _, k := range []GroupFailureKind{GroupFailureOverload, GroupFailureTimeout} {
			c := context.WithValue(ctx, ctxkey.Group, &Group{ID: g})
			for i := 0; i < 2; i++ {
				handled, until := s.ApplyGroupFailurePolicy(c, a, k)
				require.True(t, handled)
				require.True(t, until.IsZero())
			}
		}
	}
	require.Zero(t, repo.calls)
	_, until := s.ApplyGroupFailurePolicy(ctx, a, GroupFailureTimeout)
	require.False(t, until.IsZero())
	require.Equal(t, 1, repo.calls)
	state := groupFailureLocalState{}
	key := groupFailureLocalKey{accountID: 49, groupID: 7, kind: GroupFailureTimeout, revision: rule.Revision()}
	now := time.Now()
	require.False(t, state.observe(key, now, time.Minute, 3))
	require.False(t, state.observe(key, now, time.Minute, 3))
	rule.Threshold = 2
	key.revision = rule.Revision()
	require.False(t, state.observe(key, now, time.Minute, 2), "new rule must not reuse old counters")
	require.True(t, state.observe(key, now, time.Minute, 2))
}

func TestGroupFailureBreakerWindowBoundaryAndConcurrency(t *testing.T) {
	state := groupFailureLocalState{}
	now := time.Now()
	key := groupFailureLocalKey{accountID: 49}
	require.False(t, state.observe(key, now, time.Minute, 2))
	require.False(t, state.observe(key, now.Add(time.Minute), time.Minute, 2))
	require.True(t, state.observe(key, now.Add(time.Minute+time.Millisecond), time.Minute, 2))
	s, repo, ctx := newGroupBreakerTest(t, map[GroupFailureKind]GroupFailureRule{GroupFailureServerError: {Threshold: 3, WindowSeconds: 60, CooldownSeconds: 60}})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.ApplyGroupFailurePolicy(ctx, &Account{ID: 49}, GroupFailureServerError) }()
	}
	wg.Wait()
	require.Equal(t, 1, repo.calls)
}

func TestGroupFailureBreakerInheritedAndMissingConfiguration(t *testing.T) {
	s, repo, ctx := newGroupBreakerTest(t, map[GroupFailureKind]GroupFailureRule{})
	for _, c := range []context.Context{nil, context.Background(), ctx} {
		handled, until := s.ApplyGroupFailurePolicy(c, &Account{ID: 49}, GroupFailureOverload)
		require.False(t, handled)
		require.True(t, until.IsZero())
	}
	s.settingService.settingRepo = &groupBreakerSettingRepo{err: errors.New("offline")}
	handled, until := s.ApplyGroupFailurePolicy(ctx, &Account{ID: 49}, GroupFailureTimeout)
	require.False(t, handled)
	require.True(t, until.IsZero())
	require.Zero(t, repo.calls)
}

type groupBreakerCacheStub struct {
	TempUnschedCache
	recordErr, clearErr error
	count               int
	until               time.Time
}

func (c *groupBreakerCacheStub) RecordAccountGroupFailure(_ context.Context, _, _ int64, _, _ string, _ time.Duration, threshold int, cooldown time.Duration) (time.Time, bool, error) {
	if c.recordErr != nil {
		return time.Time{}, false, c.recordErr
	}
	c.count++
	if c.count < threshold {
		return time.Time{}, false, nil
	}
	c.until = time.Now().Add(cooldown).Truncate(time.Millisecond)
	return c.until, true, nil
}
func (c *groupBreakerCacheStub) ClearAccountGroupFailures(context.Context, int64) error {
	if c.clearErr != nil {
		return c.clearErr
	}
	c.count = 0
	c.until = time.Time{}
	return nil
}

func TestGroupFailureBreakerRedisFallbackAndVisibleRecoveryFailure(t *testing.T) {
	s, repo, ctx := newGroupBreakerTest(t, map[GroupFailureKind]GroupFailureRule{GroupFailureOverload: {Threshold: 3, WindowSeconds: 60, CooldownSeconds: 60}})
	cache := &groupBreakerCacheStub{}
	s.tempUnschedCache = cache
	s.ApplyGroupFailurePolicy(ctx, &Account{ID: 49}, GroupFailureOverload)
	s.ApplyGroupFailurePolicy(ctx, &Account{ID: 49}, GroupFailureOverload)
	cache.recordErr = errors.New("redis unavailable")
	_, until := s.ApplyGroupFailurePolicy(ctx, &Account{ID: 49}, GroupFailureOverload)
	require.False(t, until.IsZero())
	require.Equal(t, 1, repo.calls)
	cache.clearErr = errors.New("redis unavailable")
	require.Error(t, s.clearAccountOverload(ctx, 49, true))
	require.True(t, s.IsAccountOverloadCooling(49, time.Now()), "failed recovery must not pretend to resume")
	cache.clearErr = nil
	require.NoError(t, s.clearAccountOverload(ctx, 49, true))
	require.False(t, s.IsAccountOverloadCooling(49, time.Now()))
}

func TestGroupFailureBreakerPersistenceRetryDoesNotExtendDeadline(t *testing.T) {
	s, repo, ctx := newGroupBreakerTest(t, map[GroupFailureKind]GroupFailureRule{GroupFailureTimeout: {Threshold: 1, WindowSeconds: 60, CooldownSeconds: 60}})
	repo.failures = 1
	_, first := s.ApplyGroupFailurePolicy(ctx, &Account{ID: 49}, GroupFailureTimeout)
	_, second := s.ApplyGroupFailurePolicy(ctx, &Account{ID: 49}, GroupFailureTimeout)
	require.Equal(t, first, second)
	require.Equal(t, 2, repo.calls)
	_, third := s.ApplyGroupFailurePolicy(ctx, &Account{ID: 49}, GroupFailureTimeout)
	require.Equal(t, first, third)
	require.Equal(t, 2, repo.calls)
}

func TestGroupFailureBreakerIntermittentRedisRetainsFallbackEvidence(t *testing.T) {
	s, repo, ctx := newGroupBreakerTest(t, map[GroupFailureKind]GroupFailureRule{GroupFailureOverload: {Threshold: 3, WindowSeconds: 60, CooldownSeconds: 60}})
	cache := &groupBreakerCacheStub{recordErr: errors.New("redis unavailable")}
	s.tempUnschedCache = cache
	account := &Account{ID: 49}
	for i := 0; i < 2; i++ {
		_, until := s.ApplyGroupFailurePolicy(ctx, account, GroupFailureOverload)
		require.True(t, until.IsZero())
	}
	cache.recordErr = nil
	_, until := s.ApplyGroupFailurePolicy(ctx, account, GroupFailureOverload)
	require.True(t, until.IsZero(), "healthy Redis remains authoritative")
	cache.recordErr = errors.New("redis unavailable again")
	_, until = s.ApplyGroupFailurePolicy(ctx, account, GroupFailureOverload)
	require.False(t, until.IsZero(), "intermittent Redis must not discard observed failures")
	require.Equal(t, 1, repo.calls)
}

func TestGroupFailureBreakerFullFallbackDoesNotInventNewDeadlines(t *testing.T) {
	s, repo, ctx := newGroupBreakerTest(t, map[GroupFailureKind]GroupFailureRule{GroupFailureTimeout: {Threshold: 1, WindowSeconds: 60, CooldownSeconds: 60}})
	now := time.Now()
	for id := int64(1); id <= accountOverloadLocalLimit; id++ {
		s.overloadFailures.rememberCooldown(id, now, now.Add(time.Minute))
	}
	for i := 0; i < 3; i++ {
		handled, until := s.ApplyGroupFailurePolicy(ctx, &Account{ID: accountOverloadLocalLimit + 1}, GroupFailureTimeout)
		require.True(t, handled)
		require.True(t, until.IsZero())
	}
	require.Zero(t, repo.calls, "bounded fallback must not keep rewriting new deadlines")
	require.Len(t, s.overloadFailures.entries, accountOverloadLocalLimit)
}
