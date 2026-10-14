package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/custom"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type bootstrapHooks struct{ permissions []custom.PermissionProvider }

func (h bootstrapHooks) PermissionProviders() []custom.PermissionProvider { return h.permissions }
func (bootstrapHooks) RouteProviders() []custom.RouteProvider             { return nil }
func (bootstrapHooks) SettingProviders() []custom.SettingProvider         { return nil }
func (bootstrapHooks) WorkerProviders() []custom.WorkerProvider           { return nil }
func (bootstrapHooks) UsageEnrichers() []custom.UsageEnricher             { return nil }

type bootstrapPermissionProvider struct{ name string }

func (bootstrapPermissionProvider) ID() string { return "fixture" }
func (p bootstrapPermissionProvider) Permissions() []custom.Permission {
	return []custom.Permission{{Name: p.name}}
}

func bootstrapHandlers() *handler.Handlers {
	return &handler.Handlers{
		Admin: &handler.AdminHandlers{
			Setting: &admin.SettingHandler{}, PromptAudit: securityaudit.NewPromptAdminHandler(nil),
			PromptRule: &admin.PromptRuleHandler{}, APIKey: &admin.AdminAPIKeyHandler{},
			User: &admin.UserHandler{}, Ops: &admin.OpsHandler{}, Group: &admin.GroupHandler{}, Usage: &admin.UsageHandler{},
		},
		Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{},
		AsyncImage: handler.NewAsyncImageHandler(nil, nil), BatchImage: handler.NewBatchImageHandler(nil, nil, nil),
	}
}

func isolatedBootstrapDefaults(t *testing.T) {
	t.Helper()
	previous := custom.DefaultHost().Providers()
	custom.ConfigureProviderSet(nil)
	t.Cleanup(func() { custom.ConfigureProviderSet(previous) })
}

func TestProvideCustomHostComposesWithoutPublishingOrStarting(t *testing.T) {
	isolatedBootstrapDefaults(t)
	set, err := custom.NewCustomProviderSet(bootstrapHooks{permissions: []custom.PermissionProvider{bootstrapPermissionProvider{"fixture.read"}}})
	require.NoError(t, err)
	custom.ConfigureProviderSet(set)
	for i := 0; i < 2; i++ {
		host, err := ProvideCustomHost(bootstrapHandlers(), &service.PromptRuleService{})
		require.NoError(t, err)
		require.Len(t, host.Providers().Routes(), 1)
		require.Len(t, host.Providers().Workers(), 1, "native audit/batch workers must not be duplicated")
		require.Equal(t, "prompt-rule-subscription", host.Providers().Workers()[0].ID())
		require.Len(t, host.Providers().Permissions(), 2)
		require.Equal(t, authz.NewOperatorPolicyProvider().ID(), host.Providers().Permissions()[1].ID())
		require.Same(t, set, custom.DefaultHost().Providers(), "application assembly must not mutate global optional providers")
	}
}

func TestProvideCustomHostRejectsMissingDependenciesAndPermissionCollision(t *testing.T) {
	isolatedBootstrapDefaults(t)
	_, err := ProvideCustomHost(nil, &service.PromptRuleService{})
	require.Error(t, err)
	_, err = ProvideCustomHost(bootstrapHandlers(), nil)
	require.Error(t, err)
	h := bootstrapHandlers()
	h.Admin.PromptRule = nil
	_, err = ProvideCustomHost(h, &service.PromptRuleService{})
	require.Error(t, err, "missing required custom routes cannot become a silent empty provider")
	name := authz.NewOperatorPolicyProvider().Permissions()[0].Name
	set, err := custom.NewCustomProviderSet(bootstrapHooks{permissions: []custom.PermissionProvider{bootstrapPermissionProvider{name}}})
	require.NoError(t, err)
	custom.ConfigureProviderSet(set)
	_, err = ProvideCustomHost(bootstrapHandlers(), &service.PromptRuleService{})
	require.ErrorIs(t, err, custom.ErrDuplicatePermission)
	require.Same(t, set, custom.DefaultHost().Providers())
}

// This invokes the real application registerRoutes function. Only identity
// lookup is replaced; production authorization, audit placement, all native
// registrars, custom composition and target-user guards remain connected.
func bootstrapRouter(h *handler.Handlers, host *custom.Host) *gin.Engine {
	r := gin.New()
	adminAuth := middleware.AdminAuthMiddleware(func(c *gin.Context) {
		c.Header("X-Bootstrap-Chain", strings.Join(c.HandlerNames(), "|"))
		role := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if role != service.RoleAdmin && role != service.RoleUser && role != service.RoleOperator {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(middleware.ContextKeyUserRole), role)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		c.Header("X-Bootstrap-Auth", role)
		c.Next()
	})
	auditLog := middleware.AuditLogMiddleware(func(c *gin.Context) {
		c.Header("X-Bootstrap-Audit", c.Writer.Header().Get("X-Bootstrap-Auth"))
		c.Next()
	})
	pass := func(c *gin.Context) { c.Next() }
	registerRoutes(r, h, middleware.JWTAuthMiddleware(pass), middleware.OptionalJWTAuthMiddleware(pass), adminAuth,
		middleware.APIKeyAuthMiddleware(func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) }), auditLog,
		middleware.StepUpAuthMiddleware(pass), nil, nil, nil, nil, nil, &config.Config{}, nil, host)
	return r
}

