package keyrouting

import (
	"sync"
	"testing"
	"time"
)

func TestFailureTrackerSLAWaitsForFullWindowAndIgnoresNonStatisticalOutcomes(t *testing.T) {
	tracker := NewFailureTracker()
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleSLASuccessRate, Threshold: 90, SampleSize: 20, CooldownSeconds: 120}}}
	for i := 0; i < 17; i++ {
		if tracker.RecordOutcome(identity, OutcomeSuccess, policy).Triggered {
			t.Fatal("healthy partial sample must not trigger")
		}
	}
	for _, outcome := range []RouteOutcomeKind{OutcomeFailure, OutcomeTimeout} {
		if tracker.RecordOutcome(identity, outcome, policy).Triggered {
			t.Fatal("SLA must not trigger before all twenty samples exist")
		}
	}
	for i := 0; i < 5; i++ {
		if tracker.RecordOutcome(identity, OutcomeIgnored, policy).Triggered {
			t.Fatal("local rejection/cancellation must not fill the window")
		}
		if tracker.RecordOutcome(identity, RouteOutcomeKind("unknown"), policy).Triggered {
			t.Fatal("unknown outcomes must not fill the window")
		}
		if tracker.Record(identity, 503, policy).Triggered {
			t.Fatal("individual status events must not fill final group samples")
		}
	}
	got := tracker.RecordOutcome(identity, OutcomeRateLimit, policy)
	if !got.Triggered || got.Count != 3 || got.RuleIndex != 0 {
		t.Fatalf("seventeen successes and three failures must trigger SLA: %+v", got)
	}
	assertFailureTrackerIndex(t, tracker)
}

func TestFailureTrackerSLAStrictPercentageAndRecentWindow(t *testing.T) {
	tracker := NewFailureTracker()
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleSLASuccessRate, Threshold: 90, SampleSize: 20, CooldownSeconds: 120}}}
	for i := 0; i < 18; i++ {
		tracker.RecordOutcome(identity, OutcomeSuccess, policy)
	}
	tracker.RecordOutcome(identity, OutcomeFailure, policy)
	if got := tracker.RecordOutcome(identity, OutcomeFailure, policy); got.Triggered {
		t.Fatalf("exactly ninety percent must not trigger: %+v", got)
	}
	if cooling, _ := tracker.IsCooling(identity, policy); cooling {
		t.Fatal("threshold equality must stay healthy")
	}
	got := tracker.RecordOutcome(identity, OutcomeFailure, policy)
	if !got.Triggered || got.Count != 3 {
		t.Fatalf("new failure evicts oldest success and falls to 85 percent: %+v", got)
	}
	assertFailureTrackerIndex(t, tracker)
}

func TestFailureTrackerSLANonIntegralPercentAndSuccessCompletion(t *testing.T) {
	tracker := NewFailureTracker()
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleSLASuccessRate, Threshold: 67, SampleSize: 3, CooldownSeconds: 120}}}
	tracker.RecordOutcome(identity, OutcomeFailure, policy)
	tracker.RecordOutcome(identity, OutcomeSuccess, policy)
	got := tracker.RecordOutcome(identity, OutcomeSuccess, policy)
	if !got.Triggered || got.Count != 1 {
		t.Fatalf("two of three successes is below 67 percent, including a successful final sample: %+v", got)
	}
	if cooling, _ := tracker.IsCooling(identity, policy); !cooling {
		t.Fatal("success may establish cooldown for subsequent requests without rejecting that success")
	}
	assertFailureTrackerIndex(t, tracker)
}

