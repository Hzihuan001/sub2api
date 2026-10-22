package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/stretchr/testify/require"
)

type keyRoutingGroups struct {
	GroupRepository
	groups map[int64]*Group
	err    error
}

func (r *keyRoutingGroups) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	if r.err != nil {
		return nil, r.err
	}
	if g := r.groups[id]; g != nil {
		return g, nil
	}
	return nil, ErrGroupNotFound
}

type keyRoutingAccounts struct {
	AccountRepository
	accounts map[int64][]Account
	calls    []int64
	err      error
}

func (r *keyRoutingAccounts) ListSchedulableByGroupIDAndPlatform(_ context.Context, id int64, _ string) ([]Account, error) {
	r.calls = append(r.calls, id)
	return r.accounts[id], r.err
}

func (r *keyRoutingAccounts) ListSchedulableByGroupIDAndPlatforms(_ context.Context, id int64, _ []string) ([]Account, error) {
	r.calls = append(r.calls, id)
	return r.accounts[id], r.err
}

type keyRoutingRates struct {
	UserGroupRateRepository
	calledGroup int64
	err         error
}

func (r *keyRoutingRates) GetRPMOverrideByUserAndGroup(_ context.Context, _, groupID int64) (*int, error) {
	r.calledGroup = groupID
	value := 42
	return &value, r.err
}

func newKeyRoutingFixture() (*KeyRoutingService, *APIKey, *keyRoutingAccounts, *keyRoutingGroups, *keyRoutingRates) {
	groups := &keyRoutingGroups{groups: map[int64]*Group{}}
	accounts := &keyRoutingAccounts{accounts: map[int64][]Account{}}
	for _, id := range []int64{1, 2, 3} {
		groups.groups[id] = &Group{ID: id, Status: StatusActive, Hydrated: true, Platform: PlatformDeepseek, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: float64(id)}
		accounts.accounts[id] = []Account{{ID: id * 10, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, GroupIDs: []int64{id}, Concurrency: 1, Credentials: map[string]any{"model_mapping": map[string]any{"deepseek-chat": "deepseek-chat"}}}}
	}
	rates := &keyRoutingRates{}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	openai := &OpenAIGatewayService{accountRepo: accounts, cfg: cfg}
	native := &GatewayService{accountRepo: accounts, cfg: cfg}
	service := NewKeyRoutingService(groups, rates, native, openai, NewCompositeRouteResolver(nil), cfg)
	rpm := 7
	key := &APIKey{ID: 17, UserID: 5, GroupID: &groups.groups[1].ID, Group: groups.groups[1], BackupGroupIDs: []int64{2, 3}, Quota: 100, QuotaUsed: 10,
		User: &User{ID: 5, Status: StatusActive, UserGroupRPMOverride: &rpm}}
	return service, key, accounts, groups, rates
}

func TestKeyRoutingServiceSelectsBackupAndLeavesAuthSnapshotUntouched(t *testing.T) {
	svc, key, accounts, groups, rates := newKeyRoutingFixture()
	groups.groups[1].ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"other-model"}}
	effective, plan, err := svc.Select(context.Background(), key, KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions})
	require.NoError(t, err)
	require.EqualValues(t, 2, plan.SelectedGroupID)
	require.Equal(t, keyrouting.ReasonModelUnavailable, plan.Reason)
	require.Equal(t, []int64{2}, accounts.calls)
	require.EqualValues(t, 2, *effective.GroupID)
	require.Equal(t, 2.0, effective.Group.RateMultiplier)
	require.Equal(t, key.ID, effective.ID)
	require.Equal(t, key.Quota, effective.Quota)
	require.Equal(t, key.QuotaUsed, effective.QuotaUsed)
	require.Equal(t, 42, *effective.User.UserGroupRPMOverride)
	require.EqualValues(t, 2, rates.calledGroup)
	require.EqualValues(t, 1, *key.GroupID)
	require.Equal(t, 7, *key.User.UserGroupRPMOverride)
	effective.BackupGroupIDs[0] = 999
	require.EqualValues(t, 2, key.BackupGroupIDs[0])
	effective.Group.RateMultiplier = 9
	require.Equal(t, 2.0, groups.groups[2].RateMultiplier)
}

