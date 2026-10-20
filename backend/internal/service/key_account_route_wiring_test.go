package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type keyAccountCoordinatorRevisions struct{ revision int64 }

func (r *keyAccountCoordinatorRevisions) GetKeyRouteRevision(context.Context, int64) (int64, error) {
	return r.revision, nil
}
func (r *keyAccountCoordinatorRevisions) GetGroupRoutePolicySnapshot(context.Context, int64) (string, int64, error) {
	return `{"rules":[]}`, r.revision, nil
}

type keyAccountCoordinatorAccounts struct{ source *keyRoutingAccounts }

func (a keyAccountCoordinatorAccounts) ListSchedulableByGroupID(_ context.Context, id int64) ([]Account, error) {
	return append([]Account(nil), a.source.accounts[id]...), a.source.err
}

func keyAccountCoordinatorFixture(t *testing.T) (*KeyRoutingService, *APIKey, *keyRoutingAccounts, *keyAccountCoordinatorRevisions, KeyRoutingRequest) {
	t.Helper()
	s, key, accounts, _, _ := newKeyRoutingFixture()
	key.KeyRouteScope, key.KeyRouteRevision = KeyRouteScopeAccountV2, 100
	key.KeyRouteFailurePolicy = &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{{StatusCodes: []int{503}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 600}}}
	other := accounts.accounts[1][0]
	other.ID = 11
	accounts.accounts[1] = append(accounts.accounts[1], other)
	revisions := &keyAccountCoordinatorRevisions{revision: 100}
	s.accounts = NewKeyAccountRoutingRuntime(keyrouting.NewMemoryAccountHealthStore(keyrouting.AccountHealthOptions{}), revisions, keyAccountCoordinatorAccounts{accounts}, KeyAccountRoutingRuntimeOptions{Site: "coordinator-site"})
	return s, key, accounts, revisions, KeyRoutingRequest{Model: "deepseek-chat", Endpoint: CompositeRouteEndpointChatCompletions}
}

func observeKeyAccountCoordinatorFailure(t *testing.T, ctx context.Context, groupID, accountID int64) {
	t.Helper()
	session := GetKeyAccountRoutingSession(ctx)
	metadata, err := session.Metadata(groupID, accountID, "deepseek-chat", PlatformDeepseek)
	require.NoError(t, err)
	metadata.AttemptID = uuid.NewString()
	require.NoError(t, session.TryDispatch(ctx, metadata))
	now := time.Now()
	state, err := session.Observe(ctx, keyrouting.AccountAttemptSample{
		Metadata: metadata, Dispatched: true, Outcome: keyrouting.OutcomeFailure,
		StatusCode: 503, StatusSource: keyrouting.AttemptStatusUpstream, UpstreamStatusCode: 503,
		StartedAt: now.Add(-time.Second), FinishedAt: now,
	})
	require.NoError(t, err)
	require.True(t, state.Cooling)
}

func TestKeyAccountCoordinatorKeepsHealthySameGroupBeforeBackup(t *testing.T) {
	s, key, _, _, request := keyAccountCoordinatorFixture(t)
	ctx, err := s.PrepareAccountRouteContext(context.Background(), key, request)
	require.NoError(t, err)
	observeKeyAccountCoordinatorFailure(t, ctx, 1, 10)
	effective, plan, err := s.Select(ctx, key, request)
	require.NoError(t, err)
	require.EqualValues(t, 1, plan.SelectedGroupID)
	require.EqualValues(t, 1, *effective.GroupID)
	ready, err := s.AccountGroupHasCandidate(ctx, key, request, 1)
	require.NoError(t, err)
	require.True(t, ready, "one unhealthy account must not skip its healthy peer")
	observeKeyAccountCoordinatorFailure(t, ctx, 1, 11)
	ready, err = s.AccountGroupHasCandidate(ctx, key, request, 1)
	require.NoError(t, err)
	require.False(t, ready)
	effective, plan, err = s.Select(ctx, key, request)
	require.NoError(t, err)
	require.EqualValues(t, 2, plan.SelectedGroupID)
	require.Equal(t, 2.0, effective.Group.RateMultiplier)
	require.EqualValues(t, 1, *key.GroupID, "native auth snapshot must stay immutable")
}