func TestFailureTrackerSLACooldownExpiryAndRuleRevision(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	tracker := NewFailureTracker(func() time.Time { return now })
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleSLASuccessRate, Threshold: 100, SampleSize: 2, CooldownSeconds: 120}}}
	tracker.RecordOutcome(identity, OutcomeFailure, policy)
	got := tracker.RecordOutcome(identity, OutcomeSuccess, policy)
	if !got.Triggered {
		t.Fatal("completed partial-success window should trigger")
	}
	want := now.Add(120 * time.Second)
	now = now.Add(time.Second)
	if got := tracker.RecordOutcome(identity, OutcomeTimeout, policy); !got.Triggered || !got.CooldownUntil.Equal(want) {
		t.Fatalf("failure during cooldown must not extend it: %+v", got)
	}
	tracker.RecordOutcome(identity, OutcomeSuccess, policy)
	if cooling, until := tracker.IsCooling(identity, policy); !cooling || !until.Equal(want) {
		t.Fatal("late success must not cancel established cooldown")
	}
	now = want
	if cooling, _ := tracker.IsCooling(identity, policy); cooling {
		t.Fatal("SLA cooldown must expire")
	}
	if tracker.RecordOutcome(identity, OutcomeFailure, policy).Triggered {
		t.Fatal("expired cooldown must start a fresh full window")
	}
	changed := *policy.Clone()
	changed.Rules[0].Threshold = 90
	if tracker.RecordOutcome(identity, OutcomeFailure, changed).Triggered {
		t.Fatal("new SLA percentage must not inherit previous rule samples")
	}
	if tracker.RecordOutcome(identity, OutcomeFailure, policy).Triggered {
		t.Fatal("reverting rule must not revive its old samples")
	}
	tracker.Clear(identity)
	if tracker.RecordOutcome(identity, OutcomeFailure, policy).Triggered {
		t.Fatal("manual clear must remove pending SLA samples")
	}
	if cooling, _ := tracker.IsCooling(identity, RouteFailurePolicy{Rules: []RouteFailureRule{}}); cooling {
		t.Fatal("removing SLA rules must clear pending state")
	}
	assertFailureTrackerIndex(t, tracker)
}

func TestFailureTrackerSLAIdentityIsolationAndExistingRules(t *testing.T) {
	tracker := NewFailureTracker()
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{
		{Type: FailureRuleSLASuccessRate, Threshold: 100, SampleSize: 2, CooldownSeconds: 120},
		{Type: FailureRuleTimeout, Threshold: 1, SampleSize: 20, CooldownSeconds: 120},
	}}
	tracker.RecordOutcome(identity, OutcomeFailure, policy)
	for _, other := range []RouteFailureIdentity{
		{APIKeyID: 2, GroupID: 2, Model: "m", Endpoint: "e"},
		{APIKeyID: 1, GroupID: 3, Model: "m", Endpoint: "e"},
		{APIKeyID: 1, GroupID: 2, Model: "other", Endpoint: "e"},
		{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "other"},
	} {
		if tracker.RecordOutcome(other, OutcomeFailure, policy).Triggered {
			t.Fatal("SLA samples must isolate key, group, model and endpoint")
		}
	}
	other := identity
	other.APIKeyID = 3
	got := tracker.RecordOutcome(other, OutcomeTimeout, policy)
	if !got.Triggered || got.RuleIndex != 1 || got.Count != 1 {
		t.Fatalf("existing timeout rule must still fire before full SLA window: %+v", got)
	}
	tracker.ClearGroup(2)
	if tracker.RecordOutcome(identity, OutcomeFailure, policy).Triggered {
		t.Fatal("ClearGroup must remove pending SLA samples")
	}
	assertFailureTrackerIndex(t, tracker)
}

func TestFailureTrackerTriggersAfterIndependentStatusCounts(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	tracker := NewFailureTracker(func() time.Time { return now })
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "GPT-5", Endpoint: "/V1/RESPONSES"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{
		StatusCodes: []int{502, 503}, Threshold: 3, WindowSeconds: 60, CooldownSeconds: 120,
	}}}

	if got := tracker.Record(identity, 502, policy); got.Triggered {
		t.Fatalf("first status must not trigger: %+v", got)
	}
	if got := tracker.Record(identity, 503, policy); got.Triggered {
		t.Fatalf("different status must have an independent counter: %+v", got)
	}
	if got := tracker.Record(identity, 502, policy); got.Triggered {
		t.Fatalf("second 502 must not trigger: %+v", got)
	}
	got := tracker.Record(identity, 502, policy)
	if !got.Triggered || got.Count != 3 || got.RuleIndex != 0 {
		t.Fatalf("third 502 should trigger rule: %+v", got)
	}
	if cooling, until := tracker.IsCooling(identity, policy); !cooling || !until.Equal(now.Add(120*time.Second)) {
		t.Fatalf("expected 120 second cooldown, cooling=%v until=%v", cooling, until)
	}
	// A fourth failure during cooldown does not extend it.
	if got := tracker.Record(identity, 502, policy); !got.Triggered || !got.CooldownUntil.Equal(now.Add(120*time.Second)) {
		t.Fatalf("active cooldown should be returned unchanged: %+v", got)
	}
}

