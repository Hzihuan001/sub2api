package service

import (
	"context"
	"time"
)

// HasAttemptBudget is a read-only hint for the group coordinator. A positive
// answer does not reserve or authorize a send; BeforeDispatch still checks
// the actual mapped path and spends the shared request permit atomically.
func (s *KeyAccountRoutingSession) HasAttemptBudget(ctx context.Context) error {
	if s == nil || s.budget == nil || ctx == nil {
		return ErrKeyAccountRouteScope
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.budget.CanPlanAttemptsWithin(1, 0)
}

// KeyAccountRouteRetryAfter reports the earliest recovery observed while
// evaluating this request's configured groups. It is advice only after the
// native planner has returned unavailable, never permission to bypass a
// cooldown. No remote read, probe or attempt is started by this function.
func KeyAccountRouteRetryAfter(ctx context.Context) time.Duration {
	session := GetKeyAccountRoutingSession(ctx)
	if session == nil || ctx.Err() != nil {
		return 0
	}
	var earliest time.Duration
	for groupID := range session.policies {
		if wait := session.RetryAfter(groupID); wait > 0 && (earliest == 0 || wait < earliest) {
			earliest = wait
		}
	}
	return earliest
}
