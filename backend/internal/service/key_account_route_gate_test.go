package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type accountRouteGateTestStub struct {
	excluded map[int64]struct{}
	local    map[int64]struct{}
	allowed  bool
	err      error
	group    int64
	account  int64
}

type accountRouteCandidateGateTestStub struct {
	accountRouteGateTestStub
	candidateAllowed  map[int64]bool
	candidateErr      error
	exclusionCalls    int
	finalCalls        int
	candidateAccounts []int64
}

func (g *accountRouteCandidateGateTestStub) Exclusions(ctx context.Context, groupID int64) (map[int64]struct{}, error) {
	g.exclusionCalls++
	return g.accountRouteGateTestStub.Exclusions(ctx, groupID)
}

func (g *accountRouteCandidateGateTestStub) Eligible(ctx context.Context, groupID, accountID int64) (bool, error) {
	g.finalCalls++
	return g.accountRouteGateTestStub.Eligible(ctx, groupID, accountID)
}

func (g *accountRouteCandidateGateTestStub) CandidateEligible(_ context.Context, groupID, accountID int64) (bool, error) {
	g.group, g.account = groupID, accountID
	g.candidateAccounts = append(g.candidateAccounts, accountID)
	if g.exclusionCalls == 0 {
		return false, errors.New("candidate scan omitted the authoritative group entry")
	}
	return g.candidateAllowed[accountID], g.candidateErr
}

func (g *accountRouteGateTestStub) RequestExclusions(int64) map[int64]struct{} {
	return g.local
}

func (g *accountRouteGateTestStub) Exclusions(_ context.Context, groupID int64) (map[int64]struct{}, error) {
	g.group = groupID
	return g.excluded, g.err
}

func (g *accountRouteGateTestStub) Eligible(_ context.Context, groupID, accountID int64) (bool, error) {
	g.group, g.account = groupID, accountID
	return g.allowed, g.err
}

func TestKeyAccountRouteExclusionsPreserveIndependentMaps(t *testing.T) {
	group := int64(10)
	native := map[int64]struct{}{1: {}}
	gate := &accountRouteGateTestStub{excluded: map[int64]struct{}{2: {}, -1: {}}}
	ctx := WithKeyAccountRouteGate(context.Background(), gate)
	merged, err := keyAccountRouteExclusions(ctx, &group, native)
	if err != nil {
		t.Fatal(err)
	}
	if gate.group != group || len(merged) != 2 {
		t.Fatalf("group=%d merged=%v", gate.group, merged)
	}
	delete(merged, 2)
	merged[3] = struct{}{}
	if len(native) != 1 || len(gate.excluded) != 2 {
		t.Fatal("shared map was mutated")
	}
	// Native 503 retries may clear their map, but the independent rule snapshot
	// must still be merged on the next scheduler pass.
	clear(native)
	merged, err = keyAccountRouteExclusions(ctx, &group, native)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := merged[2]; !ok {
		t.Fatal("native reset removed Key-local exclusion")
	}
}

func TestKeyAccountRouteGateFailureDoesNotBecomeEmptyPool(t *testing.T) {
	group := int64(10)
	native := map[int64]struct{}{1: {}}
	want := errors.New("optional health store offline")
	ctx := WithKeyAccountRouteGate(context.Background(), &accountRouteGateTestStub{err: want})
	merged, err := keyAccountRouteExclusions(ctx, &group, native)
	if !errors.Is(err, want) || errors.Is(err, ErrNoAvailableAccounts) {
		t.Fatalf("wrong error: %v", err)
	}
	if _, ok := merged[1]; !ok {
		t.Fatal("lost native exclusion on soft-store failure")
	}
}

func TestKeyAccountRouteGateIsRequestLocalAndRechecksAdmission(t *testing.T) {
	group := int64(10)
	base := context.Background()
	gate := &accountRouteGateTestStub{allowed: true}
	ctx := WithKeyAccountRouteGate(base, gate)
	if ok, err := keyAccountRouteEligible(ctx, &group, 2); err != nil || !ok {
		t.Fatalf("first admission: %v %v", ok, err)
	}
	gate.allowed = false // Another request established cooldown while queued.
	if ok, err := keyAccountRouteEligible(ctx, &group, 2); err != nil || ok {
		t.Fatalf("fresh admission: %v %v", ok, err)
	}
	if gate.group != group || gate.account != 2 {
		t.Fatal("wrong attempt identity")
	}
	if ok, err := keyAccountRouteEligible(base, &group, 2); err != nil || !ok {
		t.Fatal("another Key inherited the gate")
	}
}

func TestKeyAccountRouteGateCancellationIsNotFailOpen(t *testing.T) {
	group := int64(10)
	gate := &accountRouteGateTestStub{allowed: true}
	ctx, cancel := context.WithCancel(WithKeyAccountRouteGate(context.Background(), gate))
	cancel()
	if _, err := keyAccountRouteExclusions(ctx, &group, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if ok, err := keyAccountRouteEligible(ctx, &group, 2); ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v %v", ok, err)
	}
	if gate.group != 0 {
		t.Fatal("cancelled request entered optional store")
	}
}