func TestKeyRoutingServiceResolvesStoredResponseOnlyWithinConfiguredGroups(t *testing.T) {
	svc, key, _, _, _ := newKeyRoutingFixture()
	store := svc.openai.getOpenAIWSStateStore()
	ctx := context.Background()
	require.NoError(t, store.BindHTTPResponseOwner(ctx, 2, "resp_source", key.UserID, key.ID, time.Minute))
	require.NoError(t, store.BindResponseAccount(ctx, 2, "resp_source", 20, time.Minute))
	effective, accountID, err := svc.SelectHTTPResponseSource(ctx, key, KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointResponses}, "resp_source")
	require.NoError(t, err)
	require.EqualValues(t, 20, accountID)
	require.EqualValues(t, 2, *effective.GroupID)
	require.Equal(t, []int64{3}, effective.BackupGroupIDs)
	require.Equal(t, []int64{2, 3}, key.BackupGroupIDs)

	// A response owned by a group outside this key's route cannot be used as a
	// continuation; the resolver must never search globally or choose a backup.
	require.NoError(t, store.BindHTTPResponseOwner(ctx, 99, "resp_outside", key.UserID, key.ID, time.Minute))
	require.NoError(t, store.BindResponseAccount(ctx, 99, "resp_outside", 30, time.Minute))
	_, _, found, err := store.GetHTTPResponseOwner(ctx, 1, "resp_outside")
	require.NoError(t, err)
	require.False(t, found)
	_, _, found, err = store.GetHTTPResponseOwner(ctx, 2, "resp_outside")
	require.NoError(t, err)
	require.False(t, found)
	_, _, found, err = store.GetHTTPResponseOwner(ctx, 99, "resp_outside")
	require.NoError(t, err)
	require.True(t, found)
	groupID, accountID, resolveErr := svc.openai.ResolveOpenAIHTTPResponseSource(ctx, []int64{1, 2}, "resp_outside", key.UserID, key.ID)
	require.ErrorIs(t, resolveErr, ErrOpenAIHTTPResponseSourceUnavailable)
	require.Zero(t, groupID)
	require.Zero(t, accountID)
}

func TestKeyRoutingServiceRejectsAmbiguousStoredResponseOwner(t *testing.T) {
	svc, key, _, _, _ := newKeyRoutingFixture()
	store := svc.openai.getOpenAIWSStateStore()
	ctx := context.Background()
	for _, groupID := range []int64{1, 2} {
		require.NoError(t, store.BindHTTPResponseOwner(ctx, groupID, "resp_ambiguous", key.UserID, key.ID, time.Minute))
		require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_ambiguous", groupID*10, time.Minute))
	}
	_, _, err := svc.SelectHTTPResponseSource(ctx, key, KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointResponses}, "resp_ambiguous")
	require.ErrorIs(t, err, ErrOpenAIHTTPResponseSourceUnavailable)
}

func TestKeyRoutingServiceNoAccountsFallsThroughButRecoversPrimaryOnNextRequest(t *testing.T) {
	svc, key, accounts, _, _ := newKeyRoutingFixture()
	primary := accounts.accounts[1]
	accounts.accounts[1] = nil
	_, plan, err := svc.Select(context.Background(), key, KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions})
	require.NoError(t, err)
	require.EqualValues(t, 2, plan.SelectedGroupID)
	require.Equal(t, keyrouting.ReasonNoSchedulableAccounts, plan.Reason)
	accounts.accounts[1] = primary
	_, plan, err = svc.Select(context.Background(), key, KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions})
	require.NoError(t, err)
	require.EqualValues(t, 1, plan.SelectedGroupID)
}

