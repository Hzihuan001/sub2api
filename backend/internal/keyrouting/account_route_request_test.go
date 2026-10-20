package keyrouting

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestAccountRouteBudgetDoesNotResetAcrossGroups(t *testing.T) {
	now := time.Now()
	b, err := newAccountRouteBudget(3, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	path := AccountRouteFingerprint{AccountID: 1, UpstreamModel: "model", Platform: "openai", Endpoint: "/v1/responses"}
	for _, group := range []int64{1, 2, 3} {
		if err := b.TryDispatch(group, path); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.TryDispatch(4, path); !errors.Is(err, ErrAccountRouteAttemptBudget) {
		t.Fatalf("got %v", err)
	}
	if b.Dispatched() != 3 {
		t.Fatalf("dispatched=%d", b.Dispatched())
	}
}

func TestAccountRouteBudgetCrossGroupFingerprint(t *testing.T) {
	b, _ := NewAccountRouteBudget(20, time.Minute)
	path := AccountRouteFingerprint{AccountID: 1, UpstreamModel: "Model-A", Platform: " OpenAI ", Endpoint: "/v1/responses"}
	if err := b.TryDispatch(1, path); err != nil {
		t.Fatal(err)
	}
	b.RecordFailure(1, path)
	if err := b.TryDispatch(1, path); err != nil {
		t.Fatalf("native same-group retry: %v", err)
	}
	if err := b.TryDispatch(2, path); !errors.Is(err, ErrAccountRouteRepeatedPath) {
		t.Fatalf("same path in backup: %v", err)
	}
	for name, change := range map[string]func(*AccountRouteFingerprint){
		"other account": func(p *AccountRouteFingerprint) { p.AccountID++ },
		"mapped model":  func(p *AccountRouteFingerprint) { p.UpstreamModel = "Model-B" },
		"model casing":  func(p *AccountRouteFingerprint) { p.UpstreamModel = "model-a" },
		"endpoint":      func(p *AccountRouteFingerprint) { p.Endpoint = "/v1/chat/completions" },
		"platform":      func(p *AccountRouteFingerprint) { p.Platform = "deepseek" },
	} {
		t.Run(name, func(t *testing.T) {
			other := path
			change(&other)
			if err := b.TryDispatch(2, other); err != nil {
				t.Fatalf("distinct real path: %v", err)
			}
		})
	}
}

func TestAccountRouteBudgetReadinessDoesNotSpendAttempts(t *testing.T) {
	b, _ := NewAccountRouteBudget(1, time.Minute)
	path := AccountRouteFingerprint{AccountID: 1, UpstreamModel: "m", Platform: "openai", Endpoint: "/v1/responses"}
	for i := 0; i < 10; i++ {
		if err := b.CanDispatch(1, path); err != nil {
			t.Fatal(err)
		}
	}
	if b.Dispatched() != 0 {
		t.Fatal("readiness spent an attempt")
	}
	if err := b.TryDispatch(1, path); err != nil {
		t.Fatal(err)
	}
	if err := b.CanDispatch(2, path); !errors.Is(err, ErrAccountRouteAttemptBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestAccountRouteBudgetTimeBoundary(t *testing.T) {
	now := time.Now()
	b, _ := newAccountRouteBudget(10, time.Second, func() time.Time { return now })
	path := AccountRouteFingerprint{AccountID: 1, UpstreamModel: "m", Platform: "openai", Endpoint: "/v1/responses"}
	now = now.Add(time.Second)
	if err := b.TryDispatch(1, path); !errors.Is(err, ErrAccountRouteTimeBudget) {
		t.Fatalf("got %v", err)
	}
	if b.Dispatched() != 0 {
		t.Fatal("expired attempt dispatched")
	}
}

func TestAccountRouteBudgetConcurrentAdmissionBounded(t *testing.T) {
	b, _ := NewAccountRouteBudget(7, time.Minute)
	path := AccountRouteFingerprint{AccountID: 1, UpstreamModel: "m", Platform: "openai", Endpoint: "/v1/responses"}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = b.TryDispatch(1, path) }()
	}
	wg.Wait()
	if b.Dispatched() != 7 {
		t.Fatalf("dispatched=%d", b.Dispatched())
	}
}

func TestAccountRouteBudgetRejectsInvalidConfiguration(t *testing.T) {
	for _, attempts := range []int{0, -1, 4097} {
		if _, err := NewAccountRouteBudget(attempts, time.Minute); err == nil {
			t.Fatalf("accepted %d", attempts)
		}
	}
	if _, err := NewAccountRouteBudget(1, 0); err == nil {
		t.Fatal("accepted unbounded time")
	}
	b, _ := NewAccountRouteBudget(1, time.Minute)
	if err := b.TryDispatch(1, AccountRouteFingerprint{}); !errors.Is(err, ErrAccountRouteInvalidPath) {
		t.Fatalf("got %v", err)
	}
}

func TestAccountRouteBudgetPlanningIsReadOnlyAndBounded(t *testing.T) {
	now := time.Now()
	b, err := newAccountRouteBudget(3, time.Second, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	path := AccountRouteFingerprint{AccountID: 1, UpstreamModel: "m", Platform: "openai", Endpoint: "/v1/responses"}
	if err := b.TryDispatch(1, path); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := b.CanPlanAttempts(2); err != nil || b.Dispatched() != 1 {
			t.Fatalf("planning consumed or denied two remaining permits: %v count=%d", err, b.Dispatched())
		}
	}
	if err := b.TryDispatch(1, path); err != nil {
		t.Fatal(err)
	}
	if err := b.CanPlanAttempts(2); !errors.Is(err, ErrAccountRouteAttemptBudget) {
		t.Fatalf("timer could consume the final permit: %v", err)
	}
	if err := b.CanPlanAttempts(1); err != nil || b.Dispatched() != 2 {
		t.Fatalf("last actual send must remain available: %v count=%d", err, b.Dispatched())
	}
	if err := b.CanPlanAttemptsWithin(1, time.Second-time.Nanosecond); err != nil {
		t.Fatalf("strictly-before-deadline timer was rejected: %v", err)
	}
	if err := b.CanPlanAttemptsWithin(1, time.Second); !errors.Is(err, ErrAccountRouteTimeBudget) {
		t.Fatalf("timer at request deadline has no legal replacement: %v", err)
	}
	if err := b.CanPlanAttemptsWithin(1, -time.Nanosecond); err == nil {
		t.Fatal("negative planning delay was accepted")
	}
	now = now.Add(time.Second)
	if err := b.CanPlanAttempts(1); !errors.Is(err, ErrAccountRouteTimeBudget) || b.Dispatched() != 2 {
		t.Fatalf("expired planning changed permits: %v count=%d", err, b.Dispatched())
	}
	for _, invalid := range []int{0, -1} {
		if err := b.CanPlanAttempts(invalid); err == nil {
			t.Fatalf("accepted invalid planned count %d", invalid)
		}
	}
	var missing *AccountRouteBudget
	if err := missing.CanPlanAttempts(2); err == nil {
		t.Fatal("nil budget allowed a proactive retry plan")
	}
}

func TestAccountRouteBudgetConservativeProbeDoesNotChangeActualMappingRights(t *testing.T) {
	b, _ := NewAccountRouteBudget(10, time.Minute)
	failed := AccountRouteFingerprint{AccountID: 8, UpstreamModel: "model-a", Platform: "openai", Endpoint: "/v1/responses"}
	b.RecordFailure(1, failed)
	if b.FailedAccountInOtherGroup(1, 8) || b.FailedAccountInOtherGroup(2, 9) || !b.FailedAccountInOtherGroup(2, 8) {
		t.Fatal("probe did not isolate the failed account and originating group")
	}
	other := failed
	other.UpstreamModel = "model-b"
	if err := b.CanDispatch(2, other); err != nil {
		t.Fatalf("conservative probe forbade a distinct real mapping: %v", err)
	}
	if err := b.CanDispatch(2, failed); !errors.Is(err, ErrAccountRouteRepeatedPath) {
		t.Fatalf("identical actual failed mapping escaped dedup: %v", err)
	}
	if b.Dispatched() != 0 {
		t.Fatal("probe planning changed actual dispatch count")
	}
}
