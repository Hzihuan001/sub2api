package keyrouting

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func accountHealthTestID() AccountHealthIdentity {
	return AccountHealthIdentity{Site: "synthetic-site", APIKeyID: 7, GroupID: 11, AccountID: 19, Model: "public-model", Endpoint: "/v1/responses", PolicyRevision: 1}
}

func accountHealthTestPolicy(rule RouteFailureRule) RouteFailurePolicy {
	return RouteFailurePolicy{Rules: []RouteFailureRule{rule}}
}

func accountHealthTestAttempt(id string, now time.Time, kind RouteOutcomeKind, status int) AccountHealthAttempt {
	return AccountHealthAttempt{AttemptID: id, Dispatched: true, StartedAt: now.Add(-time.Second), FinishedAt: now, Outcome: kind, StatusCode: status}
}

func TestAccountHealthStatusCodesAreIndependent(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	store := NewMemoryAccountHealthStore(AccountHealthOptions{Clock: func() time.Time { return now }})
	policy := accountHealthTestPolicy(RouteFailureRule{StatusCodes: []int{502, 503}, Threshold: 2, WindowSeconds: 60, CooldownSeconds: 600})
	id := accountHealthTestID()
	for index, status := range []int{502, 503, 502} {
		now = now.Add(time.Millisecond)
		result, err := store.Observe(context.Background(), id, policy, accountHealthTestAttempt(fmt.Sprint(index), now, OutcomeFailure, status))
		require.NoError(t, err)
		require.Equal(t, index == 2, result.Cooling)
		if index == 2 {
			require.Equal(t, 2, result.Count)
			require.Equal(t, now.Add(600*time.Second), result.CooldownUntil)
		}
	}
}

func TestAccountHealthSixRuleKinds(t *testing.T) {
	tests := []struct {
		name     string
		rule     RouteFailureRule
		outcomes []RouteOutcomeKind
		content  []time.Duration
	}{
		{"consecutive", RouteFailureRule{Type: FailureRuleConsecutiveFailures, Threshold: 2, CooldownSeconds: 60}, []RouteOutcomeKind{OutcomeFailure, OutcomeSuccess, OutcomeFailure, OutcomeRateLimit}, nil},
		{"timeouts", RouteFailureRule{Type: FailureRuleTimeout, Threshold: 2, SampleSize: 3, CooldownSeconds: 60}, []RouteOutcomeKind{OutcomeTimeout, OutcomeSuccess, OutcomeTimeout}, nil},
		{"rate_limit", RouteFailureRule{Type: FailureRuleRateLimit, Threshold: 2, SampleSize: 3, CooldownSeconds: 60}, []RouteOutcomeKind{OutcomeRateLimit, OutcomeSuccess, OutcomeRateLimit}, nil},
		{"sla_full_window", RouteFailureRule{Type: FailureRuleSLASuccessRate, Threshold: 80, SampleSize: 3, CooldownSeconds: 60}, []RouteOutcomeKind{OutcomeFailure, OutcomeFailure, OutcomeSuccess}, nil},
		{"ttft", RouteFailureRule{Type: FailureRuleTTFT, Threshold: 2, SampleSize: 3, FirstContentSeconds: 5, CooldownSeconds: 60}, []RouteOutcomeKind{OutcomeSuccess, OutcomeFailure, OutcomeSuccess}, []time.Duration{6 * time.Second, -1, 6 * time.Second}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
			store := NewMemoryAccountHealthStore(AccountHealthOptions{Clock: func() time.Time { return now }})
			for index, kind := range test.outcomes {
				now = now.Add(time.Second)
				attempt := accountHealthTestAttempt(fmt.Sprint(index), now, kind, 0)
				if len(test.content) > 0 && test.content[index] >= 0 {
					duration := test.content[index]
					attempt.FirstContent = &duration
				}
				result, err := store.Observe(context.Background(), accountHealthTestID(), accountHealthTestPolicy(test.rule), attempt)
				require.NoError(t, err)
				require.Equal(t, index == len(test.outcomes)-1, result.Cooling)
			}
		})
	}
}

