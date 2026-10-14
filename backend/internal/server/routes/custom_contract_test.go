package routes

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/custom"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const customContractPath = "/custom-contract"

type contractRouteProvider struct {
	id          string
	adminPath   string
	gatewayPath string
}

func (p contractRouteProvider) ID() string { return p.id }
func (p contractRouteProvider) Permissions() []custom.Permission {
	// Metadata must not grant an operator access to an otherwise unknown route.
	return []custom.Permission{{Name: "custom.contract.read"}}
}
func (p contractRouteProvider) RegisterAdminRoutes(router gin.IRouter) {
	router.GET(p.adminPath, customContractHandler)
}
func (p contractRouteProvider) RegisterGatewayRoutes(router gin.IRouter) {
	router.POST(p.gatewayPath, customContractHandler)
}

func customContractHandler(c *gin.Context) {
	if _, err := io.ReadAll(c.Request.Body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
		} else {
			c.AbortWithStatus(http.StatusBadRequest)
		}
		return
	}
	c.Header("X-Custom-Auth", c.GetString("custom.contract.auth"))
	c.Header("X-Custom-Audit", c.GetString("custom.contract.audit"))
	c.Status(http.StatusNoContent)
}

type contractHooks struct {
	routes      []custom.RouteProvider
	permissions []custom.PermissionProvider
}

func (h contractHooks) RouteProviders() []custom.RouteProvider           { return h.routes }
func (h contractHooks) PermissionProviders() []custom.PermissionProvider { return h.permissions }
func (contractHooks) SettingProviders() []custom.SettingProvider         { return nil }
func (contractHooks) WorkerProviders() []custom.WorkerProvider           { return nil }
func (contractHooks) UsageEnrichers() []custom.UsageEnricher             { return nil }

func newCustomContractHost(t *testing.T) *custom.Host {
	t.Helper()
	p := contractRouteProvider{"contract", customContractPath, customContractPath}
	set, err := custom.NewCustomProviderSet(contractHooks{
		routes: []custom.RouteProvider{p}, permissions: []custom.PermissionProvider{p},
	})
	require.NoError(t, err)
	return custom.NewHost(set)
}

// Exercise the production route registrars, not Host.Register* on a synthetic
// router group. Only identity lookup is stubbed; the host's authorization,
// audit placement, group checks, body limit and route selection remain real.
func newCustomContractRouter(hosts ...*custom.Host) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &handler.Handlers{
		Admin: &handler.AdminHandlers{}, Gateway: &handler.GatewayHandler{},
		OpenAIGateway: &handler.OpenAIGatewayHandler{},
		AsyncImage:    handler.NewAsyncImageHandler(nil, nil),
		BatchImage:    handler.NewBatchImageHandler(nil, nil, nil),
	}
	adminAuth := servermiddleware.AdminAuthMiddleware(func(c *gin.Context) {
		role := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if role != service.RoleAdmin && role != service.RoleOperator && role != service.RoleUser {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(servermiddleware.ContextKeyUserRole), role)
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 1})
		c.Set("custom.contract.auth", "admin")
		c.Next()
	})
	auditLog := servermiddleware.AuditLogMiddleware(func(c *gin.Context) {
		c.Set("custom.contract.audit", "after-auth:"+c.GetString("custom.contract.auth"))
		c.Next()
	})
	stepUp := servermiddleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	RegisterAdminRoutes(router.Group("/api/v1"), h, adminAuth, auditLog, stepUp, nil, nil, hosts...)
	apiKeyAuth := servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer key" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		groupID := int64(1)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			ID: 1, GroupID: &groupID,
			Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI},
		})
		c.Set("custom.contract.auth", "gateway")
		c.Next()
	})
	RegisterGatewayRoutes(router, h, apiKeyAuth, nil, nil, nil, nil, nil,
		&config.Config{Gateway: config.GatewayConfig{MaxBodySize: 128, TextMaxBodySize: 128}}, hosts...)
	return router
}

func customContractRouteSet(router *gin.Engine) map[string]bool {
	routes := make(map[string]bool)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	return routes
}

func customContractMissingRoutes(router *gin.Engine) error {
	routes := customContractRouteSet(router)
	for _, route := range []string{"GET /api/v1/admin" + customContractPath, "POST /v1" + customContractPath} {
		if !routes[route] {
			return fmt.Errorf("custom host route missing: %s", route)
		}
	}
	return nil
}

