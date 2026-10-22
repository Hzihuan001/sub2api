package handler

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type keyRouteOutcomeRecorder interface {
	RecordOutcome(context.Context, *service.APIKey, service.KeyRoutingRequest, int64, keyrouting.RouteOutcomeKind) (keyrouting.RouteFailureResult, error)
}

type keyRouteContextPreparer interface {
	PrepareRouteContext(context.Context, *service.APIKey, service.KeyRoutingRequest) (context.Context, string, error)
}

type keyRouteFirstContentSelector interface {
	FirstContentTimeout(context.Context, *service.APIKey, service.KeyRoutingRequest, int64) (time.Duration, error)
	RecordFirstContent(context.Context, *service.APIKey, service.KeyRoutingRequest, int64, time.Duration) (keyrouting.RouteFailureResult, error)
}

// KeyRouteDispatch is the single platform-dispatch extension seam. Each
// handler finishes, including deferred slot and reservation cleanup, before
// the next group starts. Only an active TTFT guard holds bounded SSE preludes.
func KeyRouteDispatch(next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !keyRoutingRequestActive(c.Request.Context()) {
			next(c)
			return
		}
		c.Set("custom.key_route_dispatch", true)
		if service.GetKeyAccountRoutingSession(c.Request.Context()) != nil {
			runKeyAccountRouteDispatch(c, next)
			return
		}
		for attempt := 0; attempt <= keyrouting.MaxBackupGroups; attempt++ {
			c.Set("custom.key_route_restart", false)
			service.BeginOpsRouteErrorAttempt(c, 0)
			runKeyRouteFirstContentAttempt(c, next)
			if !c.GetBool("custom.key_route_restart") || c.IsAborted() || c.Writer.Written() || c.Request.Context().Err() != nil {
				return
			}
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "upstream_error", "message": "Configured group attempts exhausted"}})
	}
}

// The deadline cancels only a detached upstream attempt. This function stays
// on the handler goroutine and waits for all native cleanup before re-entry.
func runKeyRouteFirstContentAttempt(c *gin.Context, next gin.HandlerFunc) {
	value, _ := c.Get(keyRoutingSelectorContextKey)
	policy, policyOK := value.(keyRouteFirstContentSelector)
	selector, selectorOK := value.(keyRouteFailoverSelector)
	key, keyOK := servermiddleware.GetAPIKeyFromContext(c)
	request, requestOK := keyRouteRequest(c)
	if !policyOK || !selectorOK || !keyOK || key == nil || key.GroupID == nil || !requestOK {
		next(c)
		return
	}
	groupID := *key.GroupID
	root := cloneKeyRoutingPrimaryKey(c, key)
	wait, err := policy.FirstContentTimeout(c.Request.Context(), root, request, groupID)
	if err != nil || wait <= 0 {
		next(c)
		return
	}
	// A last eligible group must be allowed to finish rather than turn a
	// single remaining route into a predictable user-facing timeout.
	activeWait := wait
	if candidate, _, lookupErr := selector.SelectNext(c.Request.Context(), root, request, groupID); lookupErr != nil || candidate == nil {
		activeWait = 0
	}
	guard := keyrouting.NewFirstContentGuard(activeWait)
	originalWriter := c.Writer
	writer := newFirstContentWriter(originalWriter, guard, activeWait > 0)
	c.Writer = writer
	c.Request = c.Request.WithContext(keyrouting.WithFirstContentGuard(c.Request.Context(), guard))
	next(c)
	elapsed, observed := guard.FirstContentElapsed()
	if guard.Expired() && guard.ReplaySafe() {
		elapsed, observed = wait, true
	}
	// Fast failures still occupy the recent-N window. No native dispatch
	// (for example, local admission failure) must not create a TTFT sample.
	if !observed && keyRouteHasCountedOutcome(c, groupID) {
		elapsed, observed = guard.AttemptElapsed()
	}
	if observed && c.Request.Context().Err() == nil {
		_, _ = policy.RecordFirstContent(c.Request.Context(), root, request, groupID, elapsed)
	}
	expiredSafe := guard.ReplaySafe() && !originalWriter.Written() && c.Request.Context().Err() == nil && !c.IsAborted()
	restarting := c.GetBool("custom.key_route_restart")
	writer.Finalize(expiredSafe || restarting)
	guard.Finish()
	c.Writer = originalWriter
	if !expiredSafe || restarting {
		return
	}
	// Do not manufacture a provider HTTP 504: this was a Key-local deadline,
	// not an upstream status or an account health failure.
	platform := ""
	if key.Group != nil {
		platform = key.Group.Platform
	}
	service.RecordKeyRouteFirstContentTimeout(c, platform, request.Model)
	recordKeyRouteFailure(c, 0, keyrouting.OutcomeTimeout, false)
	if _, switched := switchKeyRoute(c, key, request, selector); switched {
		c.Set("custom.key_route_restart", true)
		return
	}
	c.JSON(http.StatusGatewayTimeout, gin.H{"error": gin.H{"type": "key_route_first_content_timeout", "message": "First-content wait expired and no eligible backup group remains"}})
}

