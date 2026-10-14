package custom

import "github.com/gin-gonic/gin"

// AdminMount names the existing insertion points in the upstream admin router.
// The router passed to a mount already owns its authentication and middleware.
type AdminMount string

const (
	AdminRoles                AdminMount = "roles"
	AdminUserAPIKeys          AdminMount = "user-api-keys"
	AdminGroupEffectiveModels AdminMount = "group-effective-models"
	AdminOpsCapabilities      AdminMount = "ops-capabilities"
	AdminOpsErrors            AdminMount = "ops-errors"
	AdminUsageFilters         AdminMount = "usage-filters"
	AdminAPIKeys              AdminMount = "api-keys"
	AdminPromptAuditRecording AdminMount = "prompt-audit-recording"
	AdminPromptAuditExport    AdminMount = "prompt-audit-export"
	AdminPromptRules          AdminMount = "prompt-rules"
)

// MountedAdminRouteProvider extends RouteProvider for built-in additions that
// must keep their original position among native routes. Ordinary optional
// providers continue to use RegisterAdminRoutes at the end of the admin group.
type MountedAdminRouteProvider interface {
	RouteProvider
	RegisterAdminRoutesAt(gin.IRouter, AdminMount)
}

func (h *Host) RegisterAdminRoutesAt(router gin.IRouter, mount AdminMount) {
	if h == nil || router == nil {
		return
	}
	for _, provider := range h.providers.Routes() {
		if mounted, ok := provider.(MountedAdminRouteProvider); ok {
			mounted.RegisterAdminRoutesAt(router, mount)
		}
	}
}
