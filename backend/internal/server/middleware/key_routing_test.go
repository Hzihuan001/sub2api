//go:build unit

package middleware

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type keyRouteSelectorFunc func(context.Context, *service.APIKey, service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error)

func (f keyRouteSelectorFunc) Select(ctx context.Context, key *service.APIKey, req service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
	return f(ctx, key, req)
}

type keyRouteResponseSourceSelector struct {
	key    *service.APIKey
	group  int64
	acct   int64
	called bool
}

func (s *keyRouteResponseSourceSelector) Select(context.Context, *service.APIKey, service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
	return nil, keyrouting.Plan{}, errors.New("ordinary selector must not be called for a stored response")
}

func (s *keyRouteResponseSourceSelector) SelectHTTPResponseSource(_ context.Context, _ *service.APIKey, _ service.KeyRoutingRequest, responseID string) (*service.APIKey, int64, error) {
	s.called = true
	if responseID != "resp_source" {
		return nil, 0, service.ErrOpenAIHTTPResponseSourceUnavailable
	}
	clone := *s.key
	group := *s.key.Group
	group.ID = s.group
	group.Status = service.StatusActive
	group.ModelAllowlist = service.GroupModelAllowlist{}
	clone.GroupID = &group.ID
	clone.Group = &group
	clone.BackupGroupIDs = nil
	return &clone, s.acct, nil
}

