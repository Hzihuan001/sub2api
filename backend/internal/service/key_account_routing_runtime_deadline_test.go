package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
)

type accountRuntimeSlowStore struct {
	inner               keyrouting.AccountHealthStore
	slowRead, slowWrite bool
	reads, writes       int
	onRead              func()
	deadline            time.Time
}

func (s *accountRuntimeSlowStore) Get(ctx context.Context, id keyrouting.AccountHealthIdentity) (keyrouting.AccountHealthSnapshot, error) {
	s.reads++
	s.deadline, _ = ctx.Deadline()
	if s.onRead != nil {
		s.onRead()
	}
	if s.slowRead {
		<-ctx.Done()
		return keyrouting.AccountHealthSnapshot{}, ctx.Err()
	}
	return s.inner.Get(ctx, id)
}
func (s *accountRuntimeSlowStore) Observe(ctx context.Context, id keyrouting.AccountHealthIdentity, p keyrouting.RouteFailurePolicy, a keyrouting.AccountHealthAttempt) (keyrouting.AccountHealthSnapshot, error) {
	s.writes++
	s.deadline, _ = ctx.Deadline()
	if s.slowWrite {
		<-ctx.Done()
		return keyrouting.AccountHealthSnapshot{}, ctx.Err()
	}
	return s.inner.Observe(ctx, id, p, a)
}
func (s *accountRuntimeSlowStore) Fence(ctx context.Context, id keyrouting.AccountHealthIdentity) error {
	return s.inner.Fence(ctx, id)
}

func TestKeyAccountRoutingRuntimeShortHealthDeadline(t *testing.T) {
	s, rev, store, _, _, _ := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	slow := &accountRuntimeSlowStore{inner: store.inner, slowRead: true}
	s.runtime.store = slow
	s.runtime.opts.HealthReadTimeout = 20 * time.Millisecond
	ctx := context.Background()
	start := time.Now()
	_, err := s.Exclusions(ctx, 1)
	if !errors.Is(err, ErrKeyAccountSoftHealthUnavailable) || !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatalf("child timeout not optional: %v", err)
	}
	if time.Since(start) > 250*time.Millisecond || slow.deadline.IsZero() || slow.deadline.Sub(start) > 30*time.Millisecond {
		t.Fatalf("unbounded health deadline: elapsed=%s deadline=%s", time.Since(start), slow.deadline.Sub(start))
	}
	// One optional timeout cannot multiply by every account/group/retry.
	if _, err := s.CandidateEligible(ctx, 1, 42); !errors.Is(err, ErrKeyAccountSoftHealthUnavailable) {
		t.Fatalf("request degradation lost: %v", err)
	}
	if slow.reads != 1 {
		t.Fatalf("degraded request retried optional backend %d times", slow.reads)
	}
	if err := s.ExcludeRequestAccount(1, 41); err != nil {
		t.Fatal(err)
	}
	metadata, _ := s.Metadata(1, 41, "actual-model", "openai")
	if err := s.BeforeDispatch(ctx, metadata); !errors.Is(err, ErrKeyAccountRouteCooling) || s.budget.Dispatched() != 0 {
		t.Fatalf("local failure was bypassed: %v", err)
	}
	metadata, _ = s.Metadata(1, 42, "actual-model", "openai")
	if err := s.BeforeDispatch(ctx, metadata); err != nil || s.budget.Dispatched() != 1 {
		t.Fatalf("optional timeout blocked native dispatch: %v", err)
	}
	rev.keyRevision++
	if err := s.BeforeDispatch(ctx, metadata); !errors.Is(err, ErrKeyAccountRouteStale) || s.budget.Dispatched() != 1 {
		t.Fatalf("degradation swallowed revision error: %v", err)
	}
}

func TestKeyAccountRoutingRuntimeParentCancelIsHard(t *testing.T) {
	s, _, store, _, _, _ := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	slow := &accountRuntimeSlowStore{inner: store.inner, slowRead: true}
	s.runtime.store = slow
	s.runtime.opts.HealthReadTimeout = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()
	defer cancel()
	_, err := s.Eligible(ctx, 1, 41)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrKeyAccountSoftHealthUnavailable) {
		t.Fatalf("parent cancellation degraded: %v", err)
	}
	if s.healthDeadline != nil {
		t.Fatal("client cancellation poisoned request health")
	}
}