var builtinAdminRouteContracts = map[string]string{
	"GET /api/v1/admin/roles/operator/permissions":      "GetOperatorRolePolicy",
	"PUT /api/v1/admin/roles/operator/permissions":      "UpdateOperatorRolePolicy",
	"POST /api/v1/admin/users/:id/api-keys":             "CreateForUser",
	"GET /api/v1/admin/groups/:id/effective-models":     "GetEffectiveModels",
	"GET /api/v1/admin/ops/capabilities":                "GetCapabilities",
	"DELETE /api/v1/admin/ops/errors":                   "DeleteErrorLogs",
	"GET /api/v1/admin/usage/filter-options":            "GetFilterOptions",
	"DELETE /api/v1/admin/api-keys/:id":                 "Delete",
	"GET /api/v1/admin/prompt-audit/recording/stats":    "GetRecordingStats",
	"POST /api/v1/admin/prompt-audit/recording/cleanup": "CleanupRecording",
	"POST /api/v1/admin/prompt-audit/events/export":     "ExportEvents",
	"GET /api/v1/admin/prompt-rules":                    "List",
	"GET /api/v1/admin/prompt-rules/:id":                "GetByID",
	"POST /api/v1/admin/prompt-rules":                   "Create",
	"PUT /api/v1/admin/prompt-rules/:id":                "Update",
	"DELETE /api/v1/admin/prompt-rules/:id":             "Delete",
}

func bootstrapRouteSet(r *gin.Engine) map[string]string {
	result := map[string]string{}
	for _, route := range r.Routes() {
		result[route.Method+" "+route.Path] = route.Handler
	}
	return result
}