func TestKeyRoutingServiceRouteCooldownOnlySkipsWhenBackupExists(t *testing.T) {
	policyRepo := &groupFailurePolicySettingRepo{}
	settings := groupFailurePolicyTestService(policyRepo)
	policy := &GroupFailurePolicy{StatusRules: []GroupFailureStatusRule{{
		StatusCodes: []int{502}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 600,
	}}}
	_, err := settings.UpdateGroupFailurePolicy(context.Background(), 1, policy)
	require.NoError(t, err)

	for _, withBackup := range []bool{false, true} {
		t.Run(fmt.Sprintf("with_backup_%t", withBackup), func(t *testing.T) {
			svc, key, _, groups, _ := newKeyRoutingFixture()
			svc.settings = settings
			if withBackup {
				key.BackupGroupIDs = []int64{2}
			} else {
				key.BackupGroupIDs = nil
			}
			routePolicy := keyrouting.RouteFailurePolicy{Rules: []keyrouting.RouteFailureRule{{
				StatusCodes: []int{502}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 600,
			}}}
			svc.failures.Record(keyrouting.RouteFailureIdentity{APIKeyID: key.ID, GroupID: 1, Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions}, 502, routePolicy)
			effective, plan, err := svc.Select(context.Background(), key, KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions})
			require.NoError(t, err)
			wantGroup := int64(1)
			if withBackup {
				wantGroup = 2
			}
			require.Equal(t, wantGroup, plan.SelectedGroupID)
			require.Equal(t, wantGroup, *effective.GroupID)
			require.Equal(t, wantGroup, effective.Group.ID)
			require.NotNil(t, groups.groups[wantGroup])
		})
	}
}

func TestKeyRoutingServiceKeyPolicyOverridesGroupPolicy(t *testing.T) {
	policyRepo := &groupFailurePolicySettingRepo{}
	settings := groupFailurePolicyTestService(policyRepo)
	require.NoError(t, func() error {
		_, err := settings.UpdateGroupFailurePolicy(context.Background(), 1, &GroupFailurePolicy{StatusRules: []GroupFailureStatusRule{{
			StatusCodes: []int{502}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 600,
		}}})
		return err
	}())
	svc, key, _, _, _ := newKeyRoutingFixture()
	svc.settings = settings
	key.KeyRouteFailurePolicy = &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{{
		StatusCodes: []int{503}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 600,
	}}}
	request := KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions}
	result, err := svc.RecordFailure(context.Background(), key, request, 1, 502)
	require.NoError(t, err)
	require.False(t, result.Triggered)
	result, err = svc.RecordFailure(context.Background(), key, request, 1, 503)
	require.NoError(t, err)
	require.True(t, result.Triggered)

	// An explicitly empty key policy disables group rules instead of falling
	// back to the group setting.
	key.KeyRouteFailurePolicy = &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{}}
	result, err = svc.RecordFailure(context.Background(), key, request, 1, 503)
	require.NoError(t, err)
	require.False(t, result.Triggered)
}

func TestKeyRoutingServiceFailsClosedForUnauthorizedAndUnavailableDependencies(t *testing.T) {
	for _, tc := range []string{"unauthorized", "subscription", "group-error", "account-error", "rpm-error", "simple-mode"} {
		t.Run(tc, func(t *testing.T) {
			svc, key, accounts, groups, rates := newKeyRoutingFixture()
			accounts.accounts[1] = nil
			switch tc {
			case "unauthorized":
				groups.groups[2].IsExclusive = true
			case "subscription":
				groups.groups[2].SubscriptionType = SubscriptionTypeSubscription
			case "group-error":
				groups.err = errors.New("group storage unavailable")
			case "account-error":
				accounts.err = errors.New("account storage unavailable")
			case "rpm-error":
				rates.err = errors.New("RPM storage unavailable")
			case "simple-mode":
				svc.cfg.RunMode = config.RunModeSimple
			}
			effective, _, err := svc.Select(context.Background(), key, KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions})
			require.Error(t, err)
			require.Nil(t, effective)
			require.NotContains(t, accounts.calls, int64(3))
		})
	}
}

