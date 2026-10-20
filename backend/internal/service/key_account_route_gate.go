package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

// Only this explicitly classified optional-store error may degrade to native
// scheduling. Authorization, stale configuration and cancellation errors must
// never be swallowed by a blanket fail-open policy.
var ErrKeyAccountSoftHealthUnavailable = errors.New("key account soft health unavailable")

// KeyAccountRouteGate is an optional, request-local exclusion layer. It is
// installed only for an explicitly selected account-scoped route. The native
// scheduler still owns account qualifications, priorities, sticky sessions,
// load balancing and slots. Implementations must return errors, not fabricate
// an empty pool, when the optional health store cannot be read.
type KeyAccountRouteGate interface {
	Exclusions(context.Context, int64) (map[int64]struct{}, error)
	// Eligible is the fresh check after waiting for a native account slot and
	// immediately before inference dispatch. It must not reserve or debit.
	Eligible(context.Context, int64, int64) (bool, error)
}

type keyAccountRouteGateContextKey struct{}

type keyAccountRouteGateBinding struct {
	gate KeyAccountRouteGate
	warn sync.Once
}

func WithKeyAccountRouteGate(ctx context.Context, gate KeyAccountRouteGate) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, keyAccountRouteGateContextKey{}, &keyAccountRouteGateBinding{gate: gate})
}

func keyAccountRouteGateFromContext(ctx context.Context) KeyAccountRouteGate {
	if ctx == nil {
		return nil
	}
	binding, _ := ctx.Value(keyAccountRouteGateContextKey{}).(*keyAccountRouteGateBinding)
	if binding == nil {
		return nil
	}
	return binding.gate
}

func keyAccountRouteSchedulingExclusions(ctx context.Context, groupID *int64, native map[int64]struct{}) (map[int64]struct{}, error) {
	merged, err := keyAccountRouteExclusions(ctx, groupID, native)
	if errors.Is(err, ErrKeyAccountSoftHealthUnavailable) && ctx.Err() == nil {
		keyAccountRouteWarnDegraded(ctx, err)
		// Only Redis soft health is optional. Actual failures in this request
		// remain excluded even if native 503 recovery clears its own retry map.
		return keyAccountRouteRequestExclusions(ctx, groupID, native), nil
	}
	return merged, err
}

func keyAccountRouteAdmission(ctx context.Context, groupID *int64, accountID int64) (bool, error) {
	allowed, err := keyAccountRouteEligible(ctx, groupID, accountID)
	return keyAccountRouteAdmissionResult(ctx, groupID, accountID, allowed, err)
}

// Candidate admission is only a read-only scan hint. The group scan must first
// call keyAccountRouteSchedulingExclusions, which validates the policy revision
// and preloads the group's health. A final admission after native slot waits
// must still use Eligible/BeforeDispatch, never this cached candidate result.
func keyAccountRouteCandidateAdmission(ctx context.Context, groupID *int64, accountID int64) (bool, error) {
	allowed, err := keyAccountRouteCandidateEligible(ctx, groupID, accountID)
	return keyAccountRouteAdmissionResult(ctx, groupID, accountID, allowed, err)
}

func keyAccountRouteAdmissionResult(ctx context.Context, groupID *int64, accountID int64, allowed bool, err error) (bool, error) {
	if errors.Is(err, ErrKeyAccountSoftHealthUnavailable) && ctx.Err() == nil {
		keyAccountRouteWarnDegraded(ctx, err)
		if _, excluded := keyAccountRouteRequestExclusions(ctx, groupID, nil)[accountID]; excluded {
			return false, nil
		}
		return true, nil
	}
	return allowed, err
}

// RequestExclusions is an optional extension implemented by a request session.
// It contains only already-failed local paths, never optional cached cooldowns.
func keyAccountRouteRequestExclusions(ctx context.Context, groupID *int64, native map[int64]struct{}) map[int64]struct{} {
	if groupID == nil || *groupID <= 0 {
		return native
	}
	local, ok := keyAccountRouteGateFromContext(ctx).(interface {
		RequestExclusions(int64) map[int64]struct{}
	})
	if !ok {
		return native
	}
	extra := local.RequestExclusions(*groupID)
	merged := make(map[int64]struct{}, len(native)+len(extra))
	for id := range native {
		merged[id] = struct{}{}
	}
	for id := range extra {
		if id > 0 {
			merged[id] = struct{}{}
		}
	}
	return merged
}

func keyAccountRouteWarnDegraded(ctx context.Context, err error) {
	binding, _ := ctx.Value(keyAccountRouteGateContextKey{}).(*keyAccountRouteGateBinding)
	if binding != nil {
		binding.warn.Do(func() {
			slog.WarnContext(ctx, "key_account_route_soft_health_degraded", "error", err)
		})
	}
}

// keyAccountRouteExclusions never mutates the native retry map or a gate's
// cached snapshot. Thus a native 503 backoff may reset its own retry state but
// cannot erase account cooldowns; the caller merges them again on every pass.
// Errors are deliberately not converted into ErrNoAvailableAccounts.
func keyAccountRouteExclusions(ctx context.Context, groupID *int64, native map[int64]struct{}) (map[int64]struct{}, error) {
	if ctx == nil {
		return nil, errors.New("key account route exclusions require a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gate := keyAccountRouteGateFromContext(ctx)
	if gate == nil || groupID == nil || *groupID <= 0 {
		return native, nil
	}
	extra, err := gate.Exclusions(ctx, *groupID)
	if err != nil {
		// The coordinator chooses fail-open for optional soft health failures;
		// returning the original exclusions allows this without losing a native
		// quota/profit/retry exclusion or treating the group as exhausted.
		return native, err
	}
	merged := make(map[int64]struct{}, len(native)+len(extra))
	for id := range native {
		merged[id] = struct{}{}
	}
	for id := range extra {
		if id > 0 {
			merged[id] = struct{}{}
		}
	}
	return merged, nil
}

func keyAccountRouteEligible(ctx context.Context, groupID *int64, accountID int64) (bool, error) {
	if ctx == nil {
		return false, errors.New("key account route admission requires a context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	gate := keyAccountRouteGateFromContext(ctx)
	if gate == nil {
		return true, nil
	}
	if groupID == nil || *groupID <= 0 || accountID <= 0 {
		return false, errors.New("key account route admission requires group and account identities")
	}
	return gate.Eligible(ctx, *groupID, accountID)
}

// Older gate implementations retain their authoritative Eligible behavior.
// Only a gate that explicitly supplies candidate hints may skip per-account
// revision reads during an already-validated group scan.
func keyAccountRouteCandidateEligible(ctx context.Context, groupID *int64, accountID int64) (bool, error) {
	if ctx == nil {
		return false, errors.New("key account route candidate requires a context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	gate := keyAccountRouteGateFromContext(ctx)
	if gate == nil {
		return true, nil
	}
	if groupID == nil || *groupID <= 0 || accountID <= 0 {
		return false, errors.New("key account route candidate requires group and account identities")
	}
	if candidates, ok := gate.(interface {
		CandidateEligible(context.Context, int64, int64) (bool, error)
	}); ok {
		return candidates.CandidateEligible(ctx, *groupID, accountID)
	}
	return gate.Eligible(ctx, *groupID, accountID)
}
