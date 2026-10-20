package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/stretchr/testify/require"
)

type keyRouteCatalogueGroups struct {
	GroupRepository
	groups map[int64]*Group
	calls  []int64
	err    map[int64]error
}

func (r *keyRouteCatalogueGroups) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	r.calls = append(r.calls, id)
	if err := r.err[id]; err != nil {
		return nil, err
	}
	if group := r.groups[id]; group != nil {
		return group, nil
	}
	return nil, ErrGroupNotFound
}

type keyRouteCatalogueAccounts struct {
	AccountRepository
	accounts map[int64][]Account
	calls    []int64
	err      map[int64]error
}

func (r *keyRouteCatalogueAccounts) ListSchedulableByGroupID(_ context.Context, id int64) ([]Account, error) {
	r.calls = append(r.calls, id)
	return r.accounts[id], r.err[id]
}

func newKeyRouteCatalogueFixture() (*KeyRoutingService, *APIKey, *keyRouteCatalogueGroups, *keyRouteCatalogueAccounts) {
	groups := &keyRouteCatalogueGroups{groups: map[int64]*Group{}, err: map[int64]error{}}
	accounts := &keyRouteCatalogueAccounts{accounts: map[int64][]Account{}, err: map[int64]error{}}
	for _, id := range []int64{1, 2, 3, 4, 5} {
		groups.groups[id] = &Group{ID: id, Platform: PlatformDeepseek, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard}
		accounts.accounts[id] = []Account{{ID: id * 10, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"deepseek-visible": "deepseek-upstream"}}}}
	}
	key := &APIKey{ID: 17, UserID: 7, User: &User{ID: 7}, GroupID: &groups.groups[1].ID, Group: groups.groups[1], BackupGroupIDs: []int64{3, 2}}
	svc := &KeyRoutingService{groups: groups, native: &GatewayService{accountRepo: accounts}}
	return svc, key, groups, accounts
}

func TestKeyRouteCatalogueCandidatesOrderAndIsolation(t *testing.T) {
	svc, key, groups, accounts := newKeyRouteCatalogueFixture()
	groups.groups[1].ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"deepseek-*"}}
	result, err := svc.CatalogueCandidates(context.Background(), key)
	require.NoError(t, err)
	require.Len(t, result, 3)
	require.Equal(t, []int64{1, 3, 2}, groups.calls)
	require.Equal(t, []int64{1, 3, 2}, accounts.calls)
	require.EqualValues(t, 1, result[0].Group.ID)
	require.EqualValues(t, 3, result[1].Group.ID)
	require.EqualValues(t, 2, result[2].Group.ID)
	require.Equal(t, []string{"deepseek-visible"}, result[0].ModelIDs)
	result[0].Group.ModelAllowlist.Models[0] = "changed"
	require.Equal(t, "deepseek-*", groups.groups[1].ModelAllowlist.Models[0])
	require.EqualValues(t, 1, *key.GroupID)
	require.Equal(t, []int64{3, 2}, key.BackupGroupIDs)
	require.False(t, accounts.accounts[1][0].modelMappingCacheReady, "read-only discovery must not mutate source account snapshots")
}

func TestKeyRouteCatalogueCandidatesRevalidateGroups(t *testing.T) {
	for _, condition := range []string{"missing", "inactive", "subscription", "unknown type", "claude only", "exclusive", "restricted public"} {
		t.Run(condition, func(t *testing.T) {
			svc, key, groups, accounts := newKeyRouteCatalogueFixture()
			key.BackupGroupIDs = []int64{2}
			switch condition {
			case "missing":
				delete(groups.groups, 2)
			case "inactive":
				groups.groups[2].Status = StatusDisabled
			case "subscription":
				groups.groups[2].SubscriptionType = SubscriptionTypeSubscription
			case "unknown type":
				groups.groups[2].SubscriptionType = "future-subscription"
			case "claude only":
				groups.groups[2].ClaudeCodeOnly = true
			case "exclusive":
				groups.groups[2].IsExclusive = true
			case "restricted public":
				key.User.RestrictPublicGroups = true
				key.User.AllowedGroups = []int64{1}
			}
			result, err := svc.CatalogueCandidates(context.Background(), key)
			require.NoError(t, err)
			require.Len(t, result, 1)
			require.Equal(t, []int64{1}, accounts.calls, "ineligible groups must not even expose their account catalogue")
		})
	}
	// Do not trust the group snapshot already cached on the key.
	svc, key, groups, _ := newKeyRouteCatalogueFixture()
	groups.groups[1] = &Group{ID: 1, Platform: PlatformDeepseek, Status: StatusDisabled, SubscriptionType: SubscriptionTypeStandard}
	result, err := svc.CatalogueCandidates(context.Background(), key)
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.EqualValues(t, 3, result[0].Group.ID)
}

