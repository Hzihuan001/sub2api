package service

import (
	"context"
	"errors"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type groupFailurePolicySettingRepo struct {
	SettingRepository
	values            map[string]string
	readErr, writeErr error
	reads, writes     int
}

func (r *groupFailurePolicySettingRepo) GetValue(_ context.Context, key string) (string, error) {
	r.reads++
	if r.readErr != nil {
		return "", r.readErr
	}
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *groupFailurePolicySettingRepo) Set(_ context.Context, key, value string) error {
	r.writes++
	if r.writeErr != nil {
		return r.writeErr
	}
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	return nil
}

type groupFailurePolicyGroupReader struct {
	groups map[int64]*Group
	err    error
}

func (r *groupFailurePolicyGroupReader) GetByID(_ context.Context, id int64) (*Group, error) {
	if r.err != nil {
		return nil, r.err
	}
	group, ok := r.groups[id]
	if !ok {
		return nil, ErrGroupNotFound
	}
	return group, nil
}

func groupFailurePolicyTestService(repo *groupFailurePolicySettingRepo) *SettingService {
	svc := NewSettingService(repo, nil)
	svc.SetDefaultSubscriptionGroupReader(&groupFailurePolicyGroupReader{groups: map[int64]*Group{1: {ID: 1}, 2: {ID: 2}}})
	return svc
}

func TestGroupFailurePolicySettingsRoundTripAndIsolation(t *testing.T) {
	ctx := context.Background()
	repo := &groupFailurePolicySettingRepo{}
	svc := groupFailurePolicyTestService(repo)
	initial, err := svc.GetGroupFailurePolicy(ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, initial.Rules)
	require.Empty(t, initial.Rules, "unconfigured kinds inherit existing behavior")
	policy := &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{
		GroupFailureOverload: {Threshold: 3, WindowSeconds: 300, CooldownSeconds: 600},
		GroupFailureTimeout:  {Threshold: 2, WindowSeconds: 60, CooldownSeconds: 120},
	}}
	updated, err := svc.UpdateGroupFailurePolicy(ctx, 1, policy)
	require.NoError(t, err)
	require.Equal(t, policy, updated)
	require.Contains(t, repo.values, "custom.gateway-resilience.group-failure-policy.1")
	delete(updated.Rules, GroupFailureTimeout)
	stored, err := svc.GetGroupFailurePolicy(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, policy, stored, "returned maps do not mutate stored configuration")
	other, err := svc.GetGroupFailurePolicy(ctx, 2)
	require.NoError(t, err)
	require.Empty(t, other.Rules)
	_, err = svc.UpdateGroupFailurePolicy(ctx, 1, &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{GroupFailureRateLimit: {1, 1, 1}}})
	require.NoError(t, err)
	stored, err = svc.GetGroupFailurePolicy(ctx, 1)
	require.NoError(t, err)
	require.Len(t, stored.Rules, 1, "PUT replaces all rules")
	_, err = svc.UpdateGroupFailurePolicy(ctx, 1, &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{}})
	require.NoError(t, err)
	stored, err = svc.GetGroupFailurePolicy(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, stored.Rules)
	require.Equal(t, `{"rules":{}}`, repo.values[groupFailurePolicySettingPrefix+"1"])
}

func TestGroupFailurePolicyDeletionResetsRuntimeState(t *testing.T) {
	repo := &groupFailurePolicySettingRepo{}
	svc := groupFailurePolicyTestService(repo)
	var resetGroupID int64
	svc.SetGroupFailurePolicyResetter(func(_ context.Context, groupID int64) error {
		resetGroupID = groupID
		return nil
	})

	_, err := svc.UpdateGroupFailurePolicy(context.Background(), 1, &GroupFailurePolicy{StatusRules: []GroupFailureStatusRule{{
		StatusCodes: []int{502}, Threshold: 2, WindowSeconds: 60, CooldownSeconds: 600,
	}}})
	require.NoError(t, err)
	require.Zero(t, resetGroupID, "adding the first rule must not reset accounts")

	_, err = svc.UpdateGroupFailurePolicy(context.Background(), 1, &GroupFailurePolicy{StatusRules: []GroupFailureStatusRule{}})
	require.NoError(t, err)
	require.Equal(t, int64(1), resetGroupID)
}

func TestGroupFailurePolicyUpdateNotifiesRouteCacheImmediately(t *testing.T) {
	repo := &groupFailurePolicySettingRepo{}
	svc := groupFailurePolicyTestService(repo)
	var changed []int64
	svc.SetGroupFailurePolicyChangedListener(func(groupID int64) {
		changed = append(changed, groupID)
	})
	_, err := svc.UpdateGroupFailurePolicy(context.Background(), 1, &GroupFailurePolicy{StatusRules: []GroupFailureStatusRule{{
		StatusCodes: []int{502}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 60,
	}}})
	require.NoError(t, err)
	_, err = svc.UpdateGroupFailurePolicy(context.Background(), 1, &GroupFailurePolicy{StatusRules: []GroupFailureStatusRule{}})
	require.NoError(t, err)
	require.Equal(t, []int64{1, 1}, changed)
}

func TestGroupFailurePolicySettingsValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy *GroupFailurePolicy
	}{
		{"nil", nil}, {"missing rules", &GroupFailurePolicy{}},
		{"unknown kind", &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{"credential": {1, 1, 1}}}},
		{"zero threshold", &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{GroupFailureOverload: {0, 1, 1}}}},
		{"high threshold", &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{GroupFailureOverload: {101, 1, 1}}}},
		{"zero window", &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{GroupFailureOverload: {1, 0, 1}}}},
		{"high window", &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{GroupFailureOverload: {1, 3601, 1}}}},
		{"negative cooldown", &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{GroupFailureOverload: {1, 1, -1}}}},
		{"high cooldown", &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{GroupFailureOverload: {1, 1, 7201}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &groupFailurePolicySettingRepo{}
			_, err := groupFailurePolicyTestService(repo).UpdateGroupFailurePolicy(context.Background(), 1, tc.policy)
			require.Equal(t, 400, infraerrors.Code(err))
			require.Zero(t, repo.writes)
		})
	}
	for _, rule := range []GroupFailureRule{{1, 1, 1}, {100, 3600, 7200}} {
		_, err := groupFailurePolicyTestService(&groupFailurePolicySettingRepo{}).UpdateGroupFailurePolicy(context.Background(), 1, &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{GroupFailureServerError: rule}})
		require.NoError(t, err)
	}
}

func TestGroupFailurePolicySettingsDoesNotHideFailures(t *testing.T) {
	ctx := context.Background()
	policy := &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{}}
	t.Run("missing group", func(t *testing.T) {
		repo := &groupFailurePolicySettingRepo{}
		svc := groupFailurePolicyTestService(repo)
		_, err := svc.GetGroupFailurePolicy(ctx, 99)
		require.ErrorIs(t, err, ErrGroupNotFound)
		_, err = svc.UpdateGroupFailurePolicy(ctx, 99, policy)
		require.ErrorIs(t, err, ErrGroupNotFound)
		require.Zero(t, repo.reads)
		require.Zero(t, repo.writes)
	})
	t.Run("group lookup error", func(t *testing.T) {
		errDB := errors.New("group lookup unavailable")
		svc := groupFailurePolicyTestService(&groupFailurePolicySettingRepo{})
		svc.SetDefaultSubscriptionGroupReader(&groupFailurePolicyGroupReader{err: errDB})
		_, err := svc.GetGroupFailurePolicy(ctx, 1)
		require.ErrorIs(t, err, errDB)
	})
	t.Run("missing dependencies", func(t *testing.T) {
		for _, svc := range []*SettingService{nil, {}, NewSettingService(&groupFailurePolicySettingRepo{}, nil)} {
			_, err := svc.GetGroupFailurePolicy(ctx, 1)
			require.Equal(t, 500, infraerrors.Code(err))
			_, err = svc.UpdateGroupFailurePolicy(ctx, 1, policy)
			require.Equal(t, 500, infraerrors.Code(err))
		}
	})
	t.Run("storage errors", func(t *testing.T) {
		errDB := errors.New("database unavailable")
		svc := groupFailurePolicyTestService(&groupFailurePolicySettingRepo{readErr: errDB, writeErr: errDB})
		_, err := svc.GetGroupFailurePolicy(ctx, 1)
		require.ErrorIs(t, err, errDB)
		_, err = svc.UpdateGroupFailurePolicy(ctx, 1, policy)
		require.ErrorIs(t, err, errDB)
	})
	for _, raw := range []string{"", "null", "{}", `{"rules":null}`, `{"rules":{},"enabled":true}`, `{"rules":{"overload":{"threshold":1,"window_seconds":1,"cooldown_seconds":1,"extra":1}}}`, `{"rules":{"future":{"threshold":1,"window_seconds":1,"cooldown_seconds":1}}}`, `{"rules":{"overload":{"threshold":0,"window_seconds":1,"cooldown_seconds":1}}}`, `{"rules":{}} {"rules":{}}`} {
		t.Run("stored "+raw, func(t *testing.T) {
			svc := groupFailurePolicyTestService(&groupFailurePolicySettingRepo{values: map[string]string{groupFailurePolicySettingPrefix + "1": raw}})
			_, err := svc.GetGroupFailurePolicy(ctx, 1)
			require.Equal(t, 500, infraerrors.Code(err))
		})
	}
}

func TestGroupFailureRuleRevisionChangesWithEachValue(t *testing.T) {
	base := GroupFailureRule{3, 300, 600}
	require.Len(t, base.Revision(), 16)
	require.Equal(t, base.Revision(), (GroupFailureRule{3, 300, 600}).Revision())
	for _, changed := range []GroupFailureRule{{4, 300, 600}, {3, 301, 600}, {3, 300, 601}} {
		require.NotEqual(t, base.Revision(), changed.Revision())
	}
}