func TestFailureTrackerWindowAndCooldownExpire(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	tracker := NewFailureTracker(func() time.Time { return now })
	identity := RouteFailureIdentity{APIKeyID: 10, GroupID: 20, Model: "model", Endpoint: "/v1/chat/completions"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{StatusCodes: []int{502}, Threshold: 2, WindowSeconds: 10, CooldownSeconds: 5}}}

	tracker.Record(identity, 502, policy)
	now = now.Add(11 * time.Second)
	if got := tracker.Record(identity, 502, policy); got.Triggered {
		t.Fatalf("failure outside rolling window must not trigger: %+v", got)
	}
	if cooling, _ := tracker.IsCooling(identity, policy); cooling {
		t.Fatal("tracker should not be cooling before threshold")
	}
	if got := tracker.Record(identity, 502, policy); !got.Triggered {
		t.Fatalf("second failure in current window should trigger: %+v", got)
	}
	if cooling, _ := tracker.IsCooling(identity, policy); !cooling {
		t.Fatal("tracker should be cooling after threshold")
	}
	now = now.Add(6 * time.Second)
	if cooling, _ := tracker.IsCooling(identity, policy); cooling {
		t.Fatal("cooldown should expire")
	}
}

func TestFailureTrackerPolicyReplacementAndIdentityIsolation(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	tracker := NewFailureTracker(func() time.Time { return now })
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "model", Endpoint: "/v1/responses"}
	otherModel := identity
	otherModel.Model = "other-model"
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{StatusCodes: []int{503}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 60}}}
	if got := tracker.Record(identity, 503, policy); !got.Triggered {
		t.Fatal("threshold-one rule should trigger")
	}
	if cooling, _ := tracker.IsCooling(otherModel, policy); cooling {
		t.Fatal("model must isolate route counters")
	}
	if cooling, _ := tracker.IsCooling(identity, RouteFailurePolicy{}); cooling {
		t.Fatal("removing all rules must immediately clear the effective cooldown")
	}
	if cooling, _ := tracker.IsCooling(identity, policy); cooling {
		t.Fatal("removed policy must not leave stale cooldown state")
	}
	if got := tracker.Record(identity, 503, policy); !got.Triggered {
		t.Fatal("new policy should be able to trigger after replacement")
	}
	tracker.Clear(identity)
	if cooling, _ := tracker.IsCooling(identity, policy); cooling {
		t.Fatal("Clear must remove all identity state")
	}
}

func TestFailureTrackerClearGroupRemovesAllKeyState(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	tracker := NewFailureTracker(func() time.Time { return now })
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{StatusCodes: []int{502}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 60}}}
	tracker.Record(RouteFailureIdentity{APIKeyID: 1, GroupID: 7, Model: "a", Endpoint: "e"}, 502, policy)
	tracker.Record(RouteFailureIdentity{APIKeyID: 2, GroupID: 7, Model: "b", Endpoint: "e"}, 502, policy)
	tracker.Record(RouteFailureIdentity{APIKeyID: 3, GroupID: 8, Model: "c", Endpoint: "e"}, 502, policy)
	tracker.ClearGroup(7)
	if cooling, _ := tracker.IsCooling(RouteFailureIdentity{APIKeyID: 1, GroupID: 7, Model: "a", Endpoint: "e"}, policy); cooling {
		t.Fatal("group clear must remove the first key cooldown")
	}
	if cooling, _ := tracker.IsCooling(RouteFailureIdentity{APIKeyID: 2, GroupID: 7, Model: "b", Endpoint: "e"}, policy); cooling {
		t.Fatal("group clear must remove the second key cooldown")
	}
	if cooling, _ := tracker.IsCooling(RouteFailureIdentity{APIKeyID: 3, GroupID: 8, Model: "c", Endpoint: "e"}, policy); !cooling {
		t.Fatal("group clear must not affect another group")
	}
}

func TestFailureTrackerIgnoresInvalidStatusAndRules(t *testing.T) {
	tracker := NewFailureTracker()
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{
		{StatusCodes: []int{502}, Threshold: 0, WindowSeconds: 10, CooldownSeconds: 10},
		{StatusCodes: []int{503}, Threshold: 1, WindowSeconds: 10, CooldownSeconds: 10},
	}}
	if got := tracker.Record(identity, 0, policy); got.Triggered {
		t.Fatal("status 0 must be ignored")
	}
	if got := tracker.Record(identity, 502, policy); got.Triggered {
		t.Fatal("invalid rule must be ignored")
	}
	if got := tracker.Record(identity, 503, policy); !got.Triggered {
		t.Fatal("valid rule should still be evaluated")
	}
}