func TestKeyRoutingServiceZeroMultiplierAndClearedRPMKeepActualGroupSemantics(t *testing.T) {
	svc, key, accounts, groups, _ := newKeyRoutingFixture()
	accounts.accounts[1] = nil
	groups.groups[2].RateMultiplier = 0
	svc.rates = nil
	effective, _, err := svc.Select(context.Background(), key, KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions})
	require.NoError(t, err)
	require.Equal(t, 0.0, effective.Group.RateMultiplier)
	require.Nil(t, effective.User.UserGroupRPMOverride)
	require.Equal(t, 7, *key.User.UserGroupRPMOverride)
}

func TestKeyRoutingServiceRejectsClaudeOnlyPolicyChangedAfterKeyCreation(t *testing.T) {
	for _, id := range []int64{1, 2} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			svc, key, accounts, groups, _ := newKeyRoutingFixture()
			accounts.accounts[1] = nil
			groups.groups[id].ClaudeCodeOnly = true
			effective, _, err := svc.Select(context.Background(), key, KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions})
			require.ErrorIs(t, err, keyrouting.ErrInvalidConfig)
			require.Nil(t, effective)
			require.NotContains(t, accounts.calls, id)
			require.NotContains(t, accounts.calls, int64(3))
		})
	}
}

func TestKeyRoutingEndpointPolicyDoesNotBroadenMessagesDispatch(t *testing.T) {
	g := &Group{Platform: PlatformOpenAI}
	require.False(t, keyRoutingEndpointAllowed(g, PlatformOpenAI, CompositeRouteEndpointMessages))
	require.True(t, keyRoutingEndpointAllowed(g, PlatformOpenAI, CompositeRouteEndpointResponses))
	require.True(t, keyRoutingEndpointAllowed(g, PlatformDeepseek, CompositeRouteEndpointMessages))
	require.False(t, keyRoutingEndpointAllowed(g, PlatformDeepseek, CompositeRouteEndpointImages))
	g.AllowMessagesDispatch = true
	require.True(t, keyRoutingEndpointAllowed(g, PlatformOpenAI, CompositeRouteEndpointMessages))
}

func keyRoutingChannelMappingFixture(groupID int64, platform, publicModel, upstreamModel string) *ChannelService {
	channels := &ChannelService{}
	cache := newEmptyChannelCache()
	cache.loadedAt = time.Now()
	cache.channelByGroupID[groupID] = &Channel{ID: 9, Status: StatusActive, BillingModelSource: BillingModelSourceChannelMapped}
	cache.groupPlatform[groupID] = platform
	cache.mappingByGroupModel[channelModelKey{groupID: groupID, platform: platform, model: publicModel}] = upstreamModel
	channels.cache.Store(cache)
	return channels
}

func TestKeyRoutingServiceMatchesTextHandlerSchedulingModel(t *testing.T) {
	for _, tt := range []struct {
		name            string
		endpoint        string
		publicModel     string
		accountModel    string
		dispatchMapping string
		channelMapping  string
	}{
		{"messages dispatch mapping", CompositeRouteEndpointMessages, "claude-sonnet-4-5", "gpt-5.3-codex", "gpt-5.3-codex", ""},
		{"responses channel mapping", CompositeRouteEndpointResponses, "public-text", "gpt-5.3-codex", "", "gpt-5.3-codex"},
		{"chat channel mapping", CompositeRouteEndpointChatCompletions, "public-text", "gpt-5.3-codex", "", "gpt-5.3-codex"},
		{"responses ignores messages dispatch mapping", CompositeRouteEndpointResponses, "claude-sonnet-4-5", "claude-sonnet-4-5", "gpt-5.3-codex", ""},
		{"chat ignores messages dispatch mapping", CompositeRouteEndpointChatCompletions, "claude-sonnet-4-5", "claude-sonnet-4-5", "gpt-5.3-codex", ""},
		{"messages does not schedule on channel forwarding mapping", CompositeRouteEndpointMessages, "public-text", "public-text", "", "gpt-5.3-codex"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, key, accounts, groups, _ := newKeyRoutingFixture()
			primary := groups.groups[1]
			primary.Platform = PlatformOpenAI
			primary.AllowMessagesDispatch = true
			primary.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{tt.publicModel}}
			if tt.dispatchMapping != "" {
				primary.MessagesDispatchModelConfig = OpenAIMessagesDispatchModelConfig{ExactModelMappings: map[string]string{tt.publicModel: tt.dispatchMapping}}
			}
			account := &accounts.accounts[1][0]
			account.Platform = PlatformOpenAI
			account.Credentials = map[string]any{"model_mapping": map[string]any{tt.accountModel: tt.accountModel}}
			if tt.channelMapping != "" {
				svc.openai.channelService = keyRoutingChannelMappingFixture(1, PlatformOpenAI, tt.publicModel, tt.channelMapping)
			}
			effective, plan, err := svc.Select(context.Background(), key, KeyRoutingRequest{Model: tt.publicModel, Endpoint: tt.endpoint})
			require.NoError(t, err)
			require.EqualValues(t, 1, plan.SelectedGroupID)
			require.Equal(t, keyrouting.ReasonPrimary, plan.Reason)
			require.Equal(t, []int64{1}, accounts.calls)
			require.True(t, effective.Group.ModelAllowlist.Allows(tt.publicModel))
		})
	}
}

