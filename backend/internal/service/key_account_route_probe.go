package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
)

// AccountFirstContentTimeout returns an active timer only if another native
// legal candidate exists at this dispatch's preflight. The probe excludes the
// selected account without mutating historical health or request exclusions.
// Native timeouts still apply when this returns zero for the last candidate.
func (s *KeyRoutingService) AccountFirstContentTimeout(ctx context.Context, root *APIKey, request KeyRoutingRequest, groupID, accountID int64) (time.Duration, error) {
	session := GetKeyAccountRoutingSession(ctx)
	if s == nil || session == nil || root == nil || root.GroupID == nil || accountID <= 0 {
		return 0, ErrKeyAccountRouteScope
	}
	policy, ok := session.Policy(groupID)
	if !ok {
		return 0, ErrKeyAccountRouteScope
	}
	wait := policy.Policy.FirstContentTimeout()
	if wait <= 0 {
		return 0, nil
	}
	// This preflight runs before the current real Do spends its permit. Keep
	// its last permit usable without a proactive timer that has no successor.
	// Actual dispatch still checks the live budget at its own final boundary.
	if err := session.budget.CanPlanAttemptsWithin(2, wait); err != nil {
		if errors.Is(err, keyrouting.ErrAccountRouteAttemptBudget) || errors.Is(err, keyrouting.ErrAccountRouteTimeBudget) {
			return 0, nil
		}
		return 0, err
	}
	ids := append([]int64{*root.GroupID}, root.BackupGroupIDs...)
	start := -1
	for i, id := range ids {
		if id == groupID {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, ErrKeyAccountRouteScope
	}
	probe := &keyAccountRouteProbeGate{inner: session, excludedAccount: accountID}
	probeCtx := WithKeyAccountRouteGate(ctx, probe)
	_, _, err := s.selectFrom(probeCtx, root, request, start, false)
	switch {
	case err == nil:
		return wait, nil
	case errors.Is(err, keyrouting.ErrNoSchedulableAccounts), errors.Is(err, keyrouting.ErrModelUnavailable):
		return 0, nil
	default:
		return 0, err
	}
}

// Excluding the selected account across the probe is conservative for shared
// accounts with different group mappings: uncertain alternatives disable the
// proactive timer, never cause premature cancellation of the last real path.
type keyAccountRouteProbeGate struct {
	inner           *KeyAccountRoutingSession
	excludedAccount int64
}

func (g *keyAccountRouteProbeGate) Exclusions(ctx context.Context, groupID int64) (map[int64]struct{}, error) {
	result, err := g.inner.Exclusions(ctx, groupID)
	if err != nil {
		return nil, err
	}
	result[g.excludedAccount] = struct{}{}
	return result, nil
}

func (g *keyAccountRouteProbeGate) Eligible(ctx context.Context, groupID, accountID int64) (bool, error) {
	if g.excludes(groupID, accountID) {
		// A probe exclusion does not suppress authoritative revision checks.
		return false, g.inner.ValidateRevision(ctx, groupID)
	}
	return g.inner.Eligible(ctx, groupID, accountID)
}

// Exclusions already checked the group's authoritative revision and prefetched
// health. Forward the hint port so a TTFT probe cannot accidentally fall back
// to O(candidates) fresh SQL/Redis reads. This is never a final admission.
func (g *keyAccountRouteProbeGate) CandidateEligible(ctx context.Context, groupID, accountID int64) (bool, error) {
	if err := g.inner.validateGroupScope(ctx, groupID); err != nil {
		return false, err
	}
	if g.excludes(groupID, accountID) {
		return false, nil
	}
	return g.inner.CandidateEligible(ctx, groupID, accountID)
}

func (g *keyAccountRouteProbeGate) excludes(groupID, accountID int64) bool {
	return accountID == g.excludedAccount || g.inner.budget.FailedAccountInOtherGroup(groupID, accountID)
}

func (g *keyAccountRouteProbeGate) RequestExclusions(groupID int64) map[int64]struct{} {
	result := g.inner.RequestExclusions(groupID)
	result[g.excludedAccount] = struct{}{}
	return result
}
