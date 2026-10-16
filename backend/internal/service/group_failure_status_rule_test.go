package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func newStatusRuleBreakerTest(t *testing.T, rules []GroupFailureStatusRule) (*RateLimitService, *overloadBreakerRepo, context.Context) {
	t.Helper()
	data, err := json.Marshal(GroupFailurePolicy{StatusRules: rules})
	require.NoError(t, err)
	repo := &overloadBreakerRepo{}
	s := NewRateLimitService(repo, nil, nil, nil, nil)
	s.settingService = &SettingService{
		settingRepo:           &groupBreakerSettingRepo{value: string(data)},
		defaultSubGroupReader: &groupBreakerReader{},
	}
	return s, repo, context.WithValue(context.Background(), ctxkey.Group, &Group{ID: 7, Status: StatusActive, Hydrated: true})
}

func TestGroupFailureStatusRuleCountsCodesIndependently(t *testing.T) {
	s, repo, ctx := newStatusRuleBreakerTest(t, []GroupFailureStatusRule{{
		StatusCodes: []int{502, 503}, Threshold: 2, WindowSeconds: 60, CooldownSeconds: 120,
	}})
	account := &Account{ID: 49, Platform: PlatformOpenAI}

	handled, until := s.ApplyGroupFailurePolicyStatus(ctx, account, 502)
	require.True(t, handled)
	require.True(t, until.IsZero(), "one 502 must not reach a threshold of two")
	handled, until = s.ApplyGroupFailurePolicyStatus(ctx, account, 503)
	require.True(t, handled)
	require.True(t, until.IsZero(), "a 503 must use its own counter")
	handled, until = s.ApplyGroupFailurePolicyStatus(ctx, account, 502)
	require.True(t, handled)
	require.WithinDuration(t, time.Now().Add(2*time.Minute), until, time.Second)
	require.Equal(t, 1, repo.calls)
}

func TestGroupFailureStatusRuleIgnoresUnconfiguredStatus(t *testing.T) {
	s, repo, ctx := newStatusRuleBreakerTest(t, []GroupFailureStatusRule{{
		StatusCodes: []int{502}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 120,
	}})
	handled, until := s.ApplyGroupFailurePolicyStatus(ctx, &Account{ID: 50}, 503)
	require.False(t, handled)
	require.True(t, until.IsZero())
	require.Zero(t, repo.calls)
}