type keyRoutingChannelReadError struct {
	ChannelRepository
	err error
}

func (r keyRoutingChannelReadError) ListAll(context.Context) ([]Channel, error) {
	return nil, r.err
}

func TestKeyRoutingServiceChannelMappingReadErrorFailsClosed(t *testing.T) {
	for _, endpoint := range []string{CompositeRouteEndpointResponses, CompositeRouteEndpointChatCompletions, CompositeRouteEndpointMessages} {
		t.Run(endpoint, func(t *testing.T) {
			svc, key, accounts, _, _ := newKeyRoutingFixture()
			readErr := errors.New("channel mapping unavailable")
			svc.openai.channelService = &ChannelService{repo: keyRoutingChannelReadError{err: readErr}}
			effective, plan, err := svc.Select(context.Background(), key, KeyRoutingRequest{Model: "deepseek-chat", Endpoint: endpoint})
			require.ErrorIs(t, err, readErr)
			require.Nil(t, effective)
			require.Zero(t, plan.SelectedGroupID)
			require.NotContains(t, accounts.calls, int64(2))
			require.NotContains(t, accounts.calls, int64(3))
		})
	}
}

func keyRoutingCooldownPolicy() *KeyRouteFailurePolicy {
	return &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{{StatusCodes: []int{502}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 600}}}
}

func coolKeyRouteGroups(t *testing.T, svc *KeyRoutingService, key *APIKey, request KeyRoutingRequest, groupIDs ...int64) {
	t.Helper()
	for _, groupID := range groupIDs {
		result, err := svc.RecordFailure(context.Background(), key, request, groupID, 502)
		require.NoError(t, err)
		require.True(t, result.Triggered)
	}
}

func TestKeyRoutingServiceKeyOverrideCooldownWorksWithoutSettings(t *testing.T) {
	svc, key, accounts, _, _ := newKeyRoutingFixture()
	key.KeyRouteFailurePolicy = keyRoutingCooldownPolicy()
	request := KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions}
	coolKeyRouteGroups(t, svc, key, request, 1)
	effective, plan, err := svc.Select(context.Background(), key, request)
	require.NoError(t, err)
	require.EqualValues(t, 2, *effective.GroupID)
	require.EqualValues(t, 2, plan.SelectedGroupID)
	require.Equal(t, []int64{1, 2}, accounts.calls)
}