func TestFailureTrackerConcurrentRecord(t *testing.T) {
	tracker := NewFailureTracker()
	identity := RouteFailureIdentity{APIKeyID: 7, GroupID: 8, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{StatusCodes: []int{502}, Threshold: 100, WindowSeconds: 60, CooldownSeconds: 10}}}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.Record(identity, 502, policy)
		}()
	}
	wg.Wait()
	if cooling, _ := tracker.IsCooling(identity, policy); !cooling {
		t.Fatal("concurrent records should still reach the threshold")
	}
}

func TestFailureTrackerConsecutiveFinalOutcomes(t *testing.T) {
	tracker := NewFailureTracker()
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleConsecutiveFailures, Threshold: 3, CooldownSeconds: 120}}}
	for i := 0; i < 10; i++ {
		if tracker.Record(identity, 503, policy).Triggered {
			t.Fatal("upstream account attempts must not count as final group failures")
		}
	}
	tracker.RecordOutcome(identity, OutcomeFailure, policy)
	tracker.RecordOutcome(identity, OutcomeTimeout, policy)
	tracker.RecordOutcome(identity, OutcomeSuccess, policy)
	if tracker.RecordOutcome(identity, OutcomeRateLimit, policy).Triggered {
		t.Fatal("success must reset consecutive failure count")
	}
	tracker.RecordOutcome(identity, OutcomeIgnored, policy)
	if tracker.RecordOutcome(identity, OutcomeFailure, policy).Triggered {
		t.Fatal("only two final failures since success")
	}
	got := tracker.RecordOutcome(identity, OutcomeTimeout, policy)
	if !got.Triggered || got.Count != 3 {
		t.Fatalf("third final failure must trigger, ignored must not reset: %+v", got)
	}
}

func TestFailureTrackerRollingOutcomeSamples(t *testing.T) {
	for _, tc := range []struct {
		kind    string
		failure RouteOutcomeKind
	}{
		{FailureRuleTimeout, OutcomeTimeout},
		{FailureRuleRateLimit, OutcomeRateLimit},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			tracker := NewFailureTracker()
			identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
			policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: tc.kind, Threshold: 2, SampleSize: 3, CooldownSeconds: 120}}}
			tracker.RecordOutcome(identity, tc.failure, policy)
			tracker.RecordOutcome(identity, OutcomeSuccess, policy)
			tracker.RecordOutcome(identity, OutcomeFailure, policy)
			if tracker.RecordOutcome(identity, tc.failure, policy).Triggered {
				t.Fatal("success and other real results must evict an older matching failure")
			}
			tracker.RecordOutcome(identity, OutcomeIgnored, policy)
			got := tracker.RecordOutcome(identity, tc.failure, policy)
			if !got.Triggered || got.Count != 2 {
				t.Fatalf("two failures in last three real results must trigger: %+v", got)
			}
		})
	}
}

func TestFailureTrackerSemanticPolicyChangesAndIdentityIsolation(t *testing.T) {
	tracker := NewFailureTracker()
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleConsecutiveFailures, Threshold: 2, CooldownSeconds: 120}}}
	tracker.RecordOutcome(identity, OutcomeFailure, policy)
	for _, other := range []RouteFailureIdentity{
		{APIKeyID: 2, GroupID: 2, Model: "m", Endpoint: "e"},
		{APIKeyID: 1, GroupID: 3, Model: "m", Endpoint: "e"},
		{APIKeyID: 1, GroupID: 2, Model: "other", Endpoint: "e"},
		{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "other"},
	} {
		if tracker.RecordOutcome(other, OutcomeFailure, policy).Triggered {
			t.Fatal("key, group, model and endpoint must isolate counts")
		}
	}
	changed := *policy.Clone()
	changed.Rules[0].Threshold = 3
	if tracker.RecordOutcome(identity, OutcomeFailure, changed).Triggered {
		t.Fatal("new rule revision must not reuse old samples")
	}
	if tracker.RecordOutcome(identity, OutcomeFailure, policy).Triggered {
		t.Fatal("reverting a rule must not revive stale revision samples")
	}
	tracker.ClearGroup(2)
	if tracker.RecordOutcome(identity, OutcomeFailure, policy).Triggered {
		t.Fatal("ClearGroup must remove semantic counters")
	}
	tracker.Clear(identity)
	if tracker.RecordOutcome(identity, OutcomeFailure, policy).Triggered {
		t.Fatal("Clear must remove semantic counters")
	}
}

