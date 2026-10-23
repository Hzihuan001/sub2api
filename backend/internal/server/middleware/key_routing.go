package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
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

type apiKeyHTTPResponseSourceSelector interface {
	SelectHTTPResponseSource(context.Context, *service.APIKey, service.KeyRoutingRequest, string) (*service.APIKey, int64, error)
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
		// Ordinary model discovery exposes a read-only catalogue source without
		// selecting a group. Other metadata, media, WS and resource endpoints
		// retain the primary group's behavior; never guess a resource's owner.
		setAPIKeyRouteCatalogueSource(c, selector)
		return key, true
	}
	body, readErr := httputil.ReadRequestBodyWithPrealloc(c.Request)
	if readErr != nil {
		status := http.StatusBadRequest
		var maxErr *http.MaxBytesError
		if errors.As(readErr, &maxErr) {
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
	previousResponseID, hasPreviousResponseID, validPreviousResponseID := keyRoutingPreviousResponseID(body)
	if !validPreviousResponseID {
		// An unusable provider reference is not a reason for the relay to reject
		// the user's turn. Remove the opaque pointer and keep the request content.
		var stripErr error
		body, stripErr = keyRoutingDropStoredResponseReferences(body)
		if stripErr == nil {
			requestmodel.ResetRequestBody(c.Request, body)
			slog.WarnContext(c.Request.Context(), "key_route_invalid_response_reference_using_current_input", "api_key_id", key.ID)
			hasPreviousResponseID = false
			previousResponseID = ""
		} else {
			slog.WarnContext(c.Request.Context(), "key_route_response_reference_strip_failed_using_primary", "api_key_id", key.ID, "error", stripErr)
			return key, true
		}
	}
	if hasPreviousResponseID && endpoint != service.CompositeRouteEndpointResponses {
		// Do not impose an endpoint restriction at the relay. A reference from
		// another protocol cannot safely cross credentials, so retry only the
		// content explicitly present in this request.
		var stripErr error
		body, stripErr = keyRoutingDropStoredResponseReferences(body)
		if stripErr == nil {
			requestmodel.ResetRequestBody(c.Request, body)
			previousResponseID = ""
			hasPreviousResponseID = false
			slog.WarnContext(c.Request.Context(), "key_route_non_responses_reference_using_current_input", "api_key_id", key.ID, "endpoint", endpoint)
		} else {
			slog.WarnContext(c.Request.Context(), "key_route_non_responses_reference_strip_failed_using_primary", "api_key_id", key.ID, "endpoint", endpoint, "error", stripErr)
			return key, true
		}
	}
	// Stateful references are deliberately preserved and dispatched, rather
	// than rejected here. Responses references are pinned to their source when
	// known; if that account cannot serve the request, the gateway first tries
	// history recovery, then removes only unavailable provider-owned pointers
	// before routing the current request content through another account/group.
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
	if selector == nil {
		AbortWithError(c, http.StatusServiceUnavailable, "KEY_ROUTE_UNAVAILABLE", "Multi-group routing is unavailable")
		return nil, false
	}
	if key.KeyRouteScope == service.KeyRouteScopeAccountV2 {
		preparer, ok := selector.(interface {
			PrepareAccountRouteContext(context.Context, *service.APIKey, service.KeyRoutingRequest) (context.Context, error)
		})
		if !ok {
			AbortWithError(c, http.StatusServiceUnavailable, "KEY_ROUTE_UNAVAILABLE", "Account-scoped key routing is unavailable")
			return nil, false
		}
		ctx, prepareErr := preparer.PrepareAccountRouteContext(c.Request.Context(), key, service.KeyRoutingRequest{Model: strings.TrimSpace(model.String()), Endpoint: endpoint})
		if prepareErr != nil {
			slog.WarnContext(c.Request.Context(), "key_account_route_prepare_failed", "api_key_id", key.ID, "error", prepareErr)
			AbortWithError(c, http.StatusServiceUnavailable, "KEY_ROUTE_UNAVAILABLE", "Unable to prepare account-scoped routing; retry later")
			return nil, false
		}
		c.Request = c.Request.WithContext(ctx)
	}
	routingRequest := service.KeyRoutingRequest{Model: strings.TrimSpace(model.String()), Endpoint: endpoint}
	var effective *service.APIKey
	var plan keyrouting.Plan
	var sourceAccountID int64
	var err error
	if hasPreviousResponseID {
		sourceSelector, supported := selector.(apiKeyHTTPResponseSourceSelector)
		if !supported {
			// The referenced server-side state cannot be trusted or retrieved, so
			// never forward the opaque ID to another credential. Drop only that
			// provider-owned pointer and route the content actually present in this
			// request through the normal group selector.
			body, err = keyRoutingDropStoredResponseReferences(body)
			if err != nil {
				slog.WarnContext(c.Request.Context(), "key_route_response_reference_strip_failed_using_primary", "api_key_id", key.ID, "error", err)
				return key, true
			}
			requestmodel.ResetRequestBody(c.Request, body)
			hasPreviousResponseID = false
			previousResponseID = ""
			slog.WarnContext(c.Request.Context(), "key_route_response_source_unavailable_using_current_input", "api_key_id", key.ID)
		} else {
			effective, sourceAccountID, err = sourceSelector.SelectHTTPResponseSource(c.Request.Context(), key, routingRequest, previousResponseID)
			if err != nil {
				// A missing/expired owner mapping and a lookup failure both mean the
				// gateway cannot safely fetch that state. Since it will strip the ID,
				// falling back to the current request input cannot expose the foreign
				// response; it only avoids failing a request that already carries its
				// own user content.
				slog.WarnContext(c.Request.Context(), "key_route_response_source_unavailable_using_current_input", "api_key_id", key.ID, "error", err)
				body, err = keyRoutingDropStoredResponseReferences(body)
				if err != nil {
					slog.WarnContext(c.Request.Context(), "key_route_response_reference_strip_failed_using_primary", "api_key_id", key.ID, "error", err)
					return key, true
				}
				requestmodel.ResetRequestBody(c.Request, body)
				hasPreviousResponseID = false
				previousResponseID = ""
			} else if sourceAccountID <= 0 || effective == nil || effective.GroupID == nil || effective.Group == nil {
				body, err = keyRoutingDropStoredResponseReferences(body)
				if err != nil {
					slog.WarnContext(c.Request.Context(), "key_route_response_reference_strip_failed_using_primary", "api_key_id", key.ID, "error", err)
					return key, true
				}
				requestmodel.ResetRequestBody(c.Request, body)
				effective = nil
				hasPreviousResponseID = false
				previousResponseID = ""
			}
			if hasPreviousResponseID {
				plan.SelectedGroupID = *effective.GroupID
			}
		}
	}
	if !hasPreviousResponseID {
		effective, plan, err = selector.Select(c.Request.Context(), key, routingRequest)
	}
	if err != nil {
		switch {
		case errors.Is(err, keyrouting.ErrNotAuthorized):
			AbortWithError(c, http.StatusForbidden, "KEY_ROUTE_NOT_AUTHORIZED", "A configured group is no longer authorized; update the key's group configuration")
		case errors.Is(err, keyrouting.ErrInvalidConfig):
			AbortWithError(c, http.StatusBadRequest, "KEY_ROUTE_INVALID_CONFIG", "Invalid key group configuration")
		case errors.Is(err, keyrouting.ErrModelUnavailable):
			AbortWithError(c, http.StatusNotFound, "MODEL_NOT_FOUND", "No authorized group supports this model and endpoint")
		case errors.Is(err, keyrouting.ErrNoSchedulableAccounts):
			if wait := service.KeyAccountRouteRetryAfter(c.Request.Context()); wait > 0 {
				c.Header("Retry-After", strconv.FormatInt(int64((wait+time.Second-1)/time.Second), 10))
			}
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
	primaryGroupID := int64(0)
	if key.GroupID != nil {
		primaryGroupID = *key.GroupID
	}
	routingCtx := c.Request.Context()
	if hasPreviousResponseID {
		// A stored Responses ID starts pinned to its source, but can be routed
		// through the normal bounded failover path if that account is unavailable.
		// Preserve the verified owner group separately so a backup-group retry
		// does not validate the ID against the backup's local ownership map.
		routingCtx = context.WithValue(routingCtx, ctxkey.KeyRoutingActive, true)
		routingCtx = context.WithValue(routingCtx, ctxkey.KeyRoutingPrimaryGroupID, *effective.GroupID)
		routingCtx = context.WithValue(routingCtx, ctxkey.KeyRoutingResponseSourceGroupID, *effective.GroupID)
		routingCtx = context.WithValue(routingCtx, ctxkey.KeyRoutingResponseSourceAccountID, sourceAccountID)
		routingCtx = context.WithValue(routingCtx, ctxkey.KeyRoutingResponseOwnerGroupID, *effective.GroupID)
	} else {
		routingCtx = context.WithValue(routingCtx, ctxkey.KeyRoutingActive, true)
		if primaryGroupID > 0 {
			routingCtx = context.WithValue(routingCtx, ctxkey.KeyRoutingPrimaryGroupID, primaryGroupID)
		}
	}
	c.Request = c.Request.WithContext(routingCtx)
	// Retain the immutable ingress snapshot before platform/model/pricing
	// adapters add group-specific context. A backup dispatch must start from
	// this snapshot, never from a previous group's forwarding context.
	c.Set("custom.key_route_base_context", routingCtx)
	c.Set("custom.key_route_body", append([]byte(nil), body...))
	c.Set("custom.key_route_request", service.KeyRoutingRequest{Model: strings.TrimSpace(model.String()), Endpoint: endpoint})
	c.Set("custom.key_routing_selector", selector)
	// Keep an immutable copy of the user's primary+backup route. A stored
	// Responses continuation is initially pinned to its source group/account;
	// a later capability-gated replay may need to resume normal routing from
	// that source group without reconstructing the original key from mutated
	// request context.
	rootSnapshot := *key
	rootSnapshot.BackupGroupIDs = append([]int64(nil), key.BackupGroupIDs...)
	c.Set("custom.key_route_root_key", &rootSnapshot)
	c.Set("custom.key_route", plan)
	if key.GroupID != nil && *effective.GroupID != *key.GroupID {
		if !hasPreviousResponseID && c.Request.Header != nil {
			// This opaque Codex state is minted by an upstream identity. When the
			// request is routed by model to another group, do not send that state to
			// the unrelated account; the current request content remains intact.
			c.Request.Header.Del("X-Codex-Turn-State")
		}
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

func keyRoutingPreviousResponseID(body []byte) (responseID string, present bool, valid bool) {
	valid = true
	gjson.ParseBytes(body).ForEach(func(field, value gjson.Result) bool {
		if !strings.EqualFold(field.String(), "previous_response_id") {
			return true
		}
		if value.Type == gjson.Null || (value.Type == gjson.String && strings.TrimSpace(value.String()) == "") {
			return true
		}
		present = true
		if value.Type != gjson.String {
			valid = false
			return false
		}
		responseID = strings.TrimSpace(value.String())
		valid = responseID != ""
		return false
	})
	return responseID, present, valid
}

// keyRoutingDropStoredResponseReferences removes only provider-owned
// continuation pointers. It deliberately preserves the current request input,
// instructions, tools, and all other user-supplied content for a stateless
// retry when the referenced server-side history cannot be retrieved.
func keyRoutingDropStoredResponseReferences(body []byte) ([]byte, error) {
	return service.RemoveOpenAIStoredResponseReferencesFromBody(body)
}
