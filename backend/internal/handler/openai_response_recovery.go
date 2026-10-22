package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const keyRouteResponseRecoveryAttempted = "custom.key_route_response_recovery_attempted"

// recoverOpenAIResponsesContinuation is the only place where a stored HTTP
// Responses ID may become a stateless request. It is called before any
// semantic bytes or usage have been observed. The provider adapter performs
// all capability and size checks; this coordinator only installs the rebuilt
// body and re-enables ordinary same-group/backup-group routing.
func (h *OpenAIGatewayHandler) recoverOpenAIResponsesContinuation(
	c *gin.Context,
	currentKey *service.APIKey,
	sourceAccountID int64,
	responseID string,
	body []byte,
	model string,
) (*service.APIKey, []byte, bool) {
	if h == nil || h.gatewayService == nil || c == nil || currentKey == nil || sourceAccountID <= 0 || strings.TrimSpace(responseID) == "" {
		return currentKey, body, false
	}
	if attempted, _ := c.Get(keyRouteResponseRecoveryAttempted); attempted == true {
		return currentKey, body, false
	}
	c.Set(keyRouteResponseRecoveryAttempted, true)
	result, err := h.gatewayService.RecoverOpenAIHTTPContinuation(c.Request.Context(), sourceAccountID, responseID, body, model)
	if err != nil || result == nil || len(result.Body) == 0 {
		return currentKey, body, false
	}

	// The current source clone already contains only groups after the source
	// group. Keep it as the new route root: source-group accounts are preferred,
	// then the remaining configured backups are tried in order.
	recoveredKey := *currentKey
	recoveredKey.BackupGroupIDs = append([]int64(nil), currentKey.BackupGroupIDs...)
	if recoveredKey.GroupID == nil || *recoveredKey.GroupID <= 0 || recoveredKey.Group == nil {
		return currentKey, body, false
	}

	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingActive, true)
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingPrimaryGroupID, *recoveredKey.GroupID)
	// Override the source pin with zero values. Context values are immutable, so
	// WithValue is used instead of attempting to delete the old pin.
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingResponseSourceGroupID, int64(0))
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingResponseSourceAccountID, int64(0))
	c.Request = c.Request.WithContext(ctx)
	requestmodel.ResetRequestBody(c.Request, result.Body)
	c.Set(string(servermiddleware.ContextKeyAPIKey), &recoveredKey)
	servermiddleware.SetOpsFallbackAPIKey(c, &recoveredKey)
	c.Set("custom.key_route_root_key", &recoveredKey)
	c.Set("custom.key_route_base_context", ctx)
	c.Set("custom.key_route_body", append([]byte(nil), result.Body...))
	return &recoveredKey, result.Body, true
}

func isOpenAIResponseRecoveryEligibleError(err error) bool {
	if err == nil {
		return false
	}
	var failoverErr *service.UpstreamFailoverError
	if !errors.As(err, &failoverErr) {
		return false
	}
	// A replay is only considered after the forwarding layer has returned an
	// explicit HTTP failure with no semantic bytes. Transport timeouts/EOFs do
	// not prove that the provider rejected the original request and therefore
	// must never be replayed automatically.
	if len(failoverErr.ResponseBody) == 0 {
		return false
	}
	switch failoverErr.StatusCode {
	case 429, 500, 502, 503, 504:
		return true
	default:
		// Client validation, authorization, missing-model and not-found
		// responses are deterministic request errors, not account failover
		// signals. Replaying them would add latency and could hide the real
		// error from the caller.
		return false
	}
}