func TestKeyRoutingServiceAllCoolingUsesLastReadyGroupOnceAndRetainsCooldown(t *testing.T) {
	svc, key, accounts, _, _ := newKeyRoutingFixture()
	key.KeyRouteFailurePolicy = keyRoutingCooldownPolicy()
	request := KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions}
	coolKeyRouteGroups(t, svc, key, request, 1, 2, 3)
	for attempt := 0; attempt < 2; attempt++ {
		accounts.calls = nil
		effective, plan, err := svc.Select(context.Background(), key, request)
		require.NoError(t, err)
		require.EqualValues(t, 3, *effective.GroupID)
		require.EqualValues(t, 3, plan.SelectedGroupID)
		require.Equal(t, []int64{1, 2, 3, 3}, accounts.calls, "only the last ready cooled candidate receives one native recheck")
		require.Len(t, plan.Attempts, 4)
		for _, id := range []int64{1, 2, 3} {
			cooling, _ := svc.failures.IsCooling(keyrouting.RouteFailureIdentity{APIKeyID: key.ID, GroupID: id, Model: request.Model, Endpoint: request.Endpoint}, *key.KeyRouteFailurePolicy)
			require.True(t, cooling, "one request-local fallback must not release the route cooldown")
		}
	}
}

func TestKeyRoutingServiceCooldownFallbackNeverBypassesNativeReadiness(t *testing.T) {
	for _, allDisabled := range []bool{false, true} {
		t.Run(fmt.Sprint(allDisabled), func(t *testing.T) {
			svc, key, accounts, _, _ := newKeyRoutingFixture()
			key.KeyRouteFailurePolicy = keyRoutingCooldownPolicy()
			request := KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions}
			coolKeyRouteGroups(t, svc, key, request, 1, 2, 3)
			accounts.accounts[3][0].Schedulable = false
			if allDisabled {
				accounts.accounts[1][0].Schedulable = false
				accounts.accounts[2][0].Schedulable = false
			}
			effective, plan, err := svc.Select(context.Background(), key, request)
			if allDisabled {
				require.ErrorIs(t, err, keyrouting.ErrNoSchedulableAccounts)
				require.Nil(t, effective)
				require.Equal(t, []int64{1, 2, 3}, accounts.calls)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 2, plan.SelectedGroupID)
			require.EqualValues(t, 2, *effective.GroupID)
			require.Equal(t, []int64{1, 2, 3, 2}, accounts.calls)
		})
	}
}

func TestKeyRoutingServiceCooldownFallbackDoesNotBypassAuthorization(t *testing.T) {
	svc, key, accounts, groups, _ := newKeyRoutingFixture()
	key.KeyRouteFailurePolicy = keyRoutingCooldownPolicy()
	request := KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions}
	coolKeyRouteGroups(t, svc, key, request, 1, 2, 3)
	groups.groups[3].IsExclusive = true
	effective, _, err := svc.Select(context.Background(), key, request)
	require.ErrorIs(t, err, keyrouting.ErrNotAuthorized)
	require.Nil(t, effective)
	require.Equal(t, []int64{1, 2}, accounts.calls, "revoked access must fail closed, not use a cooled group")
}

func TestKeyRoutingServiceSelectNextCooldownFallbackNeverWraps(t *testing.T) {
	svc, key, accounts, _, _ := newKeyRoutingFixture()
	key.KeyRouteFailurePolicy = keyRoutingCooldownPolicy()
	request := KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions}
	coolKeyRouteGroups(t, svc, key, request, 1, 2, 3)
	effective, plan, err := svc.SelectNext(context.Background(), key, request, 1)
	require.NoError(t, err)
	require.EqualValues(t, 3, plan.SelectedGroupID)
	require.EqualValues(t, 3, *effective.GroupID)
	require.Equal(t, []int64{2, 3, 3}, accounts.calls)
	accounts.calls = nil
	effective, _, err = svc.SelectNext(context.Background(), key, request, 3)
	require.Error(t, err)
	require.Nil(t, effective)
	require.Empty(t, accounts.calls)
	effective, _, err = svc.SelectNext(context.Background(), key, request, 99)
	require.ErrorIs(t, err, keyrouting.ErrInvalidConfig)
	require.Nil(t, effective)
	require.Empty(t, accounts.calls)
}

