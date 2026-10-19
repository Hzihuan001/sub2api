//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/kirocooldown"
	"github.com/stretchr/testify/require"
)

type groupNativeSchedulingRepo struct {
	*mockAccountRepoForPlatform
	overloadWrites int
	recoveries     int
}

func (r *groupNativeSchedulingRepo) SetOverloaded(context.Context, int64, time.Time) error {
	r.overloadWrites++
	return errors.New("overload persistence unavailable")
}

func (r *groupNativeSchedulingRepo) ClearRateLimit(context.Context, int64) error {
	r.recoveries++
	return nil
}

func newGroupNativeSchedulingFixture(t *testing.T, platform string) (context.Context, *GatewayService, *groupNativeSchedulingRepo, *groupFailurePolicySettingRepo) {
	t.Helper()
	settingsRepo := &groupFailurePolicySettingRepo{}
	settings := groupFailurePolicyTestService(settingsRepo)
	_, err := settings.UpdateGroupFailurePolicy(context.Background(), 1, &GroupFailurePolicy{Rules: groupRuntimeRules(1)})
	require.NoError(t, err)
	repo := &groupNativeSchedulingRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
		accounts: []Account{
			{ID: 901, Platform: platform, Type: AccountTypeOAuth, Priority: 0, Status: StatusActive, Schedulable: true},
			{ID: 902, Platform: platform, Type: AccountTypeOAuth, Priority: 1, Status: StatusActive, Schedulable: true},
		},
		accountsByID: map[int64]*Account{},
	}}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
	}
	limiter := NewRateLimitService(repo, nil, testConfig(), nil, nil)
	limiter.SetSettingService(settings)
	svc := &GatewayService{accountRepo: repo, cfg: testConfig(), rateLimitService: limiter,
		cache: &mockGatewayCacheForPlatform{sessionBindings: map[string]int64{"sticky": 901}},
	}
	return context.WithValue(context.Background(), ctxkey.Group, &Group{ID: 1}), svc, repo, settingsRepo
}

func TestGroupFailureNativeSelectionIsNotChangedByRoutingRules(t *testing.T) {
	for _, provider := range []struct{ platform, model string }{
		{PlatformAnthropic, "claude-sonnet-4-5"},
		{PlatformGemini, "gemini-2.5-pro"},
	} {
		for _, session := range []string{"", "sticky"} {
			t.Run(provider.platform+"/"+session, func(t *testing.T) {
				ctx, svc, repo, settingsRepo := newGroupNativeSchedulingFixture(t, provider.platform)
				primary := repo.accountsByID[901]
				selected, err := svc.selectAccountForModelWithPlatform(ctx, nil, session, provider.model, nil, provider.platform)
				require.NoError(t, err)
				require.Equal(t, primary.ID, selected.ID)

				require.False(t, svc.rateLimitService.HandleUpstreamError(ctx, primary, http.StatusServiceUnavailable, nil, []byte(`{"error":{"message":"upstream unavailable"}}`)))
				require.Zero(t, repo.overloadWrites)
				require.Nil(t, primary.OverloadUntil)
				require.True(t, primary.IsSchedulable())
				failureReads := settingsRepo.reads

				// Group routing evidence must not leak into another key or group
				// through account-wide native scheduling state.
				otherGroup := context.WithValue(context.Background(), ctxkey.Group, &Group{ID: 2})
				selected, err = svc.selectAccountForModelWithPlatform(otherGroup, nil, session, provider.model, nil, provider.platform)
				require.NoError(t, err)
				require.Equal(t, primary.ID, selected.ID)
				require.True(t, svc.isAccountSchedulableForSelection(primary))
				require.True(t, svc.isAccountSchedulableForModelSelection(otherGroup, primary, provider.model))
				require.Equal(t, failureReads, settingsRepo.reads, "selection adds no settings read")

				require.NoError(t, svc.rateLimitService.ClearAccountOverload(ctx, primary.ID))
				require.Equal(t, 1, repo.recoveries)
				selected, err = svc.selectAccountForModelWithPlatform(otherGroup, nil, "", provider.model, nil, provider.platform)
				require.NoError(t, err)
				require.Equal(t, primary.ID, selected.ID, "explicit recovery restores the preferred account")
				require.True(t, svc.isAccountSchedulableForModelSelection(otherGroup, primary, provider.model))
				require.Equal(t, failureReads, settingsRepo.reads)
			})
		}
	}
}

func TestGroupFailureNativeCooldownIsNotClearedByKiroPoolRecovery(t *testing.T) {
	ctx, svc, repo, _ := newGroupNativeSchedulingFixture(t, PlatformKiro)
	account := repo.accountsByID[901]
	handled, until := svc.rateLimitService.ApplyGroupFailurePolicy(ctx, account, GroupFailureTimeout)
	require.False(t, handled)
	require.True(t, until.IsZero())
	// Existing native/manual/legacy scheduling blocks still remain effective.
	now := time.Now()
	svc.rateLimitService.overloadFailures.rememberCooldown(account.ID, now, now.Add(time.Minute))
	store := &stubKiroCooldownStore{state: &kirocooldown.State{
		Active: true, Reason: kirocooldown.CooldownReason429,
		CooldownUntil: time.Now().Add(time.Minute), Remaining: time.Minute,
	}, clearResult: true}
	svc.kiroCooldownStore = store

	require.False(t, svc.tryRecoverKiroCooldownPool(ctx, []Account{*account}, "", nil, false))
	require.False(t, store.clearCalled, "pool exhaustion cannot shorten the explicit group cooldown")
	require.NoError(t, svc.rateLimitService.ClearAccountOverload(ctx, account.ID))
	require.True(t, svc.tryRecoverKiroCooldownPool(ctx, []Account{*account}, "", nil, false))
	require.Equal(t, []string{buildKiroAccountKey(account)}, store.clearKeys)
}