func TestAccountHealthDedupLateSuccessAndCooldownRestart(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	store := NewMemoryAccountHealthStore(AccountHealthOptions{Clock: func() time.Time { return now }})
	id := accountHealthTestID()
	policy := accountHealthTestPolicy(RouteFailureRule{Type: FailureRuleConsecutiveFailures, Threshold: 2, CooldownSeconds: 60})
	one := accountHealthTestAttempt("same", now, OutcomeFailure, 503)
	result, err := store.Observe(context.Background(), id, policy, one)
	require.NoError(t, err)
	require.True(t, result.Accepted)
	require.False(t, result.Cooling)
	result, err = store.Observe(context.Background(), id, policy, one)
	require.NoError(t, err)
	require.True(t, result.Duplicate)
	require.False(t, result.Cooling)
	now = now.Add(time.Second)
	result, err = store.Observe(context.Background(), id, policy, accountHealthTestAttempt("second", now, OutcomeFailure, 503))
	require.NoError(t, err)
	require.True(t, result.Cooling)
	until := result.CooldownUntil
	now = now.Add(5 * time.Second)
	result, err = store.Observe(context.Background(), id, policy, accountHealthTestAttempt("late-success", now, OutcomeSuccess, 200))
	require.NoError(t, err)
	require.True(t, result.Cooling)
	require.Equal(t, until, result.CooldownUntil)
	now = until.Add(time.Second)
	attempt := accountHealthTestAttempt("late-old-failure", now, OutcomeFailure, 503)
	attempt.StartedAt = until.Add(-time.Minute)
	result, err = store.Observe(context.Background(), id, policy, attempt)
	require.NoError(t, err)
	require.False(t, result.Cooling)
	now = now.Add(2 * time.Second)
	result, err = store.Observe(context.Background(), id, policy, accountHealthTestAttempt("fresh", now, OutcomeFailure, 503))
	require.NoError(t, err)
	require.False(t, result.Cooling)
}

func TestAccountHealthRevisionFenceAndPolicyMutation(t *testing.T) {
	now := time.Now().UTC()
	store := NewMemoryAccountHealthStore(AccountHealthOptions{Clock: func() time.Time { return now }})
	id := accountHealthTestID()
	policy := accountHealthTestPolicy(RouteFailureRule{Type: FailureRuleConsecutiveFailures, Threshold: 1, CooldownSeconds: 60})
	result, err := store.Observe(context.Background(), id, policy, accountHealthTestAttempt("old", now, OutcomeFailure, 503))
	require.NoError(t, err)
	require.True(t, result.Cooling)
	changed := policy.Clone()
	changed.Rules[0].Threshold = 2
	_, err = store.Observe(context.Background(), id, *changed, accountHealthTestAttempt("mismatched", now, OutcomeFailure, 503))
	require.ErrorIs(t, err, ErrAccountHealthPolicyConflict)
	newID := id
	newID.PolicyRevision++
	require.NoError(t, store.Fence(context.Background(), newID))
	_, err = store.Observe(context.Background(), id, policy, accountHealthTestAttempt("late", now, OutcomeFailure, 503))
	require.ErrorIs(t, err, ErrAccountHealthStaleRevision)
	_, err = store.Get(context.Background(), id)
	require.ErrorIs(t, err, ErrAccountHealthStaleRevision)
	result, err = store.Get(context.Background(), newID)
	require.NoError(t, err)
	require.False(t, result.Cooling)
	// Same-revision Fence is idempotent, not a way to undo current health.
	result, err = store.Observe(context.Background(), newID, policy, accountHealthTestAttempt("new", now, OutcomeFailure, 503))
	require.NoError(t, err)
	require.True(t, result.Cooling)
	require.NoError(t, store.Fence(context.Background(), newID))
	result, err = store.Get(context.Background(), newID)
	require.NoError(t, err)
	require.True(t, result.Cooling)
}

