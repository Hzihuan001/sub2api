// Package customapp is the distribution's composition root. Only this adapter
// knows concrete host handlers; route contributions retain method functions,
// not a service locator or the application's entire handler graph.
package customapp

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/custom"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// NewHost assembles the built-in fork additions and any explicitly configured
// optional providers. Native audit/batch routes and workers are never included.
// Construction validates dependencies but does not start background work.
func NewHost(h *handler.Handlers, promptRules *service.PromptRuleService, additional *custom.CustomProviderSet) (*custom.Host, error) {
	if h == nil || h.Admin == nil || h.Admin.Setting == nil || h.Admin.PromptAudit == nil ||
		h.Admin.PromptRule == nil || h.Admin.APIKey == nil || h.Admin.User == nil ||
		h.Admin.Ops == nil || h.Admin.Group == nil || h.Admin.Usage == nil {
		return nil, fmt.Errorf("custom application admin handlers are incomplete")
	}
	if promptRules == nil {
		return nil, fmt.Errorf("custom application prompt rule subscription is missing")
	}
	hooks := contributions{}
	if additional != nil {
		hooks.routes = additional.Routes()
		hooks.permissions = additional.Permissions()
		hooks.settings = additional.Settings()
		hooks.workers = additional.Workers()
		hooks.enrichers = additional.UsageEnrichers()
	}
	permissions := authz.NewOperatorPolicyProvider()
	hasOperator := false
	for _, provider := range hooks.permissions {
		if provider.ID() == permissions.ID() {
			hasOperator = true
		}
	}
	if !hasOperator {
		hooks.permissions = append(hooks.permissions, permissions)
	}
	a := h.Admin
	hooks.routes = append(hooks.routes, adminRoutes{routes: []adminRoute{
		{custom.AdminRoles, http.MethodGet, "/roles/operator/permissions", []gin.HandlerFunc{a.Setting.GetOperatorRolePolicy}},
		{custom.AdminRoles, http.MethodPut, "/roles/operator/permissions", []gin.HandlerFunc{a.Setting.UpdateOperatorRolePolicy}},
		{custom.AdminUserAPIKeys, http.MethodPost, "/:id/api-keys", []gin.HandlerFunc{a.User.OperatorTargetUserWriteGuard(), a.APIKey.CreateForUser}},
		{custom.AdminGroupEffectiveModels, http.MethodGet, "/:id/effective-models", []gin.HandlerFunc{a.Group.GetEffectiveModels}},
		{custom.AdminGroupEffectiveModels, http.MethodGet, "/:id/failure-policy", []gin.HandlerFunc{a.Setting.GetGroupFailurePolicy}},
		{custom.AdminGroupEffectiveModels, http.MethodPut, "/:id/failure-policy", []gin.HandlerFunc{a.Setting.UpdateGroupFailurePolicy}},
		{custom.AdminOpsCapabilities, http.MethodGet, "/capabilities", []gin.HandlerFunc{a.Ops.GetCapabilities}},
		{custom.AdminOpsErrors, http.MethodDelete, "/errors", []gin.HandlerFunc{a.Ops.DeleteErrorLogs}},
		{custom.AdminUsageFilters, http.MethodGet, "/filter-options", []gin.HandlerFunc{a.Usage.GetFilterOptions}},
		{custom.AdminAPIKeys, http.MethodDelete, "/:id", []gin.HandlerFunc{a.APIKey.OperatorTargetUserWriteGuard(), a.APIKey.Delete}},
		{custom.AdminPromptAuditRecording, http.MethodGet, "/recording/stats", []gin.HandlerFunc{a.PromptAudit.GetRecordingStats}},
		{custom.AdminPromptAuditRecording, http.MethodPost, "/recording/cleanup", []gin.HandlerFunc{a.PromptAudit.CleanupRecording}},
		{custom.AdminPromptAuditExport, http.MethodPost, "/events/export", []gin.HandlerFunc{a.PromptAudit.ExportEvents}},
		{custom.AdminPromptRules, http.MethodGet, "/prompt-rules", []gin.HandlerFunc{a.PromptRule.List}},
		{custom.AdminPromptRules, http.MethodGet, "/prompt-rules/:id", []gin.HandlerFunc{a.PromptRule.GetByID}},
		{custom.AdminPromptRules, http.MethodPost, "/prompt-rules", []gin.HandlerFunc{a.PromptRule.Create}},
		{custom.AdminPromptRules, http.MethodPut, "/prompt-rules/:id", []gin.HandlerFunc{a.PromptRule.Update}},
		{custom.AdminPromptRules, http.MethodDelete, "/prompt-rules/:id", []gin.HandlerFunc{a.PromptRule.Delete}},
	}})
	hooks.workers = append(hooks.workers, promptRuleSubscription{promptRules})
	set, err := custom.NewCustomProviderSet(hooks)
	if err != nil {
		return nil, fmt.Errorf("assemble custom application: %w", err)
	}
	return custom.NewHost(set), nil
}

type adminRoute struct {
	mount    custom.AdminMount
	method   string
	path     string
	handlers []gin.HandlerFunc
}

type adminRoutes struct{ routes []adminRoute }

func (adminRoutes) ID() string                        { return "builtin-admin-extensions" }
func (adminRoutes) RegisterAdminRoutes(gin.IRouter)   {}
func (adminRoutes) RegisterGatewayRoutes(gin.IRouter) {}
func (p adminRoutes) RegisterAdminRoutesAt(router gin.IRouter, mount custom.AdminMount) {
	for _, route := range p.routes {
		if route.mount == mount {
			router.Handle(route.method, route.path, route.handlers...)
		}
	}
}

type promptRuleSubscription struct{ service *service.PromptRuleService }

func (promptRuleSubscription) ID() string                           { return "prompt-rule-subscription" }
func (w promptRuleSubscription) Start(ctx context.Context) error    { return w.service.Start(ctx) }
func (w promptRuleSubscription) Shutdown(ctx context.Context) error { return w.service.Shutdown(ctx) }

type contributions struct {
	routes      []custom.RouteProvider
	permissions []custom.PermissionProvider
	settings    []custom.SettingProvider
	workers     []custom.WorkerProvider
	enrichers   []custom.UsageEnricher
}

func (h contributions) RouteProviders() []custom.RouteProvider           { return h.routes }
func (h contributions) PermissionProviders() []custom.PermissionProvider { return h.permissions }
func (h contributions) SettingProviders() []custom.SettingProvider       { return h.settings }
func (h contributions) WorkerProviders() []custom.WorkerProvider         { return h.workers }
func (h contributions) UsageEnrichers() []custom.UsageEnricher           { return h.enrichers }