// Called only after the existing group/account retry loop has finished.
// A group result counts once, not once per account retry. Local admission
// errors and client cancellation never enter this hook.
func tryFinalKeyRouteFailover(c *gin.Context, failure *service.UpstreamFailoverError) bool {
	if failure == nil {
		return false
	}
	if c != nil && c.Request != nil && service.GetKeyAccountRoutingSession(c.Request.Context()) != nil {
		return tryKeyAccountGroupExhausted(c)
	}
	return recordKeyRouteFailure(c, failure.StatusCode, keyRouteOutcome(failure), true)
}

func recordKeyRouteFailure(c *gin.Context, status int, outcome keyrouting.RouteOutcomeKind, allowSwitch bool) bool {
	if c != nil && c.Request != nil && service.GetKeyAccountRoutingSession(c.Request.Context()) != nil {
		return false // Real wire attempts own account_v2 samples, not group hooks.
	}
	if c == nil || c.Request == nil || !c.GetBool("custom.key_route_dispatch") || !keyRoutingRequestActive(c.Request.Context()) || c.Request.Context().Err() != nil {
		return false
	}
	key, ok := servermiddleware.GetAPIKeyFromContext(c)
	request, requestOK := keyRouteRequest(c)
	if !ok || key == nil || key.GroupID == nil || !requestOK {
		return false
	}
	if !claimKeyRouteOutcome(c, *key.GroupID, outcome) {
		return false
	}
	value, _ := c.Get(keyRoutingSelectorContextKey)
	selector, ok := value.(keyRouteFailoverSelector)
	if !ok {
		return false
	}
	root := cloneKeyRoutingPrimaryKey(c, key)
	var statusResult keyrouting.RouteFailureResult
	var statusErr error
	if status >= 400 && status <= 599 {
		statusResult, statusErr = selector.RecordFailure(c.Request.Context(), root, request, *key.GroupID, int64(status))
	}
	var outcomeResult keyrouting.RouteFailureResult
	if recorder, ok := value.(keyRouteOutcomeRecorder); ok {
		outcomeResult, _ = recorder.RecordOutcome(c.Request.Context(), root, request, *key.GroupID, outcome)
	}
	if !allowSwitch || statusErr != nil || (!statusResult.Triggered && !outcomeResult.Triggered) || c.Writer.Written() || c.Writer.Size() > 0 {
		return false
	}
	if _, switched := switchKeyRoute(c, key, request, selector); switched {
		c.Set("custom.key_route_restart", true)
		return true
	}
	return false
}

