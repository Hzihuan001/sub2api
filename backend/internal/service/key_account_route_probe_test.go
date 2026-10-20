package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/stretchr/testify/require"
)

func keyAccountProbeFixture(t *testing.T) (*KeyRoutingService, *APIKey, *keyRoutingAccounts, *accountRuntimeRevisionStub, *accountRuntimeBatchStore, KeyRoutingRequest) {
	t.Helper()
	s, key, accounts, _, request := keyAccountCoordinatorFixture(t)
	key.KeyRouteFailurePolicy.Rules = append(key.KeyRouteFailurePolicy.Rules, KeyRouteFailureRule{
		Type: keyrouting.FailureRuleTTFT, Threshold: 2, SampleSize: 3, FirstContentSeconds: 5, CooldownSeconds: 60,
	})
	revisions := &accountRuntimeRevisionStub{keyRevision: 100}
	store := &accountRuntimeBatchStore{accountRuntimeStoreStub: &accountRuntimeStoreStub{
		inner: keyrouting.NewMemoryAccountHealthStore(keyrouting.AccountHealthOptions{}),
	}}
	s.accounts = NewKeyAccountRoutingRuntime(store, revisions, keyAccountCoordinatorAccounts{accounts}, KeyAccountRoutingRuntimeOptions{Site: "probe-site"})
	return s, key, accounts, revisions, store, request
}

func TestKeyAccountRouteProbeForwardsCachedCandidateAdmission(t *testing.T) {
	session, revisions, _, lister, _, _ := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	store := &accountRuntimeBatchStore{accountRuntimeStoreStub: &accountRuntimeStoreStub{inner: session.runtime.store}}
	session.runtime.store = store
	probe := &keyAccountRouteProbeGate{inner: session, excludedAccount: 41}
	ctx := WithKeyAccountRouteGate(context.Background(), probe)
	groupID := int64(1)
	reads := revisions.keyReads
	excluded, err := keyAccountRouteSchedulingExclusions(ctx, &groupID, nil)
	require.NoError(t, err)
	require.Contains(t, excluded, int64(41))
	for i := 0; i < 32; i++ {
		allowed, err := keyAccountRouteCandidateAdmission(ctx, &groupID, 41)
		require.NoError(t, err)
		require.False(t, allowed)
		allowed, err = keyAccountRouteCandidateAdmission(ctx, &groupID, 42)
		require.NoError(t, err)
		require.True(t, allowed)
	}
	require.Equal(t, reads+1, revisions.keyReads, "pool scan must validate once, not once per candidate")
	require.Equal(t, 1, store.batchCalls)
	require.Zero(t, store.getCalls)
	require.Equal(t, 1, lister.calls)
	require.Empty(t, session.RequestExclusions(1), "probe-only exclusion escaped into the real route")
	require.Zero(t, session.budget.Dispatched())
	require.Zero(t, store.observeCalls)
	// The separate final-admission API must retain its fresh contract.
	allowed, err := probe.Eligible(ctx, 1, 42)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, reads+2, revisions.keyReads)
	require.Equal(t, 1, store.getCalls)
}

func TestKeyAccountRouteProbeRequiresTwoPermitsAndTimeForTimer(t *testing.T) {
	s, key, _, revisions, store, request := keyAccountProbeFixture(t)
	ctx, err := s.PrepareAccountRouteContext(context.Background(), key, request)
	require.NoError(t, err)
	session := GetKeyAccountRoutingSession(ctx)
	initialReads := revisions.keyReads
	session.budget, err = keyrouting.NewAccountRouteBudget(1, time.Minute)
	require.NoError(t, err)
	wait, err := s.AccountFirstContentTimeout(ctx, key, request, 1, 10)
	require.NoError(t, err)
	require.Zero(t, wait, "last dispatch permit cannot have an active replacement timer")
	require.Equal(t, initialReads, revisions.keyReads)
	require.Zero(t, store.batchCalls)

	session.budget, err = keyrouting.NewAccountRouteBudget(2, 4*time.Second)
	require.NoError(t, err)
	wait, err = s.AccountFirstContentTimeout(ctx, key, request, 1, 10)
	require.NoError(t, err)
	require.Zero(t, wait, "a five-second timer outliving the route deadline cannot have a replacement")
	require.Zero(t, store.batchCalls)

	session.budget, err = keyrouting.NewAccountRouteBudget(2, time.Minute)
	require.NoError(t, err)
	wait, err = s.AccountFirstContentTimeout(ctx, key, request, 1, 10)
	require.NoError(t, err)
	require.Equal(t, 5*time.Second, wait)
	require.Zero(t, session.budget.Dispatched())
	require.Equal(t, 1, store.batchCalls)
	require.Zero(t, store.observeCalls)
	metadata, err := session.Metadata(1, 10, request.Model, PlatformDeepseek)
	require.NoError(t, err)
	require.NoError(t, session.TryDispatch(ctx, metadata))
	wait, err = s.AccountFirstContentTimeout(ctx, key, request, 1, 10)
	require.NoError(t, err)
	require.Zero(t, wait)
	require.Equal(t, 1, session.budget.Dispatched())
	require.Equal(t, 1, store.batchCalls)
	require.Zero(t, store.observeCalls, "readiness must never create an attempt sample")
}