func TestAccountHealthIdentityIsolation(t *testing.T) {
	store := NewMemoryAccountHealthStore(AccountHealthOptions{})
	id := accountHealthTestID()
	policy := accountHealthTestPolicy(RouteFailureRule{Type: FailureRuleConsecutiveFailures, Threshold: 1, CooldownSeconds: 60})
	_, err := store.Observe(context.Background(), id, policy, accountHealthTestAttempt("one", time.Now(), OutcomeFailure, 503))
	require.NoError(t, err)
	for _, mutate := range []func(*AccountHealthIdentity){func(i *AccountHealthIdentity) { i.Site += "-other" }, func(i *AccountHealthIdentity) { i.APIKeyID++ }, func(i *AccountHealthIdentity) { i.GroupID++ }, func(i *AccountHealthIdentity) { i.AccountID++ }, func(i *AccountHealthIdentity) { i.Model += "-other" }, func(i *AccountHealthIdentity) { i.Endpoint = "/v1/chat/completions" }} {
		other := id
		mutate(&other)
		result, err := store.Get(context.Background(), other)
		require.NoError(t, err)
		require.False(t, result.Cooling)
	}
}

func TestAccountHealthNoDispatchIgnoredAndLocalTTFT(t *testing.T) {
	store := NewMemoryAccountHealthStore(AccountHealthOptions{})
	id := accountHealthTestID()
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleTimeout, Threshold: 1, SampleSize: 1, CooldownSeconds: 600}, {Type: FailureRuleTTFT, Threshold: 1, SampleSize: 1, FirstContentSeconds: 5, CooldownSeconds: 60}}}
	for _, attempt := range []AccountHealthAttempt{{Outcome: OutcomeFailure}, {Dispatched: true, Outcome: OutcomeIgnored}} {
		result, err := store.Observe(context.Background(), id, policy, attempt)
		require.NoError(t, err)
		require.False(t, result.Accepted)
		require.False(t, result.Cooling)
	}
	attempt := accountHealthTestAttempt("local-ttft", time.Now(), OutcomeTimeout, 0)
	attempt.LocalFirstContentTimeout, attempt.FirstContentExpired = true, true
	result, err := store.Observe(context.Background(), id, policy, attempt)
	require.NoError(t, err)
	require.True(t, result.Cooling)
	require.Equal(t, 1, result.RuleIndex, "local TTFT is not a provider timeout")
	attempt.StatusCode = 504
	_, err = store.Observe(context.Background(), id, policy, attempt)
	require.ErrorIs(t, err, ErrAccountHealthAttempt, "do not manufacture a provider HTTP status for local expiry")
}

func TestAccountHealthStatusWindowAndSLABoundary(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	store := NewMemoryAccountHealthStore(AccountHealthOptions{Clock: func() time.Time { return now }})
	id := accountHealthTestID()
	policy := accountHealthTestPolicy(RouteFailureRule{StatusCodes: []int{503}, Threshold: 2, WindowSeconds: 60, CooldownSeconds: 60})
	_, err := store.Observe(context.Background(), id, policy, accountHealthTestAttempt("first", now, OutcomeFailure, 503))
	require.NoError(t, err)
	now = now.Add(60 * time.Second)
	result, err := store.Observe(context.Background(), id, policy, accountHealthTestAttempt("boundary", now, OutcomeFailure, 503))
	require.NoError(t, err)
	require.False(t, result.Cooling)
	id.AccountID++
	policy = accountHealthTestPolicy(RouteFailureRule{Type: FailureRuleSLASuccessRate, Threshold: 50, SampleSize: 2, CooldownSeconds: 60})
	for index, kind := range []RouteOutcomeKind{OutcomeFailure, OutcomeSuccess} {
		now = now.Add(time.Millisecond)
		result, err = store.Observe(context.Background(), id, policy, accountHealthTestAttempt(fmt.Sprint(index), now, kind, 0))
		require.NoError(t, err)
		require.False(t, result.Cooling, "equal-to SLA threshold is not below it")
	}
}