func TestApplicationCustomRoutesPreserveOrderMiddlewareAndNativeSurface(t *testing.T) {
	isolatedBootstrapDefaults(t)
	oldMode, oldPrint := gin.Mode(), gin.DebugPrintRouteFunc
	t.Cleanup(func() { gin.SetMode(oldMode); gin.DebugPrintRouteFunc = oldPrint })
	gin.SetMode(gin.DebugMode)
	var order []string
	counts := map[string]int{}
	gin.DebugPrintRouteFunc = func(method, path, handlerName string, n int) {
		key := method + " " + path
		order = append(order, key)
		counts[key] = n
	}
	h := bootstrapHandlers()
	host, err := ProvideCustomHost(h, &service.PromptRuleService{})
	require.NoError(t, err)
	r := bootstrapRouter(h, host)
	routes := bootstrapRouteSet(r)
	for route, method := range builtinAdminRouteContracts {
		require.Contains(t, routes, route, "missing real application custom mount")
		require.Contains(t, routes[route], "."+method+"-fm", "route must retain its actual handler")
		expectedCount := 7 // auth, global limit, audit, authorization, financial filter, compliance, handler
		if route == "POST /api/v1/admin/users/:id/api-keys" || route == "DELETE /api/v1/admin/api-keys/:id" {
			expectedCount++
		}
		require.Equal(t, expectedCount, counts[route], "route middleware changed: %s", route)
		parts := strings.SplitN(route, " ", 2)
		for _, role := range []string{"", service.RoleUser} {
			req := httptest.NewRequest(parts[0], strings.ReplaceAll(parts[1], ":id", "1"), nil)
			if role != "" {
				req.Header.Set("Authorization", "Bearer "+role)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			want := http.StatusUnauthorized
			if role != "" {
				want = http.StatusForbidden
			}
			require.Equal(t, want, rec.Code, "%s role=%s", route, role)
			chain := strings.Split(rec.Header().Get("X-Bootstrap-Chain"), "|")
			require.Len(t, chain, expectedCount)
			require.Contains(t, chain[3], "AdminRouteAuthorization")
			require.Contains(t, chain[4], "OperatorFinancialResponseFilter")
			require.Contains(t, chain[5], "AdminComplianceGuard")
			if expectedCount == 8 {
				require.Contains(t, chain[6], "OperatorTargetUserWriteGuard", "retain the original per-resource write guard")
			}
		}
	}
	joined := strings.Join(order, "\n")
	for _, adjacent := range [][]string{
		{"GET /api/v1/admin/roles/operator/permissions", "PUT /api/v1/admin/roles/operator/permissions", "GET /api/v1/admin/compliance"},
		{"POST /api/v1/admin/users/:id/auth-identities", "POST /api/v1/admin/users/:id/api-keys", "POST /api/v1/admin/users"},
		{"GET /api/v1/admin/groups/:id/model-allowlist-candidates", "GET /api/v1/admin/groups/:id/effective-models", "GET /api/v1/admin/groups/:id/composite-routes"},
		{"GET /api/v1/admin/ops/capabilities", "GET /api/v1/admin/ops/concurrency"},
		{"GET /api/v1/admin/ops/errors", "DELETE /api/v1/admin/ops/errors", "GET /api/v1/admin/ops/errors/:id"},
		{"GET /api/v1/admin/usage/search-api-keys", "GET /api/v1/admin/usage/filter-options", "GET /api/v1/admin/usage/cleanup-tasks"},
		{"PUT /api/v1/admin/api-keys/:id", "DELETE /api/v1/admin/api-keys/:id"},
		{"GET /api/v1/admin/prompt-audit/runtime", "GET /api/v1/admin/prompt-audit/recording/stats", "POST /api/v1/admin/prompt-audit/recording/cleanup", "GET /api/v1/admin/prompt-audit/events", "POST /api/v1/admin/prompt-audit/events/export", "GET /api/v1/admin/prompt-audit/events/:id"},
		{"POST /api/v1/admin/prompt-audit/events/delete-by-filter", "GET /api/v1/admin/prompt-rules", "GET /api/v1/admin/prompt-rules/:id", "POST /api/v1/admin/prompt-rules", "PUT /api/v1/admin/prompt-rules/:id", "DELETE /api/v1/admin/prompt-rules/:id"},
	} {
		require.Contains(t, joined, strings.Join(adjacent, "\n"), "registration insertion order changed")
	}
	// Exercise an actual built-in handler after the real authorization chain.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ops/capabilities", nil)
	req.Header.Set("Authorization", "Bearer admin")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, "nil Ops service is the actual handler's explicit response")
	require.Equal(t, "admin", rec.Header().Get("X-Bootstrap-Audit"))

	empty := bootstrapRouteSet(bootstrapRouter(h, custom.NewHost(nil)))
	for route := range builtinAdminRouteContracts {
		require.NotContains(t, empty, route, "omitting the composition must be observable")
		delete(routes, route)
	}
	require.Equal(t, empty, routes, "empty custom must remove only the 16 fork routes")
	for _, route := range []string{
		"GET /api/v1/admin/prompt-audit/config", "GET /api/v1/admin/prompt-audit/events",
		"POST /v1/images/batches", "GET /v1/images/batches", "GET /v1/images/batches/models",
		"GET /v1/images/batches/:id", "GET /v1/images/batches/:id/items", "GET /v1/images/batches/:id/items/:custom_id/content",
		"GET /v1/images/batches/:id/download", "POST /v1/images/batches/:id/cancel", "DELETE /v1/images/batches/:id", "DELETE /v1/images/batches/:id/outputs",
		"GET /api/v1/admin/accounts/kiro/default-model-mapping", "POST /api/v1/admin/kiro/oauth/auth-url",
	} {
		require.Contains(t, empty, route, "native/supplier route must remain outside custom")
	}
}

type bootstrapRuleRepo struct{ service.PromptRuleRepository }

func (bootstrapRuleRepo) List(context.Context) ([]*model.PromptRule, error) {
	return []*model.PromptRule{}, nil
}

type bootstrapRuleCache struct {
	service.PromptRuleCache
	starts atomic.Int32
	stops  atomic.Int32
}

func (*bootstrapRuleCache) Set(context.Context, []*model.PromptRule) error { return nil }
func (c *bootstrapRuleCache) SubscribeUpdates(ctx context.Context, _ func()) {
	c.starts.Add(1)
	<-ctx.Done()
	c.stops.Add(1)
}

func TestApplicationCustomHostStartsAndWaitsForActualPromptRuleSubscription(t *testing.T) {
	isolatedBootstrapDefaults(t)
	cache := &bootstrapRuleCache{}
	rules := service.NewPromptRuleService(bootstrapRuleRepo{}, cache)
	require.Zero(t, cache.starts.Load(), "constructor must retain only synchronous initialization")
	host, err := ProvideCustomHost(bootstrapHandlers(), rules)
	require.NoError(t, err)
	bootstrapRouter(bootstrapHandlers(), host)
	require.Zero(t, cache.starts.Load(), "route registration must not start a worker")
	require.NoError(t, host.StartWorkers(context.Background()))
	require.NoError(t, host.StartWorkers(context.Background()))
	require.Eventually(t, func() bool { return cache.starts.Load() == 1 }, time.Second, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, host.ShutdownWorkers(ctx))
	require.NoError(t, host.ShutdownWorkers(ctx))
	require.EqualValues(t, 1, cache.starts.Load())
	require.EqualValues(t, 1, cache.stops.Load(), "shutdown must wait for the real subscription runner")
}