func TestKeyRoutingStoredResponsePinsSourceGroupAndAccount(t *testing.T) {
	key := routeTestKey()
	selector := &keyRouteResponseSourceSelector{key: key, group: 2, acct: 42}
	router := keyRouteTestRouter(t, key, selector, http.MethodPost, "/v1/responses", func(c *gin.Context) {
		got, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.EqualValues(t, 2, *got.GroupID)
		require.Empty(t, got.BackupGroupIDs)
		require.EqualValues(t, 2, c.Request.Context().Value(ctxkey.KeyRoutingResponseSourceGroupID))
		require.EqualValues(t, 42, c.Request.Context().Value(ctxkey.KeyRoutingResponseSourceAccountID))
		require.Nil(t, c.Request.Context().Value(ctxkey.KeyRoutingActive))
		data, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"primary-only","previous_response_id":"resp_source"}`, string(data))
		c.Status(http.StatusOK)
	})
	w := sendKeyRouteRequest(router, http.MethodPost, "/v1/responses", `{"model":"primary-only","previous_response_id":"resp_source"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, selector.called)
}

func TestKeyRoutingStoredResponseRejectsPrimaryOnlyIntent(t *testing.T) {
	key := routeTestKey()
	selector := &keyRouteResponseSourceSelector{key: key, group: 2, acct: 42}
	router := keyRouteTestRouter(t, key, selector, http.MethodPost, "/v1/responses", func(*gin.Context) { t.Fatal("forwarded") })
	w := sendKeyRouteRequest(router, http.MethodPost, "/v1/responses", `{"model":"primary-only","previous_response_id":"resp_source","tools":[{"type":"image_generation"}]}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.False(t, selector.called)
}

func routeTestKey() *service.APIKey {
	group := &service.Group{ID: 1, Status: service.StatusActive, Platform: service.PlatformOpenAI, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"primary-only"}}}
	return &service.APIKey{ID: 10, UserID: 20, Key: "key-routing-test", Status: service.StatusActive, GroupID: &group.ID, Group: group, BackupGroupIDs: []int64{2}, User: &service.User{ID: 20, Status: service.StatusActive, Role: service.RoleUser, Balance: 10}}
}

func keyRouteTestRouter(t *testing.T, key *service.APIKey, selector APIKeyRouteSelector, method, path string, final gin.HandlerFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	router := gin.New()
	router.Handle(method, path, RequestBodyLimit(1024), apiKeyAuthWithSubscription(svc, nil, cfg, selector), GroupModelAllowlist(), final)
	return router
}

func sendKeyRouteRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer key-routing-test")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestKeyRoutingAuthSelectsBeforePrimaryAllowlistAndKeepsBody(t *testing.T) {
	key := routeTestKey()
	key.Group.Status = service.StatusDisabled
	body := `{"model":"backup-model","messages":[{"role":"user","content":"hello"}],"stream":true}`
	calls := 0
	selector := keyRouteSelectorFunc(func(_ context.Context, got *service.APIKey, req service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
		calls++
		require.Equal(t, "backup-model", req.Model)
		require.Equal(t, service.CompositeRouteEndpointChatCompletions, req.Endpoint)
		clone, group := *got, *got.Group
		group.ID, group.Status, group.RateMultiplier = 2, service.StatusActive, 0.37
		group.ModelAllowlist.Models = []string{"backup-model"}
		clone.GroupID, clone.Group = &group.ID, &group
		return &clone, keyrouting.Plan{SelectedGroupID: 2, Reason: keyrouting.ReasonNoSchedulableAccounts}, nil
	})
	router := keyRouteTestRouter(t, key, selector, http.MethodPost, "/v1/chat/completions", func(c *gin.Context) {
		got, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.EqualValues(t, 2, *got.GroupID)
		require.Equal(t, 0.37, got.Group.RateMultiplier)
		require.EqualValues(t, 10, got.ID)
		require.Same(t, got.Group, c.Request.Context().Value(ctxkey.Group))
		data, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, body, string(data))
		c.String(200, "data: first\n\ndata: [DONE]\n\n")
	})
	w := sendKeyRouteRequest(router, "POST", "/v1/chat/completions", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, calls)
	require.EqualValues(t, 1, *key.GroupID)
	require.Equal(t, service.StatusDisabled, key.Group.Status)
}

func TestKeyRoutingAuthNeverBypassesIdentityOrBalance(t *testing.T) {
	for _, mutate := range []func(*service.APIKey){
		func(key *service.APIKey) { key.Status = service.StatusDisabled },
		func(key *service.APIKey) { key.User.Status = service.StatusDisabled },
	} {
		key := routeTestKey()
		mutate(key)
		selector := keyRouteSelectorFunc(func(context.Context, *service.APIKey, service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
			t.Fatal("invalid identity must not reach routing")
			return nil, keyrouting.Plan{}, nil
		})
		router := keyRouteTestRouter(t, key, selector, "POST", "/v1/responses", func(*gin.Context) { t.Fatal("forwarded") })
		require.Equal(t, 401, sendKeyRouteRequest(router, "POST", "/v1/responses", `{"model":"x"}`).Code)
	}
	key := routeTestKey()
	key.User.Balance = -1
	selector := keyRouteSelectorFunc(func(_ context.Context, got *service.APIKey, _ service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
		return got, keyrouting.Plan{SelectedGroupID: 1}, nil
	})
	router := keyRouteTestRouter(t, key, selector, "POST", "/v1/responses", func(*gin.Context) { t.Fatal("forwarded") })
	w := sendKeyRouteRequest(router, "POST", "/v1/responses", `{"model":"primary-only"}`)
	require.Equal(t, 403, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "INSUFFICIENT_BALANCE")
}

func TestKeyRoutingIngressRejectsAmbiguousAndStatefulBeforeSelection(t *testing.T) {
	for _, body := range []string{
		`{"model":"a","model":"b"}`, `{"model":"a","Model":"b"}`,
		`{"model":"a","conversation":{"id":"conv_x"}}`, `{"model":"a","input":[{"type":"item_reference","id":"x"}]}`,
		`{"model":"a","input":[{"type":"message","type":"item_reference","id":"x"}]}`, `{"model":"a","background":true}`,
		`{"model":"a","input":[{"type":"message","content":[{"type":"input_file","file_id":"file_x"}]}]}`,
		`{"model":"a","messages":[{"role":"user","content":[{"type":"file","file":{"file_id":"file_x"}}]}]}`,
		`{"model":"a","tools":[{"type":"file_search","vector_store_ids":["vs_x"]}]}`,
		`{"model":"a","tools":[{"type":"code_interpreter","container":"container_x"}]}`,
		`{"model":"a",`, `[]`, `{"messages":[]}`,
	} {
		t.Run(body, func(t *testing.T) {
			selector := keyRouteSelectorFunc(func(context.Context, *service.APIKey, service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
				t.Fatal("invalid or stateful input reached planner")
				return nil, keyrouting.Plan{}, nil
			})
			router := keyRouteTestRouter(t, routeTestKey(), selector, "POST", "/v1/responses", func(*gin.Context) { t.Fatal("forwarded") })
			require.Equal(t, 400, sendKeyRouteRequest(router, "POST", "/v1/responses", body).Code)
		})
	}
}

func TestKeyRoutingForgedContentTypeCannotHideConflictingModels(t *testing.T) {
	router := keyRouteTestRouter(t, routeTestKey(), nil, "POST", "/v1/responses", func(*gin.Context) { t.Fatal("forwarded") })
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"allowed","model":"blocked"}`))
	req.Header.Set("Authorization", "Bearer key-routing-test")
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "KEY_ROUTE_INVALID_REQUEST")
}