func TestAccountHealthCapacityTTLAndCancellation(t *testing.T) {
	now := time.Now().UTC()
	store := NewMemoryAccountHealthStore(AccountHealthOptions{Clock: func() time.Time { return now }, MaxIdentities: 2, MaxIdentitiesPerKey: 1, MaxAttempts: 1})
	id := accountHealthTestID()
	_, err := store.Get(context.Background(), id)
	require.NoError(t, err)
	other := id
	other.AccountID++
	_, err = store.Get(context.Background(), other)
	require.ErrorIs(t, err, ErrAccountHealthCapacity)
	other.APIKeyID++
	_, err = store.Get(context.Background(), other)
	require.NoError(t, err)
	other.APIKeyID++
	_, err = store.Get(context.Background(), other)
	require.ErrorIs(t, err, ErrAccountHealthCapacity)
	policy := accountHealthTestPolicy(RouteFailureRule{Type: FailureRuleConsecutiveFailures, Threshold: 3, CooldownSeconds: 60})
	_, err = store.Observe(context.Background(), id, policy, accountHealthTestAttempt("one", now, OutcomeFailure, 503))
	require.NoError(t, err)
	_, err = store.Observe(context.Background(), id, policy, accountHealthTestAttempt("two", now, OutcomeFailure, 503))
	require.ErrorIs(t, err, ErrAccountHealthCapacity)
	now = now.Add(16 * time.Minute)
	_, err = store.Observe(context.Background(), id, policy, accountHealthTestAttempt("two", now, OutcomeFailure, 503))
	require.NoError(t, err)
	now = now.Add(25 * time.Hour)
	_, err = store.Get(context.Background(), other)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.Get(ctx, id)
	require.ErrorIs(t, err, context.Canceled)
}

func TestAccountHealthSiteBucketLimitNeverExceedsSiteQuota(t *testing.T) {
	opts := AccountHealthOptions{MaxIdentitiesPerSite: 10, SiteBucketCount: 3}.Normalized()
	if got := opts.SiteBucketLimit(); got != 3 {
		t.Fatalf("unexpected per-bucket limit: got %d want 3", got)
	}
	if got := opts.SiteBucketLimit() * opts.SiteBucketCount; got > opts.MaxIdentitiesPerSite {
		t.Fatalf("bucket quotas exceed site quota: got %d want <= %d", got, opts.MaxIdentitiesPerSite)
	}
}

func TestAccountHealthConcurrentDuplicateAndPureCopy(t *testing.T) {
	now := time.Now().UTC()
	store := NewMemoryAccountHealthStore(AccountHealthOptions{Clock: func() time.Time { return now }})
	id := accountHealthTestID()
	policy := accountHealthTestPolicy(RouteFailureRule{Type: FailureRuleConsecutiveFailures, Threshold: 2, CooldownSeconds: 60})
	var wg sync.WaitGroup
	results := make(chan AccountHealthSnapshot, 32)
	errs := make(chan error, 32)
	for n := 0; n < 32; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := store.Observe(context.Background(), id, policy, accountHealthTestAttempt("duplicate", now, OutcomeFailure, 503))
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	accepted := 0
	for result := range results {
		if result.Accepted {
			accepted++
		}
		require.False(t, result.Cooling)
	}
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, accepted)
	state := store.entries[id.StorageKey()].state
	next, _, err := PrepareAccountHealth(state, id, now.Add(time.Second), AccountHealthOptions{})
	require.NoError(t, err)
	next.Seen["mutate"] = now.UnixMilli()
	require.NotContains(t, state.Seen, "mutate")
	bad := *state
	bad.Schema = 2
	_, _, err = PrepareAccountHealth(&bad, id, now, AccountHealthOptions{})
	require.True(t, errors.Is(err, ErrAccountHealthCorrupt))
}

func TestAccountHealthExpiredReplayRequiresFrozenCompletionTime(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	store := NewMemoryAccountHealthStore(AccountHealthOptions{Clock: func() time.Time { return now }})
	id := accountHealthTestID()
	policy := accountHealthTestPolicy(RouteFailureRule{Type: FailureRuleConsecutiveFailures, Threshold: 2, CooldownSeconds: 60})
	attempt := accountHealthTestAttempt("frozen-terminal", now, OutcomeFailure, 503)
	attempt.StartedAt = now.Add(-10 * time.Hour)
	result, err := store.Observe(context.Background(), id, policy, attempt)
	require.NoError(t, err)
	require.True(t, result.Accepted, "a ten-hour request has just completed and is not stale")
	require.False(t, result.Cooling)
	now = now.Add(16 * time.Minute)
	result, err = store.Observe(context.Background(), id, policy, attempt)
	require.NoError(t, err)
	require.False(t, result.Accepted, "expired terminal event cannot re-enter after dedup expiry")
	require.False(t, result.Cooling)
	attempt.FinishedAt = time.Time{}
	_, err = store.Observe(context.Background(), id, policy, attempt)
	require.ErrorIs(t, err, ErrAccountHealthAttempt, "missing completion time cannot default to now and bypass acceptance fencing")
}