func TestKeyRouteCatalogueStopsAtNativeRouteAuthorizationBarrier(t *testing.T) {
	for _, condition := range []string{"revoked", "subscription", "claude only", "inactive and revoked"} {
		t.Run(condition, func(t *testing.T) {
			svc, key, groups, accounts := newKeyRouteCatalogueFixture()
			key.BackupGroupIDs = []int64{2, 3}
			switch condition {
			case "revoked":
				groups.groups[2].IsExclusive = true
			case "subscription":
				groups.groups[2].SubscriptionType = SubscriptionTypeSubscription
			case "claude only":
				groups.groups[2].ClaudeCodeOnly = true
			case "inactive and revoked":
				groups.groups[2].IsExclusive = true
				groups.groups[2].Status = StatusDisabled
			}
			result, err := svc.CatalogueCandidates(context.Background(), key)
			require.NoError(t, err)
			require.Len(t, result, 1, "keep earlier reachable catalogues without advertising unreachable later groups")
			require.EqualValues(t, 1, result[0].Group.ID)
			require.Equal(t, []int64{1, 2}, groups.calls, "never inspect groups beyond a native route barrier")
			require.Equal(t, []int64{1}, accounts.calls, "neither blocked nor unreachable group accounts may be queried")
		})
	}
}

func TestKeyRouteCatalogueInactiveAndMissingGroupsDoNotBlockLaterGroups(t *testing.T) {
	for _, condition := range []string{"inactive", "missing"} {
		t.Run(condition, func(t *testing.T) {
			svc, key, groups, accounts := newKeyRouteCatalogueFixture()
			key.BackupGroupIDs = []int64{2, 3}
			if condition == "inactive" {
				groups.groups[2].Status = StatusDisabled
			} else {
				delete(groups.groups, 2)
			}
			result, err := svc.CatalogueCandidates(context.Background(), key)
			require.NoError(t, err)
			require.Len(t, result, 2)
			require.EqualValues(t, 1, result[0].Group.ID)
			require.EqualValues(t, 3, result[1].Group.ID)
			require.Equal(t, []int64{1, 2, 3}, groups.calls)
			require.Equal(t, []int64{1, 3}, accounts.calls)
		})
	}
}

func TestKeyRouteCatalogueCandidatesRejectReadErrorsAndInvalidConfiguration(t *testing.T) {
	for _, source := range []string{"group", "account"} {
		t.Run(source, func(t *testing.T) {
			svc, key, groups, accounts := newKeyRouteCatalogueFixture()
			errRead := errors.New("synthetic storage failure")
			if source == "group" {
				groups.err[3] = errRead
			} else {
				accounts.err[3] = errRead
			}
			result, err := svc.CatalogueCandidates(context.Background(), key)
			require.ErrorIs(t, err, errRead)
			require.Nil(t, result, "never substitute platform defaults for an unreadable source")
		})
	}
	for _, backups := range [][]int64{{1}, {2, 2}, {-1}, {2, 3, 4, 5, 6}} {
		svc, key, groups, _ := newKeyRouteCatalogueFixture()
		key.BackupGroupIDs = backups
		_, err := svc.CatalogueCandidates(context.Background(), key)
		require.ErrorIs(t, err, keyrouting.ErrInvalidConfig)
		require.Empty(t, groups.calls)
	}
	svc, key, _, _ := newKeyRouteCatalogueFixture()
	key.User.ID = 8
	_, err := svc.CatalogueCandidates(context.Background(), key)
	require.ErrorIs(t, err, keyrouting.ErrInvalidConfig)
	svc, key, _, _ = newKeyRouteCatalogueFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.CatalogueCandidates(ctx, key)
	require.ErrorIs(t, err, context.Canceled)
	svc.cfg = &config.Config{RunMode: config.RunModeSimple}
	_, err = svc.CatalogueCandidates(context.Background(), key)
	require.Error(t, err)
}