func TestFailureTrackerCooldownCannotBeExtendedByDifferentRules(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	tracker := NewFailureTracker(func() time.Time { return now })
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{
		{StatusCodes: []int{502}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 10},
		{StatusCodes: []int{503}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 600},
		{Type: FailureRuleConsecutiveFailures, Threshold: 1, CooldownSeconds: 120},
	}}
	want := now.Add(10 * time.Second)
	tracker.Record(identity, 502, policy)
	now = now.Add(time.Second)
	if got := tracker.Record(identity, 503, policy); !got.CooldownUntil.Equal(want) {
		t.Fatalf("another status must not extend cooldown: %+v", got)
	}
	if got := tracker.RecordOutcome(identity, OutcomeFailure, policy); !got.CooldownUntil.Equal(want) {
		t.Fatalf("semantic outcome must not extend cooldown: %+v", got)
	}
	tracker.RecordOutcome(identity, OutcomeSuccess, policy)
	if cooling, until := tracker.IsCooling(identity, policy); !cooling || !until.Equal(want) {
		t.Fatal("in-flight success must not cancel cooldown")
	}
	now = want
	if cooling, _ := tracker.IsCooling(identity, policy); cooling {
		t.Fatal("cooldown should expire at original deadline")
	}
}

func TestFailureTrackerSemanticRulesIgnoreUnknownOutcomeAndEarlyThreshold(t *testing.T) {
	tracker := NewFailureTracker()
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleTimeout, Threshold: 1, SampleSize: 20, CooldownSeconds: 120}}}
	if tracker.RecordOutcome(identity, RouteOutcomeKind("unknown"), policy).Triggered {
		t.Fatal("unknown outcome must be ignored")
	}
	if !tracker.RecordOutcome(identity, OutcomeTimeout, policy).Triggered {
		t.Fatal("threshold should apply to available samples without waiting for twenty outcomes")
	}
	if cooling, _ := tracker.IsCooling(identity, RouteFailurePolicy{Rules: []RouteFailureRule{}}); cooling {
		t.Fatal("empty explicit policy must clear semantic cooldown")
	}
	if cooling, _ := tracker.IsCooling(identity, policy); cooling {
		t.Fatal("removed policy must not revive semantic cooldown")
	}
}

func assertFailureTrackerIndex(t *testing.T, tracker *FailureTracker) {
	t.Helper()
	count := 0
	for identity, state := range tracker.identities {
		if len(state.keys) == 0 {
			t.Fatal("empty identity must not consume capacity")
		}
		for key := range state.keys {
			if key.identity != identity {
				t.Fatal("counter indexed under a different identity")
			}
			if len(tracker.events[key]) == 0 && len(tracker.outcomes[key]) == 0 && tracker.cooldowns[key].IsZero() {
				t.Fatal("indexed counter has no retained state")
			}
			count++
		}
	}
	if count != tracker.counterCount || count != len(tracker.events)+len(tracker.outcomes)+len(tracker.cooldowns) {
		t.Fatalf("index and counter capacity accounting differ: indexed=%d reserved=%d events=%d outcomes=%d cooldowns=%d", count, tracker.counterCount, len(tracker.events), len(tracker.outcomes), len(tracker.cooldowns))
	}
}

func TestFailureTrackerBoundsIdentitiesWithoutRejectingExistingCounters(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	tracker := NewFailureTracker(func() time.Time { return now })
	tracker.maxIdentities = 2
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleConsecutiveFailures, Threshold: 2, CooldownSeconds: 120}}}
	a := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "a", Endpoint: "e"}
	b, c := a, a
	b.Model, c.Model = "b", "c"
	tracker.RecordOutcome(a, OutcomeFailure, policy)
	tracker.RecordOutcome(b, OutcomeFailure, policy)
	for i := 0; i < 10; i++ {
		if tracker.RecordOutcome(c, OutcomeFailure, policy).Triggered {
			t.Fatal("new identities at capacity must fail open without creating a cooldown")
		}
	}
	if len(tracker.identities) != 2 || tracker.counterCount != 2 {
		t.Fatal("identity limit was exceeded")
	}
	if !tracker.RecordOutcome(a, OutcomeFailure, policy).Triggered {
		t.Fatal("existing counters must continue working while at capacity")
	}
	tracker.Clear(a)
	if tracker.RecordOutcome(c, OutcomeFailure, policy).Triggered {
		t.Fatal("a newly admitted identity must not inherit the previously dropped outcomes")
	}
	if tracker.identities[c] == nil {
		t.Fatal("clearing an identity must release its capacity")
	}
	assertFailureTrackerIndex(t, tracker)
}

