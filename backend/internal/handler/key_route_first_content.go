package handler

import (
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Deliberately no Unwrap: once usage/content is known, the original retryable
// classification must not send the same billable request to another account.
// Its message and native partial result are preserved for normal error handling.
type keyRouteNoReplayForwardError struct{ message string }

func (e *keyRouteNoReplayForwardError) Error() string { return e.message }

func keyRouteFirstContentNormalizeError(c *gin.Context, err error) error {
	if err == nil || c == nil || c.Request == nil {
		return err
	}
	guard := keyrouting.GetFirstContentGuard(c.Request.Context())
	if !guard.ReplayBlocked() {
		return err
	}
	var failover *service.UpstreamFailoverError
	if !errors.As(err, &failover) {
		return err
	}
	// This is a final group result, but a parser's synthetic retry status is
	// not necessarily a real HTTP response. Only semantic rules are updated.
	recordKeyRouteFailure(c, 0, keyRouteOutcome(failover), false)
	return &keyRouteNoReplayForwardError{message: err.Error()}
}

// This hook runs after Forward and its account-slot cleanup, before native
// retry/health reporting. Known usage always wins over cross-group replay: the
// original handler must retain its normal partial-result settlement path.
func keyRouteFirstContentInterrupted(c *gin.Context, result any, forwardErr error) bool {
	if c == nil || c.Request == nil {
		return false
	}
	guard := keyrouting.GetFirstContentGuard(c.Request.Context())
	if guard == nil {
		return false
	}
	if keyRouteFirstContentHasUsage(result) {
		guard.PreventReplay()
	}
	// A complete native response wins the deadline/return race even when its
	// provider does not report token usage. Never discard a successful result.
	if forwardErr == nil {
		switch r := result.(type) {
		case *service.OpenAIForwardResult:
			if r != nil {
				guard.PreventReplay()
			}
		case *service.ForwardResult:
			if r != nil {
				guard.PreventReplay()
			}
		}
	}
	return guard.ReplaySafe() && c.Request.Context().Err() == nil
}

func keyRouteFirstContentHasUsage(result any) bool {
	switch r := result.(type) {
	case *service.OpenAIForwardResult:
		if r == nil {
			return false
		}
		u := r.Usage
		return u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheCreationInputTokens > 0 ||
			u.CacheReadInputTokens > 0 || u.ImageInputTokens > 0 || u.ImageOutputTokens > 0 ||
			u.ImageCacheReadTokens > 0 || u.KiroCredits > 0 || r.ImageCount > 0 ||
			r.VideoCount > 0 || r.VideoDurationSeconds > 0 || r.WebSearchCalls > 0 ||
			r.SearchCount > 0 || (r.AudioUsage != nil && r.AudioUsage.DurationOrUnits > 0)
	case *service.ForwardResult:
		if r == nil {
			return false
		}
		u := r.Usage
		return u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheCreationInputTokens > 0 ||
			u.CacheReadInputTokens > 0 || u.CacheCreation5mTokens > 0 || u.CacheCreation1hTokens > 0 ||
			u.ImageOutputTokens > 0 || u.KiroCredits > 0 || r.ImageCount > 0 || r.SearchCount > 0 ||
			r.CacheStorageTokenHours > 0 || (r.AudioUsage != nil && r.AudioUsage.DurationOrUnits > 0)
	default:
		return false
	}
}