func TestKeyRouteCatalogueSourceDoesNotInventDefaults(t *testing.T) {
	svc, key, groups, accounts := newKeyRouteCatalogueFixture()
	key.BackupGroupIDs = []int64{2}
	accounts.accounts[1] = nil
	accounts.accounts[2][0].Credentials = nil // DeepSeek has no ordinary native static catalogue.
	result, err := svc.CatalogueCandidates(context.Background(), key)
	require.NoError(t, err)
	require.Empty(t, result)
	groups.groups[2].Platform = PlatformOpenAI
	accounts.accounts[2][0].Platform = PlatformOpenAI
	result, err = svc.CatalogueCandidates(context.Background(), key)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Empty(t, result[0].ModelIDs)
	require.Equal(t, []string{PlatformOpenAI}, result[0].DefaultPlatforms)

	future := time.Now().Add(time.Hour)
	for _, condition := range []string{"disabled", "unschedulable", "overload", "rate limited", "temporarily paused", "expired"} {
		t.Run(condition, func(t *testing.T) {
			account := accounts.accounts[2][0]
			switch condition {
			case "disabled":
				account.Status = StatusDisabled
			case "unschedulable":
				account.Schedulable = false
			case "overload":
				account.OverloadUntil = &future
			case "rate limited":
				account.RateLimitResetAt = &future
			case "temporarily paused":
				account.TempUnschedulableUntil = &future
			case "expired":
				past := time.Now().Add(-time.Hour)
				account.AutoPauseOnExpired, account.ExpiresAt = true, &past
			}
			candidate := keyRouteCatalogueSource(groups.groups[2], []Account{account}, true)
			require.Empty(t, candidate.ModelIDs)
			require.Empty(t, candidate.DefaultPlatforms)
		})
	}
}

func TestKeyRouteCatalogueSourceFiltersMappingsAndPlatforms(t *testing.T) {
	group := &Group{ID: 1, Platform: PlatformOpenAI, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"visible-*"}}}
	account := Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"visible-b": "upstream", " visible-trimmed ": "upstream", "visible-a": "upstream", "hidden": "upstream", "visible-*": "upstream", "visible-?": "upstream", "": "upstream", "visible-empty": " "}}}
	candidate := keyRouteCatalogueSource(group, []Account{account, {Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"visible-foreign": "upstream"}}}}, false)
	require.Equal(t, []string{"visible-?", "visible-a", "visible-b"}, candidate.ModelIDs)
	require.Empty(t, candidate.DefaultPlatforms)
	account.Extra = map[string]any{"openai_passthrough": true}
	candidate = keyRouteCatalogueSource(group, []Account{account}, false)
	require.Empty(t, candidate.ModelIDs, "passthrough must not advertise ignored stale mapping keys")
	require.Equal(t, []string{PlatformOpenAI}, candidate.DefaultPlatforms)
}

func TestKeyRouteCatalogueCompositeOnlyUsesAccountBackedSources(t *testing.T) {
	group := &Group{ID: 1, Platform: PlatformComposite}
	accounts := []Account{
		{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true},
		{Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"deepseek-custom": "deepseek-chat"}}},
		{Platform: "unknown-platform", Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"invented-model": "upstream"}}},
	}
	candidate := keyRouteCatalogueSource(group, accounts, true)
	require.Equal(t, []string{"deepseek-custom"}, candidate.ModelIDs)
	require.Equal(t, []string{PlatformOpenAI}, candidate.DefaultPlatforms)
}

