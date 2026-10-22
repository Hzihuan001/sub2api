package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

const keyRoutingSelectorContextKey = "custom.key_routing_selector"

type keyRouteFailoverSelector interface {
	RecordFailure(context.Context, *service.APIKey, service.KeyRoutingRequest, int64, int64) (keyrouting.RouteFailureResult, error)
	SelectNext(context.Context, *service.APIKey, service.KeyRoutingRequest, int64) (*service.APIKey, keyrouting.Plan, error)
}

// switchKeyRoute only installs the selected group's clean context and body.
// Final-result collection and the bounded native handler loop live in dispatch.
func switchKeyRoute(c *gin.Context, effectiveKey *service.APIKey, request service.KeyRoutingRequest, selector keyRouteFailoverSelector) (*service.APIKey, bool) {
	if c == nil || c.Request == nil || effectiveKey == nil || effectiveKey.GroupID == nil || *effectiveKey.GroupID <= 0 || selector == nil || c.Writer == nil || c.Writer.Written() || c.Writer.Size() > 0 || c.Request.Context().Err() != nil {
		return effectiveKey, false
	}
	// Native parsing can observe billable usage before flushing any response,
	// and may return a retryable error without its partial result. The writer's
	// untouched state alone therefore cannot establish replay safety.
	if guard := keyrouting.GetFirstContentGuard(c.Request.Context()); guard.ReplayBlocked() {
		return effectiveKey, false
	}
	if keyAccountForwardReplayBlocked(c) {
		return effectiveKey, false
	}
	currentGroupID := *effectiveKey.GroupID
	rootKey := cloneKeyRoutingPrimaryKey(c, effectiveKey)
	// Avoid one speculative SelectNext call after the last configured group.
	// Besides unnecessary repository/scheduler work, that call records a
	// synthetic switch attempt in route diagnostics even though no group can
	// follow it. The configured route is strictly primary followed by backups.
	configuredGroups := append([]int64(nil), rootKey.BackupGroupIDs...)
	configuredGroups = append([]int64{derefKeyRouteGroupID(rootKey)}, configuredGroups...)
	last := -1
	for i, id := range configuredGroups {
		if id == currentGroupID {
			last = i
			break
		}
	}
	if last < 0 || last >= len(configuredGroups)-1 {
		return effectiveKey, false
	}
	next, _, err := selector.SelectNext(c.Request.Context(), rootKey, request, currentGroupID)
	if err != nil || next == nil || next.GroupID == nil || *next.GroupID == currentGroupID || next.Group == nil {
		return effectiveKey, false
	}
	base := c.Request.Context()
	if value, ok := c.Get("custom.key_route_base_context"); ok {
		if original, valid := value.(context.Context); valid {
			base = original
		}
	}
	ctx := context.WithValue(base, ctxkey.Group, next.Group)
	ctx = context.WithValue(ctx, ctxkey.UserID, next.UserID)
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingActive, true)
	// The original request may have resolved a composite group's platform
	// before the switch. Replace that request-local decision so downstream
	// dispatch, quota and logging use the selected backup group.
	if preparer, ok := selector.(keyRouteContextPreparer); ok {
		var upstreamModel string
		ctx, upstreamModel, err = preparer.PrepareRouteContext(ctx, next, request)
		if err != nil {
			return effectiveKey, false
		}
		if value, ok := c.Get("custom.key_route_body"); ok {
			if original, valid := value.([]byte); valid {
				body := original
				if upstreamModel != "" && upstreamModel != request.Model {
					body, err = sjson.SetBytes(original, "model", upstreamModel)
					if err != nil {
						return effectiveKey, false
					}
				}
				requestmodel.ResetRequestBody(c.Request, body)
			}
		}
	} else if next.Group.Platform == service.PlatformComposite {
		if platform, ok := service.DetectModelPlatform(request.Model); ok {
			ctx = service.WithResolvedTargetPlatform(ctx, platform)
		}
	} else {
		ctx = service.WithResolvedTargetPlatform(ctx, next.Group.Platform)
	}
	c.Request = c.Request.WithContext(ctx)
	c.Set(string(servermiddleware.ContextKeyAPIKey), next)
	servermiddleware.SetOpsFallbackAPIKey(c, next)
	c.Set(keyRoutingSelectorContextKey, selector)
	c.Set("custom.key_route_switch_group", strings.TrimSpace(next.Group.Name))
	service.MarkOpsRouteSwitch(c, next.Group.Name)
	return next, true
}

func derefKeyRouteGroupID(key *service.APIKey) int64 {
	if key == nil || key.GroupID == nil {
		return 0
	}
	return *key.GroupID
}

func cloneKeyRoutingPrimaryKey(c *gin.Context, current *service.APIKey) *service.APIKey {
	root := *current
	if c != nil && c.Request != nil {
		if primaryID, ok := c.Request.Context().Value(ctxkey.KeyRoutingPrimaryGroupID).(int64); ok && primaryID > 0 {
			id := primaryID
			root.GroupID = &id
			root.Group = nil
		}
	}
	root.BackupGroupIDs = append([]int64(nil), current.BackupGroupIDs...)
	return &root
}