func TestKeyAccountRouteGateOnlyOptionalStoreFailureDegrades(t *testing.T) {
	group := int64(10)
	for _, tc := range []struct {
		name     string
		err      error
		degraded bool
	}{
		{"optional state", fmt.Errorf("%w: redis timeout", ErrKeyAccountSoftHealthUnavailable), true},
		{"authorization", errors.New("group authorization revoked"), false},
		{"stale policy", errors.New("stale key policy revision"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := &accountRouteGateTestStub{err: tc.err}
			ctx := WithKeyAccountRouteGate(context.Background(), gate)
			native := map[int64]struct{}{1: {}}
			merged, err := keyAccountRouteSchedulingExclusions(ctx, &group, native)
			if tc.degraded {
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := merged[1]; !ok {
					t.Fatal("native exclusion lost")
				}
			} else if !errors.Is(err, tc.err) {
				t.Fatalf("hard error swallowed: %v", err)
			}
			allowed, err := keyAccountRouteAdmission(ctx, &group, 2)
			if tc.degraded {
				if !allowed || err != nil {
					t.Fatalf("soft degradation: %v %v", allowed, err)
				}
			} else if allowed || !errors.Is(err, tc.err) {
				t.Fatalf("hard admission error swallowed: %v %v", allowed, err)
			}
		})
	}
}

func TestKeyAccountRouteSoftFailureKeepsRequestFailuresAfterNativeReset(t *testing.T) {
	group := int64(10)
	gate := &accountRouteGateTestStub{
		local: map[int64]struct{}{7: {}},
		err:   fmt.Errorf("%w: state unavailable", ErrKeyAccountSoftHealthUnavailable),
	}
	ctx := WithKeyAccountRouteGate(context.Background(), gate)
	native := map[int64]struct{}{3: {}}
	merged, err := keyAccountRouteSchedulingExclusions(ctx, &group, native)
	if err != nil || len(merged) != 2 {
		t.Fatalf("merged=%v err=%v", merged, err)
	}
	clear(native)
	merged, err = keyAccountRouteSchedulingExclusions(ctx, &group, native)
	if err != nil || len(merged) != 1 {
		t.Fatalf("after native reset merged=%v err=%v", merged, err)
	}
	if _, ok := merged[7]; !ok {
		t.Fatal("request failure disappeared during Redis degradation")
	}
	if ok, err := keyAccountRouteAdmission(ctx, &group, 7); ok || err != nil {
		t.Fatalf("request failure admitted during degradation: %v %v", ok, err)
	}
	if ok, err := keyAccountRouteAdmission(ctx, &group, 8); !ok || err != nil {
		t.Fatalf("unrelated native candidate denied: %v %v", ok, err)
	}
	delete(merged, 7)
	if len(gate.local) != 1 {
		t.Fatal("caller mutated request-local exclusion state")
	}
}

func TestKeyAccountRouteCandidateHintDoesNotReplaceFinalAdmission(t *testing.T) {
	group := int64(10)
	gate := &accountRouteCandidateGateTestStub{candidateAllowed: map[int64]bool{2: true}}
	ctx := WithKeyAccountRouteGate(context.Background(), gate)
	if _, err := keyAccountRouteSchedulingExclusions(ctx, &group, nil); err != nil {
		t.Fatal(err)
	}
	if allowed, err := keyAccountRouteCandidateAdmission(ctx, &group, 2); err != nil || !allowed {
		t.Fatalf("cached candidate hint: %v %v", allowed, err)
	}
	if gate.exclusionCalls != 1 || gate.finalCalls != 0 || len(gate.candidateAccounts) != 1 {
		t.Fatalf("candidate consulted final admission: %+v", gate)
	}
	// A candidate hint is not proof after waiting for a native slot.
	if allowed, err := keyAccountRouteAdmission(ctx, &group, 2); err != nil || allowed || gate.finalCalls != 1 {
		t.Fatalf("fresh final admission was bypassed: %v %v", allowed, err)
	}
	legacy := WithKeyAccountRouteGate(context.Background(), &accountRouteGateTestStub{allowed: true})
	if allowed, err := keyAccountRouteCandidateAdmission(legacy, &group, 2); err != nil || !allowed {
		t.Fatalf("legacy gate lost its Eligible fallback: %v %v", allowed, err)
	}
}

func TestKeyAccountRouteCandidateDegradationKeepsLocalFailuresAndHardErrors(t *testing.T) {
	group := int64(10)
	soft := fmt.Errorf("%w: state unavailable", ErrKeyAccountSoftHealthUnavailable)
	gate := &accountRouteCandidateGateTestStub{
		accountRouteGateTestStub: accountRouteGateTestStub{local: map[int64]struct{}{7: {}}, err: soft},
		candidateErr:             soft,
	}
	ctx := WithKeyAccountRouteGate(context.Background(), gate)
	if _, err := keyAccountRouteSchedulingExclusions(ctx, &group, nil); err != nil {
		t.Fatal(err)
	}
	for account, want := range map[int64]bool{7: false, 8: true} {
		if allowed, err := keyAccountRouteCandidateAdmission(ctx, &group, account); err != nil || allowed != want {
			t.Fatalf("candidate %d lost request exclusions: %v %v", account, allowed, err)
		}
	}
	gate.candidateErr = ErrKeyAccountRouteStale
	if allowed, err := keyAccountRouteCandidateAdmission(ctx, &group, 8); allowed || !errors.Is(err, ErrKeyAccountRouteStale) {
		t.Fatalf("stale metadata degraded: %v %v", allowed, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	before := len(gate.candidateAccounts)
	if allowed, err := keyAccountRouteCandidateAdmission(canceled, &group, 8); allowed || !errors.Is(err, context.Canceled) || len(gate.candidateAccounts) != before {
		t.Fatalf("cancellation entered candidate gate: %v %v", allowed, err)
	}
	if allowed, err := keyAccountRouteCandidateAdmission(nil, &group, 8); allowed || err == nil {
		t.Fatalf("nil context accepted: %v %v", allowed, err)
	}
}