func TestKeyRoutingServiceDisabledFailureRulesStillAllowsCapabilityFallback(t *testing.T) {
	for _, noModel := range []bool{false, true} {
		t.Run(fmt.Sprint(noModel), func(t *testing.T) {
			svc, key, accounts, groups, _ := newKeyRoutingFixture()
			key.KeyRouteFailurePolicy = &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{}}
			if noModel {
				groups.groups[1].ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"other-model"}}
			} else {
				accounts.accounts[1] = nil
			}
			effective, plan, err := svc.Select(context.Background(), key, KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions})
			require.NoError(t, err)
			require.EqualValues(t, 2, *effective.GroupID)
			require.EqualValues(t, 2, plan.SelectedGroupID)
		})
	}
}

func TestKeyRoutingServiceRecordOutcomePreservesSemanticPolicy(t *testing.T) {
	svc, key, _, _, _ := newKeyRoutingFixture()
	key.KeyRouteFailurePolicy = &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{{Type: keyrouting.FailureRuleConsecutiveFailures, Threshold: 2, CooldownSeconds: 600}}}
	request := KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions}
	for _, outcome := range []keyrouting.RouteOutcomeKind{keyrouting.OutcomeFailure, keyrouting.OutcomeSuccess, keyrouting.OutcomeFailure} {
		result, err := svc.RecordOutcome(context.Background(), key, request, 1, outcome)
		require.NoError(t, err)
		require.False(t, result.Triggered)
	}
	result, err := svc.RecordOutcome(context.Background(), key, request, 1, keyrouting.OutcomeTimeout)
	require.NoError(t, err)
	require.True(t, result.Triggered)
	policy, err := svc.loadRouteFailurePolicy(context.Background(), key, 1)
	require.NoError(t, err)
	require.Equal(t, keyrouting.FailureRuleConsecutiveFailures, policy.Rules[0].Type)
	require.Empty(t, policy.Rules[0].StatusCodes)
}

type keyRoutingCompositeRoutes struct {
	CompositeModelRouteRepository
	err error
}

func (r keyRoutingCompositeRoutes) ListByGroup(_ context.Context, groupID int64, _ bool) ([]CompositeModelRoute, error) {
	return []CompositeModelRoute{{ID: 9, GroupID: groupID, Enabled: true, MatchType: CompositeRouteMatchExact, PublicModel: "alias", UpstreamModel: "deepseek-upstream", TargetPlatform: PlatformDeepseek, Endpoint: CompositeRouteEndpointChatCompletions}}, r.err
}

func TestKeyRoutingServicePrepareRouteContextUsesSelectedGroup(t *testing.T) {
	svc, key, _, _, _ := newKeyRoutingFixture()
	request := KeyRoutingRequest{Model: "gpt-named-alias", Endpoint: CompositeRouteEndpointChatCompletions}
	ctx, model, err := svc.PrepareRouteContext(context.Background(), key, request)
	require.NoError(t, err)
	require.Equal(t, request.Model, model)
	platform, ok := ResolvedTargetPlatformFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, PlatformDeepseek, platform, "model name must not override a concrete group's platform")

	key.Group.Platform = PlatformComposite
	svc.composite = NewCompositeRouteResolver(keyRoutingCompositeRoutes{})
	request.Model = "alias"
	ctx, model, err = svc.PrepareRouteContext(context.Background(), key, request)
	require.NoError(t, err)
	require.Equal(t, "deepseek-upstream", model)
	platform, ok = ResolvedTargetPlatformFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, PlatformDeepseek, platform)
	public, ok := RequestedPublicModelFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "alias", public)

	request.Model = "unknown-alias"
	_, _, err = svc.PrepareRouteContext(context.Background(), key, request)
	require.ErrorIs(t, err, keyrouting.ErrModelUnavailable)
	svc.composite = NewCompositeRouteResolver(keyRoutingCompositeRoutes{err: errors.New("route storage unavailable")})
	_, _, err = svc.PrepareRouteContext(context.Background(), key, request)
	require.ErrorContains(t, err, "route storage unavailable")
}
