package service

import (
	"context"
	"encoding/json"
	"testing"

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

func TestGroupFailureStatusRulesNeverWriteAccountCooldown(t *testing.T) {
	s, repo, ctx := newStatusRuleBreakerTest(t, []GroupFailureStatusRule{{
		StatusCodes: []int{502, 503}, Threshold: 2, WindowSeconds: 60, CooldownSeconds: 120,
	}})
	account := &Account{ID: 49, Platform: PlatformOpenAI}

	for attempt := 0; attempt < 5; attempt++ {
		for _, status := range []int{502, 503} {
			handled, until := s.ApplyGroupFailurePolicyStatus(ctx, account, status)
			require.False(t, handled)
			require.True(t, until.IsZero())
		}
	}
	require.Zero(t, repo.calls)
	require.Empty(t, s.groupFailures.entries)
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
