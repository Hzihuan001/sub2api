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

// enableOpenAIResponseAccountFallback releases the source pin only after the
// caller has recovered the source history or prepared a safe request body for
// another account. It only resets routing context; it does not rewrite payloads.
func enableOpenAIResponseAccountFallback(c *gin.Context, sourceGroupID int64) bool {
	if c == nil || c.Request == nil || sourceGroupID <= 0 {
		return false
	}
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingResponseSourceGroupID, int64(0))
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingResponseSourceAccountID, int64(0))
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingResponseOwnerGroupID, int64(0))
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingActive, true)
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingPrimaryGroupID, sourceGroupID)
	c.Request = c.Request.WithContext(ctx)
	// Re-entry after account/group exhaustion must restore this enabled routing
	// context, not the originally pinned source context.
	c.Set("custom.key_route_base_context", ctx)
	return true
}

// isOpenAIResponseReferenceRejectedError only matches deterministic provider
// errors that explicitly reject the stored response reference. Generic 4xx,
// 5xx, timeouts, and transport failures are not evidence that a replay is safe.
func isOpenAIResponseReferenceRejectedError(err error) bool {
	if err == nil {
		return false
	}
	var failoverErr *service.UpstreamFailoverError
	if !errors.As(err, &failoverErr) {
		return false
	}
	return service.IsOpenAIStoredResponseReferenceRejection(failoverErr.StatusCode, failoverErr.ResponseBody)
}

// dropOpenAIStoredResponseReferences preserves the current request payload and
// removes only provider-owned continuation pointers. It is the last-resort
// stateless path when source history cannot be retrieved.
func dropOpenAIStoredResponseReferences(body []byte) ([]byte, error) {
	return service.RemoveOpenAIStoredResponseReferencesFromBody(body)
}

// dropOpenAIStoredResponseForFallback clears the opaque source pin only after
// the source history is unavailable. The route resumes at the source group,
// then follows the key's normal configured backup order.
func dropOpenAIStoredResponseForFallback(c *gin.Context, body []byte, groupID int64) ([]byte, error) {
	updated, err := dropOpenAIStoredResponseReferences(body)
	if err != nil {
		return nil, err
	}
	requestmodel.ResetRequestBody(c.Request, updated)
	if c.Request != nil && c.Request.Header != nil {
		c.Request.Header.Del("X-Codex-Turn-State")
	}
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingResponseSourceGroupID, int64(0))
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingResponseSourceAccountID, int64(0))
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingResponseOwnerGroupID, int64(0))
	if active, _ := ctx.Value(ctxkey.KeyRoutingActive).(bool); active && groupID > 0 {
		ctx = context.WithValue(ctx, ctxkey.KeyRoutingPrimaryGroupID, groupID)
	}
	c.Request = c.Request.WithContext(ctx)
	c.Set("custom.key_route_base_context", ctx)
	c.Set("custom.key_route_body", append([]byte(nil), updated...))
	return updated, nil
}
