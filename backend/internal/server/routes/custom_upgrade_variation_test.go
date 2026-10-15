package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/custom"
	"github.com/Wei-Shaw/sub2api/internal/customapp"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

var customUpgradeBuiltinRoutes = []string{
	"GET /api/v1/admin/roles/operator/permissions",
	"PUT /api/v1/admin/roles/operator/permissions",
	"POST /api/v1/admin/users/:id/api-keys",
	"GET /api/v1/admin/groups/:id/effective-models",
	"GET /api/v1/admin/ops/capabilities",
	"DELETE /api/v1/admin/ops/errors",
	"GET /api/v1/admin/usage/filter-options",
	"DELETE /api/v1/admin/api-keys/:id",
	"GET /api/v1/admin/prompt-audit/recording/stats",
	"POST /api/v1/admin/prompt-audit/recording/cleanup",
	"POST /api/v1/admin/prompt-audit/events/export",
	"GET /api/v1/admin/prompt-rules",
	"GET /api/v1/admin/prompt-rules/:id",
	"POST /api/v1/admin/prompt-rules",
	"PUT /api/v1/admin/prompt-rules/:id",
	"DELETE /api/v1/admin/prompt-rules/:id",
}

// The fixture changes only the upstream layout: production group registrars,
// the real built-in composition, and real middleware retain their behavior.
// Unlike a registry-only probe, it mounts the shipped handlers at all ten
// named insertion points and sends requests through the resulting Gin engine.
func customUpgradeVariationRouter(t *testing.T, variant string, omitAuditMount bool, rpm int) (*gin.Engine, []string) {
	t.Helper()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{
		Setting: &admin.SettingHandler{}, PromptAudit: securityaudit.NewPromptAdminHandler(nil),
		PromptRule: &admin.PromptRuleHandler{}, APIKey: &admin.AdminAPIKeyHandler{},
		User: &admin.UserHandler{}, Ops: &admin.OpsHandler{}, Group: &admin.GroupHandler{}, Usage: &admin.UsageHandler{},
	}}
	probe := contractRouteProvider{"upgrade-probe", customContractPath, customContractPath}
	additional, err := custom.NewCustomProviderSet(contractHooks{
		routes: []custom.RouteProvider{probe}, permissions: []custom.PermissionProvider{probe},
	})
	require.NoError(t, err)
	host, err := customapp.NewHost(h, &service.PromptRuleService{}, additional)
	require.NoError(t, err)

	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	settings := service.NewSettingService(&channelMonitorRouteSettingRepoStub{values: map[string]string{
		service.SettingKeyPanelRateLimitSettings: fmt.Sprintf(`{"enabled":true,"user_rpm":%d,"heavy_rpm":1000,"exempt_admin":false,"public_ip_rpm":0}`, rpm),
	}}, &config.Config{})
	limiter := middleware.NewPanelRateLimiter(rdb, settings)

	// Capture actual Gin registration order, not the fixture's intended order.
	previousMode, previousPrint := gin.Mode(), gin.DebugPrintRouteFunc
	defer func() { gin.SetMode(previousMode); gin.DebugPrintRouteFunc = previousPrint }()
	gin.SetMode(gin.DebugMode)
	var order []string
	gin.DebugPrintRouteFunc = func(method, path, _ string, _ int) { order = append(order, method+" "+path) }
	router := gin.New()
	management := router.Group("/api/v1/admin")
	management.Use(func(c *gin.Context) {
		c.Header("X-Upgrade-Chain", strings.Join(c.HandlerNames(), "|"))
		role := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if role != service.RoleAdmin && role != service.RoleOperator && role != service.RoleUser {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(middleware.ContextKeyUserRole), role)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		c.Set("custom.contract.auth", role)
		c.Header("X-Upgrade-Auth", role)
		c.Next()
	})
	management.Use(limiter.Global())
	management.Use(func(c *gin.Context) {
		c.Set("custom.contract.audit", "after-auth:"+c.GetString("custom.contract.auth"))
		c.Header("X-Upgrade-Audit", c.GetString("custom.contract.audit"))
		c.Next()
	})
	// Keep the authoritative existing default operator policy, independent of
	// provider metadata. No new permissions are granted by the fixture.
	management.Use(middleware.AdminRouteAuthorization(nil))
	management.Use(middleware.OperatorFinancialResponseFilter(nil))
	management.Use(middleware.AdminComplianceGuard(nil))

	officialAdditions := func() {
		future := management.Group("/official-upgrade")
		future.GET("/status", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		future.PUT("/preferences", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		management.Group("/ops").GET("/official-new-status", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		management.Group("/prompt-audit").GET("/official-new-stat", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	}
	registrars := []func(){
		func() { host.RegisterAdminRoutesAt(management, custom.AdminRoles) },
		func() { registerUserManagementRoutes(management, h, host) },
		func() { registerGroupRoutes(management, h, host) },
		func() { registerOpsRoutes(management, h, host) },
		func() { registerUsageRoutes(management, h, host) },
		func() { registerAdminAPIKeyRoutes(management, h, host) },
		func() {
			auditHost := host
			if omitAuditMount {
				auditHost = custom.NewHost(nil)
			}
			registerPromptAuditRoutes(management, h, auditHost)
		},
		func() { host.RegisterAdminRoutesAt(management, custom.AdminPromptRules) },
		func() { host.RegisterAdminRoutes(management) },
	}
	if variant == "additions_before" {
		officialAdditions()
	}
	if variant == "reordered_with_additions" {
		slices.Reverse(registrars)
	}
	for _, register := range registrars {
		register()
	}
	if variant == "reordered_with_additions" {
		officialAdditions()
	}
	return router, order
}

func customUpgradeMissingMount(router *gin.Engine, order []string) error {
	routes := customContractRouteSet(router)
	for _, expected := range customUpgradeBuiltinRoutes {
		count := 0
		for _, registered := range order {
			if registered == expected {
				count++
			}
		}
		if !routes[expected] || count != 1 {
			return fmt.Errorf("custom upgrade mount missing or duplicated: %s (count=%d)", expected, count)
		}
	}
	return nil
}

func customUpgradeRequest(router *gin.Engine, method, path, role string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, strings.ReplaceAll(path, ":id", "1"), nil)
	if role != "" {
		req.Header.Set("Authorization", "Bearer "+role)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCustomUpgradeRouteBoundaryVariants(t *testing.T) {
	for _, variant := range []string{"baseline", "additions_before", "reordered_with_additions"} {
		t.Run(variant, func(t *testing.T) {
			router, order := customUpgradeVariationRouter(t, variant, false, 1000)
			require.NoError(t, customUpgradeMissingMount(router, order))
			roles := slices.Index(order, "GET /api/v1/admin/roles/operator/permissions")
			audit := slices.Index(order, "GET /api/v1/admin/prompt-audit/config")
			if variant == "reordered_with_additions" {
				require.Less(t, audit, roles, "the actual production group registration must be reordered")
			} else {
				require.Less(t, roles, audit)
			}
			if variant != "baseline" {
				addition := slices.Index(order, "GET /api/v1/admin/official-upgrade/status")
				if variant == "additions_before" {
					require.Less(t, addition, roles)
				} else {
					require.Greater(t, addition, roles)
				}
				require.Equal(t, http.StatusNoContent, customUpgradeRequest(router, http.MethodGet, "/api/v1/admin/official-upgrade/status", "admin").Code)
				require.Equal(t, http.StatusForbidden, customUpgradeRequest(router, http.MethodGet, "/api/v1/admin/official-upgrade/status", "operator").Code)
			}
			for _, route := range customUpgradeBuiltinRoutes {
				parts := strings.SplitN(route, " ", 2)
				for _, role := range []string{"", "user"} {
					rec := customUpgradeRequest(router, parts[0], parts[1], role)
					want := http.StatusUnauthorized
					if role != "" {
						want = http.StatusForbidden
					}
					require.Equal(t, want, rec.Code, "%s role=%q", route, role)
					chain := strings.Split(rec.Header().Get("X-Upgrade-Chain"), "|")
					count := 7
					if route == "POST /api/v1/admin/users/:id/api-keys" || route == "DELETE /api/v1/admin/api-keys/:id" {
						count++
						require.Contains(t, chain[6], "OperatorTargetUserWriteGuard")
					}
					require.Len(t, chain, count, route)
					require.Contains(t, chain[1], "PanelRateLimiter")
					require.Contains(t, chain[3], "AdminRouteAuthorization")
					require.Contains(t, chain[4], "OperatorFinancialResponseFilter")
					require.Contains(t, chain[5], "AdminComplianceGuard")
					if role == "" {
						require.Empty(t, rec.Header().Get("X-Upgrade-Audit"), "unauthenticated request cannot reach audit")
					} else {
						require.Equal(t, "after-auth:user", rec.Header().Get("X-Upgrade-Audit"), "audit precedes default-deny authorization")
					}
				}
			}
			// A real built-in handler is reached after the reordered host chain.
			capability := customUpgradeRequest(router, http.MethodGet, "/api/v1/admin/ops/capabilities", "admin")
			require.Equal(t, http.StatusServiceUnavailable, capability.Code, "the actual nil-service handler gives its explicit response")
			require.Equal(t, "after-auth:admin", capability.Header().Get("X-Upgrade-Audit"))
			unknown := customUpgradeRequest(router, http.MethodGet, "/api/v1/admin"+customContractPath, "operator")
			require.Equal(t, http.StatusForbidden, unknown.Code, "registered metadata cannot grant an unknown route")
			require.Equal(t, "after-auth:operator", unknown.Header().Get("X-Upgrade-Audit"))
			require.Empty(t, unknown.Header().Get("X-Custom-Auth"), "unknown handler never executes")
			require.Equal(t, http.StatusNoContent, customUpgradeRequest(router, http.MethodGet, "/api/v1/admin"+customContractPath, "admin").Code)
		})
	}
}

func TestCustomUpgradeRouteBoundaryOmittedMountIsDetected(t *testing.T) {
	router, order := customUpgradeVariationRouter(t, "reordered_with_additions", true, 1000)
	require.ErrorContains(t, customUpgradeMissingMount(router, order), "GET /api/v1/admin/prompt-audit/recording/stats")
	routes := customContractRouteSet(router)
	require.True(t, routes["GET /api/v1/admin/prompt-audit/config"], "official audit registrar survives the missing extension")
	require.True(t, routes["GET /api/v1/admin/prompt-audit/official-new-stat"])
	require.True(t, routes["GET /api/v1/admin/ops/capabilities"], "unrelated custom group remains connected")
	require.Equal(t, http.StatusNotFound, customUpgradeRequest(router, http.MethodGet, "/api/v1/admin/prompt-audit/recording/stats", "admin").Code)
}

func TestCustomUpgradeRouteBoundaryRateLimitPrecedesAudit(t *testing.T) {
	router, _ := customUpgradeVariationRouter(t, "reordered_with_additions", false, 1)
	const path = "/api/v1/admin/ops/capabilities"
	require.Equal(t, http.StatusUnauthorized, customUpgradeRequest(router, http.MethodGet, path, "").Code)
	first := customUpgradeRequest(router, http.MethodGet, path, "admin")
	require.Equal(t, http.StatusServiceUnavailable, first.Code, "anonymous request did not consume the authenticated rate budget")
	require.Equal(t, "after-auth:admin", first.Header().Get("X-Upgrade-Audit"))
	limited := customUpgradeRequest(router, http.MethodGet, path, "admin")
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.Equal(t, "admin", limited.Header().Get("X-Upgrade-Auth"))
	require.NotEmpty(t, limited.Header().Get("Retry-After"))
	require.Empty(t, limited.Header().Get("X-Upgrade-Audit"), "the real rate limiter rejects before the audit/handler stage")
}
