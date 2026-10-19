package keyrouting

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestFirstContentGuardExpiryCancelsOnlyDetachedChildren(t *testing.T) {
	type valueKey struct{}
	root, cancelRoot := context.WithCancel(context.WithValue(context.Background(), valueKey{}, "kept"))
	defer cancelRoot()
	guard := NewFirstContentGuard(time.Hour)
	root = WithFirstContentGuard(root, guard)
	first := guard.Detach(root)
	second := guard.Detach(root)
	guard.expire()
	for _, ctx := range []context.Context{first, second} {
		if ctx.Err() != context.Canceled {
			t.Fatalf("detached child not canceled: %v", ctx.Err())
		}
		if ctx.Value(valueKey{}) != "kept" || GetFirstContentGuard(ctx) != guard {
			t.Fatal("detachment lost context values")
		}
	}
	if root.Err() != nil || !guard.Expired() || !guard.ReplaySafe() {
		t.Fatal("expiry must cancel only upstream children and allow replay")
	}
	if guard.Observe() {
		t.Fatal("semantic content cannot commit after the deadline won")
	}
	if guard.Detach(root).Err() != context.Canceled {
		t.Fatal("retry must not reset an expired group deadline")
	}
	guard.Finish()
}

func TestFirstContentGuardObservedContentDisarmsDeadline(t *testing.T) {
	guard := NewFirstContentGuard(time.Hour)
	child := guard.Detach(context.Background())
	if !guard.Observe() {
		t.Fatal("first content should win")
	}
	first, ok := guard.FirstContentElapsed()
	if !ok || first < 0 {
		t.Fatal("first content latency not observed")
	}
	guard.expire()
	if guard.Expired() || guard.ReplaySafe() || child.Err() != nil {
		t.Fatal("observed content must prevent cancellation and replay")
	}
	if !guard.Observe() {
		t.Fatal("subsequent content must remain allowed")
	}
	second, _ := guard.FirstContentElapsed()
	if first != second {
		t.Fatal("subsequent content changed the first-content measurement")
	}
	guard.Finish()
	guard.Finish()
	if child.Err() != context.Canceled {
		t.Fatal("finish must close any surviving upstream contexts")
	}
}

func TestFirstContentGuardKnownUsageAlwaysPreventsReplay(t *testing.T) {
	for _, expireFirst := range []bool{false, true} {
		guard := NewFirstContentGuard(time.Hour)
		child := guard.Detach(context.Background())
		if expireFirst {
			guard.expire()
		}
		guard.PreventReplay()
		guard.expire()
		if guard.ReplaySafe() || !guard.Observe() {
			t.Fatalf("known usage must preserve original response, expireFirst=%v", expireFirst)
		}
		if !expireFirst && (guard.Expired() || child.Err() != nil) {
			t.Fatal("usage observed before expiry must disarm cancellation")
		}
		guard.Finish()
	}
}

func TestFirstContentGuardZeroTimeoutObservesWithoutCancellation(t *testing.T) {
	guard := NewFirstContentGuard(0)
	child := guard.Detach(context.Background())
	if guard.timer != nil || guard.Expired() || child.Err() != nil {
		t.Fatal("the last candidate must not get an active timer")
	}
	if !guard.Observe() {
		t.Fatal("the last candidate still observes first content")
	}
	if _, ok := guard.FirstContentElapsed(); !ok {
		t.Fatal("zero timeout lost the measurement")
	}
	guard.Finish()
}

func TestFirstContentGuardObserveExpiryRace(t *testing.T) {
	for i := 0; i < 100; i++ {
		guard := NewFirstContentGuard(time.Hour)
		child := guard.Detach(context.Background())
		var observed bool
		var wait sync.WaitGroup
		wait.Add(2)
		go func() { defer wait.Done(); observed = guard.Observe() }()
		go func() { defer wait.Done(); guard.expire() }()
		wait.Wait()
		if observed {
			if guard.Expired() || child.Err() != nil || guard.ReplaySafe() {
				t.Fatal("content won but expiry canceled or permitted replay")
			}
		} else if !guard.Expired() || child.Err() != context.Canceled || !guard.ReplaySafe() {
			t.Fatal("deadline won without canceling upstream or permitting safe replay")
		}
		guard.Finish()
	}
}

func TestFirstContentGuardStartsAtFirstDispatchAndDetachesClient(t *testing.T) {
	guard := NewFirstContentGuard(20 * time.Millisecond)
	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	if _, ok := guard.FirstContentElapsed(); ok || guard.timer != nil {
		t.Fatal("constructing a guard must not start its deadline")
	}
	child := guard.Detach(root)
	cancelRoot()
	if child.Err() != nil {
		t.Fatal("client cancellation must not bypass native usage-drain ownership")
	}
	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("real deadline failed to cancel detached upstream")
	}
	if !guard.Expired() {
		t.Fatal("cancel must be attributed to the group deadline")
	}
	guard.Finish()
}

func TestFirstContentGuardAttemptElapsedRequiresDispatchAndFreezes(t *testing.T) {
	guard := NewFirstContentGuard(0)
	if _, ok := guard.AttemptElapsed(); ok {
		t.Fatal("an admission failure must not become a dispatched-attempt sample")
	}
	guard.Detach(context.Background())
	before, ok := guard.AttemptElapsed()
	if !ok || before < 0 {
		t.Fatal("a dispatched fast failure must be measurable without content")
	}
	guard.Finish()
	first, ok := guard.AttemptElapsed()
	second, _ := guard.AttemptElapsed()
	if !ok || first != second || first < before {
		t.Fatal("completed attempt latency must remain stable")
	}
}

func TestFirstContentGuardReplayBlockedIsIndependentOfDeadline(t *testing.T) {
	var absent *FirstContentGuard
	if absent.ReplayBlocked() {
		t.Fatal("an absent guard must preserve legacy switching")
	}
	for _, tc := range []struct {
		name  string
		apply func(*FirstContentGuard)
		want  bool
	}{
		{name: "ordinary pre-output error", apply: func(*FirstContentGuard) {}, want: false},
		{name: "safe timeout", apply: func(g *FirstContentGuard) { g.expire() }, want: false},
		{name: "usage before timeout", apply: func(g *FirstContentGuard) { g.PreventReplay() }, want: true},
		{name: "usage after timeout", apply: func(g *FirstContentGuard) { g.expire(); g.PreventReplay() }, want: true},
		{name: "semantic content", apply: func(g *FirstContentGuard) { g.Observe() }, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard := NewFirstContentGuard(time.Hour)
			guard.Detach(context.Background())
			tc.apply(guard)
			if guard.ReplayBlocked() != tc.want {
				t.Fatalf("ReplayBlocked=%v, want %v", guard.ReplayBlocked(), tc.want)
			}
			guard.Finish()
			if guard.ReplayBlocked() != tc.want {
				t.Fatal("cleanup must not change the irrevocability decision")
			}
		})
	}
}
