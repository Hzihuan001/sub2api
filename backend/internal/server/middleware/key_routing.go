package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// APIKeyRouteSelector is the single ingress extension seam. The selector must
// finish before group admission, platform dispatch, quota checks or forwarding.
type APIKeyRouteSelector interface {
	Select(context.Context, *service.APIKey, service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error)
}

// ProvideAPIKeyAuthMiddleware is the production composition. The upstream
// constructor remains source-compatible for existing integrations/tests.
func ProvideAPIKeyAuthMiddleware(keys *service.APIKeyService, subscriptions *service.SubscriptionService, cfg *config.Config, routes *service.KeyRoutingService) APIKeyAuthMiddleware {
	return APIKeyAuthMiddleware(apiKeyAuthWithSubscription(keys, subscriptions, cfg, routes))
}

func selectAPIKeyGroupRoute(c *gin.Context, key *service.APIKey, selector APIKeyRouteSelector) (*service.APIKey, bool) {
	if len(key.BackupGroupIDs) == 0 {
		return key, true
	}
	endpoint := keyRoutingTextEndpoint(c.Request.Method, c.Request.URL.Path)
	if endpoint == "" {
		// Metadata, media, WS, forced-provider and resource endpoints retain the
		// primary group's existing behavior. Never guess a resource's owner.
		return key, true
	}
	body, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		status := http.StatusBadRequest
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status = http.StatusRequestEntityTooLarge
		}
		AbortWithError(c, status, "KEY_ROUTE_INVALID_REQUEST", "Unable to read routing request")
		return nil, false
	}
	requestmodel.ResetRequestBody(c.Request, body)
	if !keyRoutingJSONUnambiguous(body) {
		AbortWithError(c, http.StatusBadRequest, "KEY_ROUTE_INVALID_REQUEST", "Expected an unambiguous JSON object without duplicate fields")
		return nil, false
	}
	if keyRoutingHasBoundState(body) || c.GetHeader("x-codex-turn-state") != "" {
		AbortWithError(c, http.StatusBadRequest, "KEY_ROUTE_STATEFUL_UNSUPPORTED", "Multi-group keys currently require stateless text requests; use a single-group key for stored response or conversation references")
		return nil, false
	}
	// This branch has already established that the body is JSON. A forged
	// multipart Content-Type must not suppress duplicate/case-folded candidates.
	models := requestmodel.FromBodyCandidates(c.FullPath(), "application/json", body)
	model := gjson.GetBytes(body, "model")
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		AbortWithError(c, http.StatusBadRequest, "KEY_ROUTE_INVALID_MODEL", "model must be a non-empty string")
		return nil, false
	}
	for _, candidate := range models {
		if candidate != strings.TrimSpace(model.String()) {
			AbortWithError(c, http.StatusBadRequest, "KEY_ROUTE_INVALID_MODEL", "Conflicting model fields are not supported")
			return nil, false
		}
	}
	// Native remote compaction and image generation have independent lifecycle
	// and pricing rules. They do not participate in text backup routing.
	if service.IsExplicitImageGenerationIntent(c.Request.URL.Path, model.String(), body) || keyRoutingHasPrimaryOnlyIntent(body) {
		return key, true
	}
	if selector == nil {
		AbortWithError(c, http.StatusServiceUnavailable, "KEY_ROUTE_UNAVAILABLE", "Multi-group routing is unavailable")
		return nil, false
	}
	effective, plan, err := selector.Select(c.Request.Context(), key, service.KeyRoutingRequest{Model: strings.TrimSpace(model.String()), Endpoint: endpoint})
	if err != nil {
		switch {
		case errors.Is(err, keyrouting.ErrNotAuthorized):
			AbortWithError(c, http.StatusForbidden, "KEY_ROUTE_NOT_AUTHORIZED", "A configured group is no longer authorized; update the key's group configuration")
		case errors.Is(err, keyrouting.ErrInvalidConfig):
			AbortWithError(c, http.StatusBadRequest, "KEY_ROUTE_INVALID_CONFIG", "Invalid key group configuration")
		case errors.Is(err, keyrouting.ErrModelUnavailable):
			AbortWithError(c, http.StatusNotFound, "MODEL_NOT_FOUND", "No authorized group supports this model and endpoint")
		case errors.Is(err, keyrouting.ErrNoSchedulableAccounts):
			AbortWithError(c, http.StatusServiceUnavailable, "NO_AVAILABLE_ACCOUNTS", "No schedulable account is available in the configured groups")
		default:
			slog.WarnContext(c.Request.Context(), "key_route_resolution_failed", "api_key_id", key.ID, "error", err)
			AbortWithError(c, http.StatusServiceUnavailable, "KEY_ROUTE_UNAVAILABLE", "Unable to resolve key group routing; retry later")
		}
		return nil, false
	}
	if effective == nil || effective.Group == nil || effective.GroupID == nil || plan.SelectedGroupID != *effective.GroupID {
		AbortWithError(c, http.StatusServiceUnavailable, "KEY_ROUTE_UNAVAILABLE", "Invalid routing result")
		return nil, false
	}
	c.Set("custom.key_route", plan)
	if key.GroupID != nil && *effective.GroupID != *key.GroupID {
		slog.InfoContext(c.Request.Context(), "key_route_selected", "api_key_id", key.ID, "primary_group_id", *key.GroupID, "selected_group_id", *effective.GroupID, "reason", plan.Reason)
	}
	SetOpsFallbackAPIKey(c, effective)
	return effective, true
}

