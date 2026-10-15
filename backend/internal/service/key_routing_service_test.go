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