func TestFailureTrackerBoundsCountersAndReleasesOnPolicyChanges(t *testing.T) {
	tracker := NewFailureTracker()
	tracker.maxCounters = 2
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{StatusCodes: []int{502, 503, 504}, Threshold: 2, WindowSeconds: 60, CooldownSeconds: 120}}}
	tracker.Record(identity, 502, policy)
	tracker.Record(identity, 503, policy)
	for i := 0; i < 10; i++ {
		if tracker.Record(identity, 504, policy).Triggered {
			t.Fatal("new status counters at capacity must not be retained")
		}
	}
	if tracker.counterCount != 2 || len(tracker.events) != 2 {
		t.Fatal("counter limit was exceeded")
	}
	if !tracker.Record(identity, 502, policy).Triggered {
		t.Fatal("existing counters must continue reaching their threshold")
	}
	assertFailureTrackerIndex(t, tracker)
	// A policy replacement releases all old revisions and permits new ones.
	replacement := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleTimeout, Threshold: 2, SampleSize: 20, CooldownSeconds: 120}}}
	tracker.RecordOutcome(identity, OutcomeTimeout, replacement)
	if tracker.counterCount != 1 || len(tracker.outcomes) != 1 || len(tracker.cooldowns) != 0 {
		t.Fatal("policy replacement must release stale counters and cooldowns")
	}
	tracker.ClearGroup(identity.GroupID)
	if tracker.counterCount != 0 || len(tracker.identities) != 0 {
		t.Fatal("group clearing must release all memory accounting")
	}
	assertFailureTrackerIndex(t, tracker)
}

func TestFailureTrackerIdleTTLAndReadOnlyQueriesDoNotAllocate(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	tracker := NewFailureTracker(func() time.Time { return now })
	tracker.maxIdentities = 1
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleTimeout, Threshold: 2, SampleSize: 20, CooldownSeconds: 120}}}
	a := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "a", Endpoint: "e"}
	b := a
	b.Model = "b"
	tracker.RecordOutcome(a, OutcomeTimeout, policy)
	for i := 0; i < 20; i++ {
		probe := a
		probe.Model = stringInt(i)
		tracker.IsCooling(probe, policy)
	}
	if len(tracker.identities) != 1 {
		t.Fatal("availability probes must not allocate arbitrary model identities")
	}
	now = now.Add(23 * time.Hour)
	tracker.RecordOutcome(a, OutcomeSuccess, policy)
	if !tracker.RecordOutcome(a, OutcomeTimeout, policy).Triggered {
		t.Fatal("recent-N samples must remain valid while the identity is active, not expire on a short timer")
	}
	now = now.Add(24 * time.Hour)
	if tracker.RecordOutcome(b, OutcomeTimeout, policy).Triggered {
		t.Fatal("idle identity eviction must not transfer its samples to a new identity")
	}
	if tracker.identities[a] != nil || tracker.identities[b] == nil {
		t.Fatal("24-hour idle state must be reclaimed before enforcing the identity limit")
	}
	assertFailureTrackerIndex(t, tracker)
}

func TestFailureTrackerSweepReclaimsExpiredCooldownAndSuccessCounters(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	tracker := NewFailureTracker(func() time.Time { return now })
	tracker.maxIdentities = 1
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 2, Model: "m", Endpoint: "e"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleConsecutiveFailures, Threshold: 2, CooldownSeconds: 1}}}
	tracker.RecordOutcome(identity, OutcomeFailure, policy)
	tracker.RecordOutcome(identity, OutcomeSuccess, policy)
	if tracker.counterCount != 0 || len(tracker.identities) != 0 {
		t.Fatal("successful reset must release an otherwise empty consecutive counter")
	}
	tracker.RecordOutcome(identity, OutcomeFailure, policy)
	tracker.RecordOutcome(identity, OutcomeFailure, policy)
	// Another model forces a capacity sweep even before the normal minute.
	now = now.Add(2 * time.Second)
	other := identity
	other.Model = "other"
	tracker.RecordOutcome(other, OutcomeFailure, policy)
	if tracker.identities[identity] != nil || tracker.identities[other] == nil {
		t.Fatal("expired cooldown must not unnecessarily consume bounded capacity")
	}
	assertFailureTrackerIndex(t, tracker)
}
