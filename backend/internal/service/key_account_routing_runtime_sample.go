package service

import (
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
)

// No error-string parsing or synthetic client status can invent an exact HTTP
// status rule event. AccountAttempt already freezes terminal evidence once.
func keyAccountRoutingHealthAttempt(sample keyrouting.AccountAttemptSample) (keyrouting.AccountHealthAttempt, error) {
	result := keyrouting.AccountHealthAttempt{AttemptID: sample.Metadata.AttemptID, Dispatched: sample.Dispatched, StartedAt: sample.StartedAt, FinishedAt: sample.FinishedAt, Outcome: sample.Outcome}
	if !sample.Dispatched || sample.Outcome == keyrouting.OutcomeIgnored || sample.CancellationCause == keyrouting.AttemptClientCancelled {
		result.Outcome = keyrouting.OutcomeIgnored
		return result, nil
	}
	if sample.Metadata.AttemptID == "" || sample.FinishedAt.IsZero() {
		return result, fmt.Errorf("%w: terminal attempt metadata is incomplete", ErrKeyAccountRouteScope)
	}
	switch sample.Outcome {
	case keyrouting.OutcomeSuccess, keyrouting.OutcomeFailure, keyrouting.OutcomeTimeout, keyrouting.OutcomeRateLimit:
	default:
		return result, fmt.Errorf("%w: unknown terminal attempt outcome", ErrKeyAccountRouteScope)
	}
	validStatus := func(value int) bool { return value >= 100 && value <= 599 }
	switch sample.StatusSource {
	case keyrouting.AttemptStatusUpstream:
		if validStatus(sample.UpstreamStatusCode) && sample.StatusCode == sample.UpstreamStatusCode {
			result.StatusCode = sample.UpstreamStatusCode
		}
	case keyrouting.AttemptStatusClient:
		if validStatus(sample.ClientStatusCode) && sample.StatusCode == sample.ClientStatusCode && !sample.ClientStatusSynthetic && !sample.FirstContentExpired {
			result.StatusCode = sample.ClientStatusCode
		}
	}
	if sample.FirstContentObserved {
		elapsed := sample.FirstContentElapsed
		if elapsed < 0 || elapsed > 24*time.Hour {
			return result, fmt.Errorf("%w: invalid first-content evidence", ErrKeyAccountRouteScope)
		}
		result.FirstContent = &elapsed
	} else {
		result.FirstContentExpired = sample.FirstContentExpired
	}
	// A timer can win just before late content/status arrives. Retain real
	// content and status evidence, but don't turn that timer into HTTP 504 or
	// a provider timeout. Successful late completions remain successes.
	if result.Outcome != keyrouting.OutcomeSuccess {
		switch result.StatusCode {
		case 408, 504, 522, 524:
			result.Outcome = keyrouting.OutcomeTimeout
		case 429:
			result.Outcome = keyrouting.OutcomeRateLimit
		default:
			if sample.FirstContentExpired {
				result.Outcome = keyrouting.OutcomeFailure
			}
		}
	}
	if sample.FirstContentExpired && !sample.FirstContentObserved && result.StatusCode == 0 && result.Outcome != keyrouting.OutcomeSuccess {
		result.LocalFirstContentTimeout = true
		result.Outcome = keyrouting.OutcomeFailure
	}
	return result, nil
}
