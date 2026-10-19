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

func TestGroupFailureBreakerNeverPausesAccounts(t *testing.T) {
	rule := GroupFailureRule{Threshold: 1, WindowSeconds: 60, CooldownSeconds: 120}
	s, repo, ctx := newGroupBreakerTest(t, map[GroupFailureKind]GroupFailureRule{
		GroupFailureOverload: rule, GroupFailureTimeout: rule, GroupFailureServerError: rule, GroupFailureRateLimit: rule,
	})
	account := &Account{ID: 49, Platform: PlatformOpenAI}
	for _, callCtx := range []context.Context{nil, context.Background(), ctx, context.WithValue(ctx, ctxkey.KeyRoutingActive, true)} {
		for _, kind := range []GroupFailureKind{GroupFailureOverload, GroupFailureTimeout, GroupFailureServerError, GroupFailureRateLimit} {
			for attempt := 0; attempt < 5; attempt++ {
				handled, until := s.ApplyGroupFailurePolicy(callCtx, account, kind)
				require.False(t, handled, "native identity and quota handling must remain authoritative")
				require.True(t, until.IsZero())
			}
		}
	}
	require.Zero(t, repo.calls)
	require.Nil(t, account.OverloadUntil)
	require.False(t, s.IsAccountOverloadCooling(account.ID, time.Now()))
	require.Empty(t, s.groupFailures.entries)
}

func TestGroupFailureBreakerNoPolicyReadOrCounterWrite(t *testing.T) {
	s, repo, ctx := newGroupBreakerTest(t, map[GroupFailureKind]GroupFailureRule{})
	s.settingService.settingRepo = &groupBreakerSettingRepo{err: errors.New("settings unavailable")}
	s.tempUnschedCache = &groupFailureNoNewObservationCache{}
	var wg sync.WaitGroup
	for attempt := 0; attempt < 20; attempt++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.ApplyGroupFailurePolicy(ctx, &Account{ID: 49}, GroupFailureOverload)
			s.ApplyGroupFailurePolicyStatus(ctx, &Account{ID: 49}, 503)
		}()
	}
	wg.Wait()
	require.Zero(t, repo.calls)
	require.Empty(t, s.groupFailures.entries)
}

type groupFailureNoNewObservationCache struct{ TempUnschedCache }

func (*groupFailureNoNewObservationCache) RecordAccountGroupFailure(context.Context, int64, int64, string, string, time.Duration, int, time.Duration) (time.Time, bool, error) {
	panic("group routing rules must not observe account-wide failure counters")
}

func (*groupFailureNoNewObservationCache) ClearAccountGroupFailures(context.Context, int64) error {
	return nil
}

func TestGroupFailureLegacyCounterStateRemainsClearable(t *testing.T) {
	state := groupFailureLocalState{}
	now := time.Now()
	first := groupFailureLocalKey{accountID: 49, groupID: 7, kind: GroupFailureTimeout}
	second := groupFailureLocalKey{accountID: 50, groupID: 7, kind: GroupFailureTimeout}
	require.False(t, state.observe(first, now, time.Minute, 2))
	require.True(t, state.observe(first, now.Add(time.Millisecond), time.Minute, 2))
	require.False(t, state.observe(second, now, time.Minute, 2))
	state.clear(49)
	require.NotContains(t, state.entries, first)
	require.Contains(t, state.entries, second, "legacy cleanup must not remove another account's counters")
}

type groupFailureLegacyResetRepo struct {
	AccountRepository
	cleared []int64
}

func (*groupFailureLegacyResetRepo) ListByGroup(context.Context, int64) ([]Account, error) {
	return []Account{{ID: 49}, {ID: 0}}, nil
}

func (r *groupFailureLegacyResetRepo) ClearOverload(_ context.Context, id int64) error {
	r.cleared = append(r.cleared, id)
	return nil
}

func TestGroupFailureResetStillCleansLegacyAccountState(t *testing.T) {
	repo := &groupFailureLegacyResetRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, &groupFailureNoNewObservationCache{})
	now := time.Now()
	svc.overloadFailures.rememberCooldown(49, now, now.Add(time.Minute))
	legacy := groupFailureLocalKey{accountID: 49, groupID: 7, kind: GroupFailureOverload}
	svc.groupFailures.observe(legacy, now, time.Minute, 3)
	require.NoError(t, svc.ResetGroupFailurePolicy(context.Background(), 7))
	require.Equal(t, []int64{49}, repo.cleared)
	require.False(t, svc.IsAccountOverloadCooling(49, time.Now()))
	require.NotContains(t, svc.groupFailures.entries, legacy)
}