func TestKeyAccountCoordinatorAllCoolingDoesNotUseLegacyLastResort(t *testing.T) {
	s, key, _, _, request := keyAccountCoordinatorFixture(t)
	ctx, err := s.PrepareAccountRouteContext(context.Background(), key, request)
	require.NoError(t, err)
	for _, pair := range [][2]int64{{1, 10}, {1, 11}, {2, 20}, {3, 30}} {
		observeKeyAccountCoordinatorFailure(t, ctx, pair[0], pair[1])
	}
	_, _, err = s.Select(ctx, key, request)
	require.ErrorIs(t, err, keyrouting.ErrNoSchedulableAccounts)
	other := *key
	other.ID++
	otherCtx, err := s.PrepareAccountRouteContext(context.Background(), &other, request)
	require.NoError(t, err)
	_, plan, err := s.Select(otherCtx, &other, request)
	require.NoError(t, err)
	require.EqualValues(t, 1, plan.SelectedGroupID, "a second Key must not inherit cooldown")
}

func TestKeyAccountCoordinatorDoesNotDoubleRunLegacyRules(t *testing.T) {
	s, key, _, _, request := keyAccountCoordinatorFixture(t)
	ctx, err := s.PrepareAccountRouteContext(context.Background(), key, request)
	require.NoError(t, err)
	status, err := s.RecordFailure(ctx, key, request, 1, 503)
	require.NoError(t, err)
	require.False(t, status.Triggered)
	outcome, err := s.RecordOutcome(ctx, key, request, 1, keyrouting.OutcomeFailure)
	require.NoError(t, err)
	require.False(t, outcome.Triggered)
	latency, err := s.FirstContentTimeout(ctx, key, request, 1)
	require.NoError(t, err)
	require.Zero(t, latency, "account_v2 must not start a group-level TTFT")
	identity := keyrouting.RouteFailureIdentity{APIKeyID: key.ID, GroupID: 1, Model: request.Model, Endpoint: request.Endpoint}
	cooling, _ := s.failures.IsCooling(identity, *key.KeyRouteFailurePolicy)
	require.False(t, cooling)
}

func TestKeyAccountCoordinatorSingleGroupAndUnconfiguredBoundaries(t *testing.T) {
	s, key, _, revisions, request := keyAccountCoordinatorFixture(t)
	base := context.Background()
	key.BackupGroupIDs = nil
	ctx, err := s.PrepareAccountRouteContext(base, key, request)
	require.NoError(t, err)
	require.Equal(t, base, ctx)
	require.Nil(t, GetKeyAccountRoutingSession(ctx))
	key.BackupGroupIDs = []int64{2}
	revisions.revision++
	_, err = s.PrepareAccountRouteContext(base, key, request)
	require.ErrorIs(t, err, ErrKeyAccountRouteStale)
	s.accounts = nil
	_, err = s.PrepareAccountRouteContext(base, key, request)
	require.Error(t, err)
	require.False(t, errors.Is(err, keyrouting.ErrNoSchedulableAccounts))
}

func TestKeyAccountCoordinatorTTFTNeedsAnotherAccountNotAnotherGroup(t *testing.T) {
	s, key, _, _, request := keyAccountCoordinatorFixture(t)
	key.KeyRouteFailurePolicy.Rules = append(key.KeyRouteFailurePolicy.Rules, KeyRouteFailureRule{
		Type: keyrouting.FailureRuleTTFT, Threshold: 2, SampleSize: 3, FirstContentSeconds: 5, CooldownSeconds: 60,
	})
	ctx, err := s.PrepareAccountRouteContext(context.Background(), key, request)
	require.NoError(t, err)
	// All backup accounts are cooled. B in the current group must still make
	// A's TTFT active; the old last-GROUP shortcut is not sufficient.
	observeKeyAccountCoordinatorFailure(t, ctx, 2, 20)
	observeKeyAccountCoordinatorFailure(t, ctx, 3, 30)
	wait, err := s.AccountFirstContentTimeout(ctx, key, request, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 5*time.Second, wait)
	// The probe must not persistently exclude A or spend a dispatch budget.
	require.Empty(t, GetKeyAccountRoutingSession(ctx).RequestExclusions(1))
	allowed, err := GetKeyAccountRoutingSession(ctx).Eligible(ctx, 1, 10)
	require.NoError(t, err)
	require.True(t, allowed)
	observeKeyAccountCoordinatorFailure(t, ctx, 1, 11)
	wait, err = s.AccountFirstContentTimeout(ctx, key, request, 1, 10)
	require.NoError(t, err)
	require.Zero(t, wait, "the final legal account must not be cancelled by custom TTFT")
}