func TestKeyRouteCatalogueWildcardRequiresRealAccountMapping(t *testing.T) {
	group := &Group{ID: 1, Platform: PlatformAnthropic, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"claude-sonnet-4-6", "unbacked-model", "claude-*"}}}
	account := Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"claude-*": "upstream-sonnet"}}}
	candidate := keyRouteCatalogueSource(group, []Account{account}, true)
	require.Equal(t, []string{"claude-sonnet-4-6"}, candidate.ModelIDs)
	account.Credentials = nil
	candidate = keyRouteCatalogueSource(group, []Account{account}, true)
	require.Empty(t, candidate.ModelIDs, "an allowlist by itself is not a model source")
	require.Equal(t, []string{PlatformAnthropic}, candidate.DefaultPlatforms)
}

func TestKeyRouteCatalogueBackupExcludesPrimaryOnlyEndpointsAndMedia(t *testing.T) {
	group := &Group{ID: 1, Platform: PlatformTypeSafe}
	account := Account{Platform: PlatformTypeSafe, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"jev-latest": "jev-latest"}}}
	require.Equal(t, []string{"jev-latest"}, keyRouteCatalogueSource(group, []Account{account}, false).ModelIDs)
	backup := keyRouteCatalogueSource(group, []Account{account}, true)
	require.Empty(t, backup.ModelIDs)
	require.Empty(t, backup.DefaultPlatforms)

	group.Platform, account.Platform = PlatformOpenAI, PlatformOpenAI
	account.Credentials = map[string]any{"model_mapping": map[string]any{
		"gpt-text": "gpt-text", "gpt-image-2": "gpt-image-2", "image-alias": "gpt-image-2",
		"gemini-nano-banana-2.1": "gemini-nano-banana-2.1", "grok-imagine-video": "grok-imagine-video",
	}}
	primary := keyRouteCatalogueSource(group, []Account{account}, false)
	require.Len(t, primary.ModelIDs, 5)
	backup = keyRouteCatalogueSource(group, []Account{account}, true)
	require.Equal(t, []string{"gpt-text"}, backup.ModelIDs)
	for _, model := range []string{"gpt-image-2", "gemini-nano-banana-2.1", "grok-imagine-video", "grok-video-1.5", "*", ""} {
		require.False(t, KeyRouteCatalogueBackupModelAllowed(model), model)
	}
	require.True(t, KeyRouteCatalogueBackupModelAllowed("gpt-4o-mini"))
}

func TestKeyRouteCatalogueLiteralContextSuffixAndQuestionMark(t *testing.T) {
	group := &Group{ID: 1, Platform: PlatformDeepseek, ModelAllowlist: GroupModelAllowlist{
		Enabled: true, Models: []string{"deepseek-flash[1m]", "claude-fable-5[1m]", "literal?model", "unbacked[1m]"},
	}}
	account := Account{Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"deepseek-flash[1m]": "deepseek-flash[1m]", "claude-fable-5[1m]": "claude-fable-5[1m]", "literal?model": "upstream",
		}}}
	candidate := keyRouteCatalogueSource(group, []Account{account}, true)
	require.Equal(t, []string{"claude-fable-5[1m]", "deepseek-flash[1m]", "literal?model"}, candidate.ModelIDs)
	for _, model := range candidate.ModelIDs {
		require.True(t, account.IsModelSupported(model), model)
		require.True(t, KeyRouteCatalogueBackupModelAllowed(model), model)
	}
	require.False(t, account.IsModelSupported("literalXmodel"), "native ? is a literal, not a wildcard")
	account.Credentials = map[string]any{"model_mapping": map[string]any{"deepseek-*": "deepseek-flash"}}
	candidate = keyRouteCatalogueSource(group, []Account{account}, true)
	require.Equal(t, []string{"deepseek-flash[1m]"}, candidate.ModelIDs, "real wildcard mappings may concretize a context-suffix group ID")
}