func TestKeyAccountRouteProbeConservativeFailedPathDoesNotChangeRealRouting(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failedGroup int64
		wantTimer   time.Duration
	}{
		{name: "cross-group uncertain mapping", failedGroup: 1},
		{name: "same-group native retry remains candidate", failedGroup: 2, wantTimer: 5 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, key, accounts, _, store, request := keyAccountProbeFixture(t)
			key.BackupGroupIDs = []int64{2}
			accounts.accounts[1] = accounts.accounts[1][:1]
			ctx, err := s.PrepareAccountRouteContext(context.Background(), key, request)
			require.NoError(t, err)
			session := GetKeyAccountRoutingSession(ctx)
			failed := keyrouting.AccountRouteFingerprint{AccountID: 20, UpstreamModel: "previous-mapping", Platform: PlatformDeepseek, Endpoint: request.Endpoint}
			session.budget.RecordFailure(tc.failedGroup, failed)
			wait, err := s.AccountFirstContentTimeout(ctx, key, request, 1, 10)
			require.NoError(t, err)
			require.Equal(t, tc.wantTimer, wait)
			// Native model mapping is not duplicated by the probe. A distinct
			// mapped path stays eligible for actual routing even when the
			// advisory timer is conservatively disabled.
			metadata, err := session.Metadata(2, 20, "different-real-mapping", PlatformDeepseek)
			require.NoError(t, err)
			require.NoError(t, session.CanDispatch(ctx, metadata))
			require.Empty(t, session.RequestExclusions(1))
			require.Empty(t, session.RequestExclusions(2))
			require.Zero(t, session.budget.Dispatched())
			require.Zero(t, store.observeCalls)
		})
	}
}

func TestKeyAccountRouteProbeLargePoolUsesOneBatchAndRevisionRead(t *testing.T) {
	s, key, accounts, revisions, store, request := keyAccountProbeFixture(t)
	selected := accounts.accounts[1][0]
	accounts.accounts[1] = []Account{selected}
	for id := int64(11); id <= 42; id++ {
		other := selected
		other.ID = id
		accounts.accounts[1] = append(accounts.accounts[1], other)
	}
	ctx, err := s.PrepareAccountRouteContext(context.Background(), key, request)
	require.NoError(t, err)
	session := GetKeyAccountRoutingSession(ctx)
	for id := int64(11); id < 42; id++ {
		session.budget.RecordFailure(2, keyrouting.AccountRouteFingerprint{AccountID: id, UpstreamModel: request.Model, Platform: PlatformDeepseek, Endpoint: request.Endpoint})
	}
	before := revisions.keyReads
	wait, err := s.AccountFirstContentTimeout(ctx, key, request, 1, selected.ID)
	require.NoError(t, err)
	require.Equal(t, 5*time.Second, wait, "last healthy peer remains a replacement")
	require.Equal(t, before+1, revisions.keyReads, "the probe scanned thirty-two peers but may validate SQL only at group entry")
	require.Equal(t, 1, store.batchCalls)
	require.Zero(t, store.getCalls, "CandidateEligible must preserve cached scan hints")
	require.Zero(t, store.observeCalls)
	require.Zero(t, session.budget.Dispatched())
	require.Empty(t, session.RequestExclusions(1))
}