func TestKeyRoutingPrimaryOnlyEndpointsAndLegacyKey(t *testing.T) {
	for _, endpoint := range []struct{ method, path string }{{"GET", "/v1/responses"}, {"POST", "/v1/images/generations"}, {"POST", "/v1/responses/compact"}, {"POST", "/antigravity/v1/messages"}, {"GET", "/v1/usage"}} {
		key := routeTestKey()
		key.Group.ModelAllowlist.Enabled = false
		selector := keyRouteSelectorFunc(func(context.Context, *service.APIKey, service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
			t.Fatal("primary-only endpoint routed")
			return nil, keyrouting.Plan{}, nil
		})
		router := keyRouteTestRouter(t, key, selector, endpoint.method, endpoint.path, func(c *gin.Context) {
			got, _ := GetAPIKeyFromContext(c)
			require.EqualValues(t, 1, *got.GroupID)
			c.Status(200)
		})
		require.Equal(t, 200, sendKeyRouteRequest(router, endpoint.method, endpoint.path, `{}`).Code)
	}
	key := routeTestKey()
	key.BackupGroupIDs = nil
	router := keyRouteTestRouter(t, key, nil, "POST", "/v1/responses", func(c *gin.Context) { c.Status(200) })
	require.Equal(t, 200, sendKeyRouteRequest(router, "POST", "/v1/responses", `{"model":"primary-only","previous_response_id":"resp_x"}`).Code)
}