func TestKeyAccountRoutingRuntimeShortWriteAndMidReadEdit(t *testing.T) {
	s, rev, store, _, _, now := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	slow := &accountRuntimeSlowStore{inner: store.inner, slowWrite: true}
	s.runtime.store = slow
	s.runtime.opts.HealthWriteTimeout = 20 * time.Millisecond
	start := time.Now()
	_, err := s.Observe(context.Background(), accountRuntimeSample(t, s, 1, 41, "write-timeout", *now))
	if !errors.Is(err, ErrKeyAccountSoftHealthUnavailable) || time.Since(start) > 250*time.Millisecond || slow.writes != 1 {
		t.Fatalf("write not independently bounded: %v", err)
	}
	// Editing while the health backend waits must invalidate actual dispatch.
	s, rev, store, _, _, _ = accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	slow = &accountRuntimeSlowStore{inner: store.inner, slowRead: true, onRead: func() { rev.keyRevision++ }}
	s.runtime.store = slow
	s.runtime.opts.HealthReadTimeout = 20 * time.Millisecond
	metadata, _ := s.Metadata(1, 41, "actual-model", "openai")
	if err := s.BeforeDispatch(context.Background(), metadata); !errors.Is(err, ErrKeyAccountRouteStale) || s.budget.Dispatched() != 0 {
		t.Fatalf("edit during optional wait blessed old dispatch: %v", err)
	}
}

func TestKeyAccountRoutingRuntimeCandidateHintsAvoidPerAccountSQL(t *testing.T) {
	s, rev, store, _, _, _ := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	if _, err := s.Exclusions(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	reads := rev.keyReads
	for _, accountID := range []int64{41, 42, 41, 42} {
		if _, err := s.CandidateEligible(context.Background(), 1, accountID); err != nil {
			t.Fatal(err)
		}
	}
	if rev.keyReads != reads || store.getCalls != 2 {
		t.Fatalf("cached hints repeated SQL/Redis: SQL=%d reads=%d", rev.keyReads-reads, store.getCalls)
	}
	metadata, _ := s.Metadata(1, 41, "actual-model", "openai")
	for index := 0; index < 4; index++ {
		if err := s.CanDispatch(context.Background(), metadata); err != nil {
			t.Fatal(err)
		}
	}
	if rev.keyReads != reads {
		t.Fatal("budget hint performed authoritative SQL")
	}
	if err := s.BeforeDispatch(context.Background(), metadata); err != nil {
		t.Fatal(err)
	}
	if rev.keyReads != reads+1 {
		t.Fatalf("dispatch boundary did not perform exactly one revision check: %d", rev.keyReads-reads)
	}
	// Existing Eligible remains authoritative for direct/native final callers.
	rev.keyRevision++
	if _, err := s.Eligible(context.Background(), 1, 41); !errors.Is(err, ErrKeyAccountRouteStale) {
		t.Fatalf("old Eligible contract weakened: %v", err)
	}
	if err := s.TryDispatch(context.Background(), metadata); !errors.Is(err, ErrKeyAccountRouteStale) {
		t.Fatalf("direct TryDispatch bypassed final boundary: %v", err)
	}
}

func TestKeyAccountRoutingRuntimeHealthTimeoutOptionsBounded(t *testing.T) {
	r := NewKeyAccountRoutingRuntime(nil, nil, nil, KeyAccountRoutingRuntimeOptions{})
	if r.opts.HealthReadTimeout != 150*time.Millisecond || r.opts.HealthWriteTimeout != 250*time.Millisecond {
		t.Fatal("wrong default health budgets")
	}
	r = NewKeyAccountRoutingRuntime(nil, nil, nil, KeyAccountRoutingRuntimeOptions{HealthReadTimeout: time.Hour, HealthWriteTimeout: time.Hour})
	if r.opts.HealthReadTimeout != time.Second || r.opts.HealthWriteTimeout != time.Second {
		t.Fatal("health options accepted unbounded timeouts")
	}
}