func TestCustomHostRoutesUseRealAdminAndGatewayMiddleware(t *testing.T) {
	router := newCustomContractRouter(newCustomContractHost(t))
	require.NoError(t, customContractMissingRoutes(router))
	for _, tc := range []struct {
		name, method, path, token, auth, audit string
		status                                 int
	}{
		{name: "admin_missing_identity", method: http.MethodGet, path: "/api/v1/admin" + customContractPath, status: http.StatusUnauthorized},
		{name: "ordinary_user_rejected", method: http.MethodGet, path: "/api/v1/admin" + customContractPath, token: "user", status: http.StatusForbidden},
		{name: "operator_unknown_route_denied_despite_metadata", method: http.MethodGet, path: "/api/v1/admin" + customContractPath, token: "operator", status: http.StatusForbidden},
		{name: "admin_authorized", method: http.MethodGet, path: "/api/v1/admin" + customContractPath, token: "admin", status: http.StatusNoContent, auth: "admin", audit: "after-auth:admin"},
		{name: "gateway_missing_key", method: http.MethodPost, path: "/v1" + customContractPath, status: http.StatusUnauthorized},
		{name: "gateway_invalid_key", method: http.MethodPost, path: "/v1" + customContractPath, token: "invalid", status: http.StatusUnauthorized},
		{name: "gateway_authorized", method: http.MethodPost, path: "/v1" + customContractPath, token: "key", status: http.StatusNoContent, auth: "gateway"},
		{name: "no_unprotected_root_alias", method: http.MethodPost, path: customContractPath, token: "key", status: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code)
			require.Equal(t, tc.auth, rec.Header().Get("X-Custom-Auth"))
			require.Equal(t, tc.audit, rec.Header().Get("X-Custom-Audit"))
		})
	}
	request := httptest.NewRequest(http.MethodPost, "/v1"+customContractPath, strings.NewReader(strings.Repeat("x", 129)))
	request.Header.Set("Authorization", "Bearer key")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Empty(t, recorder.Header().Get("X-Custom-Auth"), "host body limit must constrain extension handler reads")
}

func TestCustomHostEmptySetPreservesOfficialRoutes(t *testing.T) {
	empty := newCustomContractRouter(custom.NewHost(nil))
	withFeature := newCustomContractRouter(newCustomContractHost(t))
	actual := customContractRouteSet(withFeature)
	delete(actual, "GET /api/v1/admin"+customContractPath)
	delete(actual, "POST /v1"+customContractPath)
	require.Equal(t, customContractRouteSet(empty), actual, "optional providers must only add their own routes")
	// These routes are official v0.2.16 capabilities, not optional custom routes.
	for _, route := range []string{
		"POST /v1/responses", "POST /v1/chat/completions", "POST /v1/images/generations",
		"POST /v1/images/batches", "GET /v1/images/batches", "GET /v1/images/batches/models",
		"GET /v1/images/batches/:id", "GET /v1/images/batches/:id/items",
		"GET /v1/images/batches/:id/items/:custom_id/content", "GET /v1/images/batches/:id/download",
		"POST /v1/images/batches/:id/cancel", "DELETE /v1/images/batches/:id", "DELETE /v1/images/batches/:id/outputs",
		"GET /api/v1/admin/prompt-audit/config", "PUT /api/v1/admin/prompt-audit/config",
		"GET /api/v1/admin/prompt-audit/runtime", "GET /api/v1/admin/prompt-audit/events",
	} {
		require.True(t, actual[route], "empty custom host lost official route %s", route)
	}
	for _, target := range []struct{ method, path string }{
		{http.MethodPost, "/v1/images/batches"},
		{http.MethodGet, "/api/v1/admin/prompt-audit/config"},
	} {
		recorder := httptest.NewRecorder()
		empty.ServeHTTP(recorder, httptest.NewRequest(target.method, target.path, nil))
		require.Equal(t, http.StatusUnauthorized, recorder.Code, "official route must remain behind its host auth chain: %s", target.path)
	}
}

func TestCustomHostOmittedRegistrationIsDetected(t *testing.T) {
	// Negative fixture deliberately omits the provider but uses the exact same
	// production route registrars and contract as the positive integration test.
	require.ErrorContains(t, customContractMissingRoutes(newCustomContractRouter(custom.NewHost(nil))), "custom host route missing")
	require.NoError(t, customContractMissingRoutes(newCustomContractRouter(newCustomContractHost(t))))
}

func TestCustomHostDefaultProviderSetReachesRealRouteRegistrars(t *testing.T) {
	previous := custom.DefaultHost().Providers()
	t.Cleanup(func() { custom.ConfigureProviderSet(previous) })
	custom.ConfigureProviderSet(newCustomContractHost(t).Providers())
	require.NoError(t, customContractMissingRoutes(newCustomContractRouter()))
}

func TestCustomHostDuplicateRoutesFailInRealRegistrars(t *testing.T) {
	for _, tc := range []struct {
		name      string
		providers []custom.RouteProvider
	}{
		{"providers_collide", []custom.RouteProvider{
			contractRouteProvider{"a", customContractPath, customContractPath},
			contractRouteProvider{"b", customContractPath, customContractPath},
		}},
		{"gateway_providers_collide", []custom.RouteProvider{
			contractRouteProvider{"a", "/custom-contract-a", customContractPath},
			contractRouteProvider{"b", "/custom-contract-b", customContractPath},
		}},
		{"official_gateway_route_collision", []custom.RouteProvider{
			contractRouteProvider{"a", customContractPath, "/images/batches"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set, err := custom.NewCustomProviderSet(contractHooks{routes: tc.providers})
			require.NoError(t, err)
			require.Panics(t, func() { newCustomContractRouter(custom.NewHost(set)) }, "duplicate routes must not silently replace a handler")
		})
	}
}