// Forwarding can fail after partial usage was received without returning an
// UpstreamFailoverError. Observe only stable transport/stream classifications;
// do not infer status codes from error text or replay these uncertain results.
// The native handler still drains and settles partial usage as before.
func recordKeyRouteForwardFailure(c *gin.Context, err error) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, service.ErrUpstreamResponseBodyTooLarge) {
		return
	}
	kind := keyrouting.OutcomeIgnored
	var transport net.Error
	_, _, streamFailure := service.OpenAIUpstreamStreamReadErrorDetails(err)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		kind = keyrouting.OutcomeTimeout
	case errors.As(err, &transport):
		kind = keyrouting.OutcomeFailure
		if transport.Timeout() {
			kind = keyrouting.OutcomeTimeout
		}
	case errors.Is(err, io.ErrUnexpectedEOF), streamFailure:
		kind = keyrouting.OutcomeFailure
	default:
		return
	}
	// There may be no HTTP response at all. Never manufacture a 502/504 for
	// exact-status counters; semantic timeout/failure rules still see the result.
	recordKeyRouteFailure(c, 0, kind, false)
}

func keyRouteOutcome(failure *service.UpstreamFailoverError) keyrouting.RouteOutcomeKind {
	switch failure.StatusCode {
	case 408, 504, 522, 524:
		return keyrouting.OutcomeTimeout
	case 429:
		return keyrouting.OutcomeRateLimit
	default:
		if failure.StatusCode >= 500 || failure.IsCredentialFailure() {
			return keyrouting.OutcomeFailure
		}
		return keyrouting.OutcomeIgnored
	}
}

func keyRouteRequest(c *gin.Context) (service.KeyRoutingRequest, bool) {
	value, ok := c.Get("custom.key_route_request")
	request, valid := value.(service.KeyRoutingRequest)
	return request, ok && valid && request.Model != "" && request.Endpoint != ""
}

func claimKeyRouteOutcome(c *gin.Context, groupID int64, outcome keyrouting.RouteOutcomeKind) bool {
	value, _ := c.Get("custom.key_route_results")
	seen, _ := value.(map[int64]bool)
	if seen == nil {
		seen = make(map[int64]bool)
		c.Set("custom.key_route_results", seen)
	}
	if seen[groupID] {
		return false
	}
	seen[groupID] = true
	value, _ = c.Get("custom.key_route_outcome_kinds")
	kinds, _ := value.(map[int64]keyrouting.RouteOutcomeKind)
	if kinds == nil {
		kinds = make(map[int64]keyrouting.RouteOutcomeKind)
		c.Set("custom.key_route_outcome_kinds", kinds)
	}
	kinds[groupID] = outcome
	return true
}

func keyRouteHasCountedOutcome(c *gin.Context, groupID int64) bool {
	value, _ := c.Get("custom.key_route_outcome_kinds")
	kinds, _ := value.(map[int64]keyrouting.RouteOutcomeKind)
	kind, ok := kinds[groupID]
	return ok && kind != keyrouting.OutcomeIgnored
}

// A completion hook, not a second usage log. Success resets consecutive
// failures and occupies the recent-N sample window.
func recordKeyRouteSuccess(c *gin.Context) {
	if c != nil && c.Request != nil && service.GetKeyAccountRoutingSession(c.Request.Context()) != nil {
		return
	}
	if c == nil || c.Request == nil || !keyRoutingRequestActive(c.Request.Context()) || c.Request.Context().Err() != nil {
		return
	}
	key, ok := servermiddleware.GetAPIKeyFromContext(c)
	request, requestOK := keyRouteRequest(c)
	if !ok || key == nil || key.GroupID == nil || !requestOK || !claimKeyRouteOutcome(c, *key.GroupID, keyrouting.OutcomeSuccess) {
		return
	}
	value, _ := c.Get(keyRoutingSelectorContextKey)
	if recorder, ok := value.(keyRouteOutcomeRecorder); ok {
		_, _ = recorder.RecordOutcome(c.Request.Context(), cloneKeyRoutingPrimaryKey(c, key), request, *key.GroupID, keyrouting.OutcomeSuccess)
	}
}