func TestKeyRoutingPrimaryOnlyIntentMatchesProtocolKeyCasing(t *testing.T) {
	for _, body := range []string{
		`{"model":"primary-only","Tools":[{"type":"image_generation"}]}`,
		`{"model":"primary-only","tools":[{"Type":"image_generation"}]}`,
		`{"model":"primary-only","TOOL_CHOICE":{"Type":"image_generation"}}`,
		`{"model":"primary-only","Context_Management":{"compact_threshold":1000}}`,
		`{"model":"primary-only","CONTEXT_MANAGEMENT":null}`,
		`{"model":"primary-only","input":[{"type":"compaction_trigger"}]}`,
		`{"model":"primary-only","Input":[{"Type":"compaction_trigger"}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			selector := keyRouteSelectorFunc(func(context.Context, *service.APIKey, service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
				t.Fatal("primary-only protocol intent reached planner")
				return nil, keyrouting.Plan{}, nil
			})
			router := keyRouteTestRouter(t, routeTestKey(), selector, "POST", "/v1/responses", func(c *gin.Context) {
				key, ok := GetAPIKeyFromContext(c)
				require.True(t, ok)
				require.EqualValues(t, 1, *key.GroupID)
				gotBody, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, body, string(gotBody))
				c.Status(http.StatusOK)
			})
			require.Equal(t, http.StatusOK, sendKeyRouteRequest(router, "POST", "/v1/responses", body).Code)
		})
	}
}

func TestKeyRoutingIngressDoesNotTreatToolSchemasAsPrimaryOnlyIntent(t *testing.T) {
	body := `{"model":"primary-only","tools":[{"type":"function","name":"describe","parameters":{"type":"object","properties":{"Context_Management":{"type":"string"},"Type":{"const":"image_generation"},"Tools":{"default":[{"type":"image_generation"}]}}}}],"tool_choice":{"type":"function","name":"describe"},"input":"context_management and image_generation are just words"}`
	calls := 0
	selector := keyRouteSelectorFunc(func(_ context.Context, key *service.APIKey, _ service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
		calls++
		return key, keyrouting.Plan{SelectedGroupID: 1}, nil
	})
	router := keyRouteTestRouter(t, routeTestKey(), selector, "POST", "/v1/responses", func(c *gin.Context) {
		gotBody, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(gotBody))
		c.Status(http.StatusOK)
	})
	require.Equal(t, http.StatusOK, sendKeyRouteRequest(router, "POST", "/v1/responses", body).Code)
	require.Equal(t, 1, calls)
}

func TestKeyRoutingProviderToolsStayOnPrimary(t *testing.T) {
	for _, tc := range []struct{ name, fields string }{
		{"web_search", `"tools":[{"type":"web_search"}]`},
		{"web_search_preview", `"tools":[{"type":"web_search_preview"}]`},
		{"anthropic_web_search", `"tools":[{"type":"web_search_20250305","name":"web_search"}]`},
		{"code_interpreter", `"tools":[{"type":"code_interpreter","container":"auto"}]`},
		{"file_search", `"tools":[{"type":"file_search"}]`},
		{"computer", `"tools":[{"type":"computer_use_preview"}]`},
		{"mcp", `"tools":[{"type":"mcp","server_url":"https://example.invalid"}]`},
		{"shell", `"tools":[{"type":"shell"}]`},
		{"unknown_future_tool", `"tools":[{"type":"future_provider_tool"}]`},
		{"unknown_wrapper", `"tools":[{"googleSearch":{}}]`},
		{"mixed_tools", `"tools":[{"type":"function","name":"safe"},{"Type":"web_search"}]`},
		{"forced_builtin", `"tool_choice":{"Type":"web_search_preview"}`},
		{"forced_unknown", `"tool_choice":{"type":"future_provider_tool"}`},
		{"disabled_builtin", `"tools":[{"type":"code_interpreter"}],"tool_choice":"none"`},
		{"chat_search", `"Web_Search_Options":{}`},
		{"anthropic_mcp_connector", `"MCP_Servers":[{"type":"url","url":"https://example.invalid","name":"connector"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"model":"primary-only",` + tc.fields + `}`
			selector := keyRouteSelectorFunc(func(context.Context, *service.APIKey, service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
				t.Fatal("provider-executed or unknown tool reached backup routing")
				return nil, keyrouting.Plan{}, nil
			})
			for _, path := range []string{"/v1/responses", "/v1/chat/completions", "/v1/messages"} {
				router := keyRouteTestRouter(t, routeTestKey(), selector, "POST", path, func(c *gin.Context) {
					key, ok := GetAPIKeyFromContext(c)
					require.True(t, ok)
					require.EqualValues(t, 1, *key.GroupID)
					require.Nil(t, c.Request.Context().Value(ctxkey.KeyRoutingActive))
					_, routed := c.Get("custom.key_route")
					require.False(t, routed)
					gotBody, err := io.ReadAll(c.Request.Body)
					require.NoError(t, err)
					require.Equal(t, body, string(gotBody))
					c.Status(http.StatusOK)
				})
				require.Equal(t, http.StatusOK, sendKeyRouteRequest(router, "POST", path, body).Code)
			}
		})
	}
}

func TestKeyRoutingClientToolsRemainRoutable(t *testing.T) {
	for _, tc := range []struct{ name, fields string }{
		{"function", `"tools":[{"type":"function","name":"web_search","parameters":{"type":"object"}}],"tool_choice":{"type":"function","name":"web_search"}`},
		{"chat_function", `"tools":[{"type":"function","function":{"name":"code_interpreter","parameters":{"type":"object"}}}],"tool_choice":"required"`},
		{"custom", `"tools":[{"type":"custom","name":"shell","format":{"type":"text"}}],"tool_choice":{"type":"custom","name":"shell"}`},
		{"anthropic", `"tools":[{"name":"web_search","input_schema":{"type":"object","properties":{"type":{"const":"mcp"}}}}],"tool_choice":{"type":"tool","name":"web_search"}`},
		{"anthropic_any", `"tools":[{"name":"run","description":"code_interpreter","input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}],"tool_choice":{"type":"any"}`},
		{"anthropic_auto", `"tool_choice":{"type":"auto"}`},
		{"anthropic_none", `"tool_choice":{"type":"none"}`},
		{"auto", `"tools":[],"tool_choice":"auto"`},
		{"none", `"tool_choice":"none"`},
		{"null", `"tools":null,"tool_choice":null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"model":"primary-only",` + tc.fields + `}`
			calls := 0
			selector := keyRouteSelectorFunc(func(_ context.Context, key *service.APIKey, _ service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
				calls++
				return key, keyrouting.Plan{SelectedGroupID: 1}, nil
			})
			router := keyRouteTestRouter(t, routeTestKey(), selector, "POST", "/v1/responses", func(c *gin.Context) {
				require.Equal(t, true, c.Request.Context().Value(ctxkey.KeyRoutingActive))
				gotBody, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, body, string(gotBody))
				c.Status(http.StatusOK)
			})
			require.Equal(t, http.StatusOK, sendKeyRouteRequest(router, "POST", "/v1/responses", body).Code)
			require.Equal(t, 1, calls)
		})
	}
}

func TestKeyRoutingErrorsNeverForwardOrRetry(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{keyrouting.ErrNotAuthorized, 403}, {keyrouting.ErrModelUnavailable, 404}, {keyrouting.ErrNoSchedulableAccounts, 503}, {errors.New("database unavailable"), 503}} {
		calls := 0
		selector := keyRouteSelectorFunc(func(context.Context, *service.APIKey, service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
			calls++
			return nil, keyrouting.Plan{}, tc.err
		})
		router := keyRouteTestRouter(t, routeTestKey(), selector, "POST", "/v1/responses", func(*gin.Context) { t.Fatal("forwarded") })
		w := sendKeyRouteRequest(router, "POST", "/v1/responses", `{"model":"a"}`)
		require.Equal(t, tc.status, w.Code, w.Body.String())
		require.Equal(t, 1, calls)
	}
}