func keyRoutingTextEndpoint(method, path string) string {
	if method != http.MethodPost {
		return ""
	}
	switch path {
	case "/v1/messages":
		return service.CompositeRouteEndpointMessages
	case "/v1/chat/completions", "/chat/completions":
		return service.CompositeRouteEndpointChatCompletions
	case "/v1/responses", "/responses", "/backend-api/codex/responses":
		return service.CompositeRouteEndpointResponses
	default:
		return ""
	}
}

// Protocol keys can bind case-insensitively in downstream JSON structs. Inspect
// only protocol-owned positions so function schemas and user content remain
// ordinary text, even when they describe image or context-management fields.
func keyRoutingHasPrimaryOnlyIntent(body []byte) bool {
	primaryOnly := false
	gjson.ParseBytes(body).ForEach(func(field, value gjson.Result) bool {
		switch {
		case strings.EqualFold(field.String(), "context_management"):
			primaryOnly = true
		case strings.EqualFold(field.String(), "tools") && value.IsArray():
			value.ForEach(func(_, tool gjson.Result) bool {
				primaryOnly = keyRoutingIsImageGenerationTool(tool)
				return !primaryOnly
			})
		case strings.EqualFold(field.String(), "tool_choice"):
			primaryOnly = keyRoutingIsImageGenerationTool(value)
		}
		return !primaryOnly
	})
	return primaryOnly
}

func keyRoutingIsImageGenerationTool(tool gjson.Result) bool {
	if !tool.IsObject() {
		return false
	}
	imageGeneration := false
	tool.ForEach(func(field, value gjson.Result) bool {
		if strings.EqualFold(field.String(), "type") && value.Type == gjson.String && strings.TrimSpace(value.String()) == "image_generation" {
			imageGeneration = true
		}
		return !imageGeneration
	})
	return imageGeneration
}

func keyRoutingHasBoundState(body []byte) bool {
	bound := false
	gjson.ParseBytes(body).ForEach(func(k, v gjson.Result) bool {
		switch strings.ToLower(k.String()) {
		case "previous_response_id", "conversation":
			bound = v.Type != gjson.Null && v.Raw != `""`
		case "background":
			bound = v.Type != gjson.Null && v.Type != gjson.False
		case "input", "messages":
			v.ForEach(func(_, item gjson.Result) bool {
				item.ForEach(func(field, value gjson.Result) bool {
					if strings.EqualFold(field.String(), "type") && value.String() == "item_reference" {
						bound = true
					}
					if strings.EqualFold(field.String(), "file_id") && keyRoutingReferencePresent(value) {
						bound = true
					}
					if strings.EqualFold(field.String(), "content") {
						bound = bound || keyRoutingContentHasFileReference(value)
					}
					return !bound
				})
				return !bound
			})
		case "tools":
			v.ForEach(func(_, item gjson.Result) bool {
				item.ForEach(func(field, value gjson.Result) bool {
					switch strings.ToLower(field.String()) {
					case "vector_store_ids":
						bound = len(value.Array()) > 0
					case "container":
						bound = value.Type == gjson.String && value.String() != "auto" && value.String() != ""
						if value.IsObject() {
							value.ForEach(func(k, v gjson.Result) bool {
								if strings.EqualFold(k.String(), "file_ids") && len(v.Array()) > 0 {
									bound = true
								}
								return !bound
							})
						}
					}
					return !bound
				})
				return !bound
			})
		}
		return !bound
	})
	return bound
}

func keyRoutingReferencePresent(value gjson.Result) bool {
	return value.Type != gjson.Null && value.Raw != `""`
}

// Only protocol content blocks are inspected, not user text, JSON tool
// arguments or function schemas that happen to describe a file_id property.
func keyRoutingContentHasFileReference(content gjson.Result) bool {
	bound := false
	if !content.IsArray() && !content.IsObject() {
		return false
	}
	inspect := func(block gjson.Result) {
		block.ForEach(func(field, value gjson.Result) bool {
			if strings.EqualFold(field.String(), "file_id") && keyRoutingReferencePresent(value) {
				bound = true
			}
			if strings.EqualFold(field.String(), "file") && value.IsObject() {
				value.ForEach(func(k, v gjson.Result) bool {
					if strings.EqualFold(k.String(), "file_id") && keyRoutingReferencePresent(v) {
						bound = true
					}
					return !bound
				})
			}
			return !bound
		})
	}
	if content.IsObject() {
		inspect(content)
	} else {
		content.ForEach(func(_, block gjson.Result) bool { inspect(block); return !bound })
	}
	return bound
}
