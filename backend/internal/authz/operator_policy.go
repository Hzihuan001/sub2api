// Package authz contains the fixed, default-deny management authorization policy.
package authz

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

type Permission string

const (
	PermissionCompliance     Permission = "compliance"
	PermissionDashboardRead  Permission = "dashboard.read"
	PermissionOpsRead        Permission = "ops.read"
	PermissionOpsDisposition Permission = "ops.disposition"
	PermissionUsersRead      Permission = "users.read"
	PermissionUsersWrite     Permission = "users.write"
	PermissionUsersSupport   Permission = "users.support"
	PermissionAnnouncements  Permission = "announcements.manage"
	PermissionRedeemCodes    Permission = "redeem_codes.manage"
	PermissionPromoCodes     Permission = "promo_codes.manage"
	PermissionUsageRead      Permission = "usage.read"
)

type routeKey struct {
	method string
	path   string
}

var operatorPermissions = map[Permission]struct{}{
	PermissionCompliance: {}, PermissionDashboardRead: {}, PermissionOpsRead: {},
	PermissionOpsDisposition: {}, PermissionUsersRead: {}, PermissionUsersWrite: {},
	PermissionUsersSupport: {}, PermissionAnnouncements: {}, PermissionRedeemCodes: {},
	PermissionPromoCodes: {}, PermissionUsageRead: {},
}

var operatorRoutes = buildOperatorRoutes()

func buildOperatorRoutes() map[routeKey]Permission {
	routes := make(map[routeKey]Permission)
	add := func(permission Permission, method string, paths ...string) {
		for _, path := range paths {
			routes[routeKey{method: method, path: path}] = permission
		}
	}

	add(PermissionCompliance, http.MethodGet, "/api/v1/admin/compliance")
	add(PermissionCompliance, http.MethodPost, "/api/v1/admin/compliance/accept")

	add(PermissionDashboardRead, http.MethodGet,
		"/api/v1/admin/dashboard/snapshot-v2", "/api/v1/admin/dashboard/stats",
		"/api/v1/admin/dashboard/realtime", "/api/v1/admin/dashboard/trend",
		"/api/v1/admin/dashboard/models", "/api/v1/admin/dashboard/groups",
		"/api/v1/admin/dashboard/api-keys-trend", "/api/v1/admin/dashboard/users-trend",
		"/api/v1/admin/dashboard/users-ranking", "/api/v1/admin/dashboard/user-breakdown",
	)
	add(PermissionDashboardRead, http.MethodPost,
		"/api/v1/admin/dashboard/users-usage", "/api/v1/admin/dashboard/api-keys-usage")

	add(PermissionOpsRead, http.MethodGet,
		"/api/v1/admin/ops/capabilities", "/api/v1/admin/ops/concurrency",
		"/api/v1/admin/ops/user-concurrency", "/api/v1/admin/ops/account-availability",
		"/api/v1/admin/ops/realtime-traffic", "/api/v1/admin/ops/alert-rules",
		"/api/v1/admin/ops/alert-events", "/api/v1/admin/ops/alert-events/:id",
		"/api/v1/admin/ops/email-notification/config", "/api/v1/admin/ops/runtime/alert",
		"/api/v1/admin/ops/runtime/logging",
		"/api/v1/admin/ops/settings/metric-thresholds", "/api/v1/admin/ops/ws/qps",
		"/api/v1/admin/ops/errors", "/api/v1/admin/ops/errors/:id",
		"/api/v1/admin/ops/request-errors", "/api/v1/admin/ops/request-errors/:id",
		"/api/v1/admin/ops/request-errors/:id/upstream-errors",
		"/api/v1/admin/ops/ingress-rejections", "/api/v1/admin/ops/ingress-rejections/health",
		"/api/v1/admin/ops/auth-cache-invalidation/health",
		"/api/v1/admin/ops/upstream-errors", "/api/v1/admin/ops/upstream-errors/:id",
		"/api/v1/admin/ops/requests", "/api/v1/admin/ops/system-logs",
		"/api/v1/admin/ops/system-logs/health", "/api/v1/admin/ops/dashboard/snapshot-v2",
		"/api/v1/admin/ops/dashboard/overview", "/api/v1/admin/ops/dashboard/throughput-trend",
		"/api/v1/admin/ops/dashboard/latency-histogram", "/api/v1/admin/ops/dashboard/error-trend",
		"/api/v1/admin/ops/dashboard/error-distribution", "/api/v1/admin/ops/dashboard/openai-token-stats",
	)
	add(PermissionOpsDisposition, http.MethodPut,
		"/api/v1/admin/ops/alert-events/:id/status", "/api/v1/admin/ops/errors/:id/resolve",
		"/api/v1/admin/ops/request-errors/:id/resolve", "/api/v1/admin/ops/upstream-errors/:id/resolve")
	add(PermissionOpsDisposition, http.MethodPost, "/api/v1/admin/ops/alert-silences")

	add(PermissionUsersRead, http.MethodGet,
		"/api/v1/admin/users", "/api/v1/admin/users/:id", "/api/v1/admin/users/:id/api-keys",
		"/api/v1/admin/users/:id/usage", "/api/v1/admin/users/:id/balance-history",
		"/api/v1/admin/users/:id/rpm-status", "/api/v1/admin/users/:id/platform-quotas",
		"/api/v1/admin/users/:id/attributes", "/api/v1/admin/users/:id/subscriptions")
	add(PermissionUsersWrite, http.MethodPost,
		"/api/v1/admin/users", "/api/v1/admin/users/:id/auth-identities",
		"/api/v1/admin/users/:id/balance", "/api/v1/admin/users/:id/replace-group",
		"/api/v1/admin/users/batch-concurrency", "/api/v1/admin/users/batch-limits",
		"/api/v1/admin/users/:id/platform-quotas/reset")
	add(PermissionUsersWrite, http.MethodPut,
		"/api/v1/admin/users/:id", "/api/v1/admin/users/:id/platform-quotas",
		"/api/v1/admin/users/:id/attributes")
	add(PermissionUsersWrite, http.MethodDelete, "/api/v1/admin/users/:id")
	add(PermissionUsersSupport, http.MethodGet,
		"/api/v1/admin/groups/all", "/api/v1/admin/user-attributes")
	add(PermissionUsersSupport, http.MethodPost, "/api/v1/admin/user-attributes/batch")
	add(PermissionUsersWrite, http.MethodPut, "/api/v1/admin/api-keys/:id")

	add(PermissionAnnouncements, http.MethodGet,
		"/api/v1/admin/announcements", "/api/v1/admin/announcements/:id",
		"/api/v1/admin/announcements/:id/read-status")
	add(PermissionAnnouncements, http.MethodPost, "/api/v1/admin/announcements")
	add(PermissionAnnouncements, http.MethodPut, "/api/v1/admin/announcements/:id")
	add(PermissionAnnouncements, http.MethodDelete, "/api/v1/admin/announcements/:id")

	add(PermissionRedeemCodes, http.MethodGet,
		"/api/v1/admin/redeem-codes", "/api/v1/admin/redeem-codes/stats",
		"/api/v1/admin/redeem-codes/export", "/api/v1/admin/redeem-codes/:id")
	add(PermissionRedeemCodes, http.MethodPost,
		"/api/v1/admin/redeem-codes/create-and-redeem", "/api/v1/admin/redeem-codes/generate",
		"/api/v1/admin/redeem-codes/batch-delete", "/api/v1/admin/redeem-codes/batch-update",
		"/api/v1/admin/redeem-codes/:id/expire")
	add(PermissionRedeemCodes, http.MethodDelete, "/api/v1/admin/redeem-codes/:id")

	add(PermissionPromoCodes, http.MethodGet,
		"/api/v1/admin/promo-codes", "/api/v1/admin/promo-codes/:id",
		"/api/v1/admin/promo-codes/:id/usages")
	add(PermissionPromoCodes, http.MethodPost, "/api/v1/admin/promo-codes")
	add(PermissionPromoCodes, http.MethodPut, "/api/v1/admin/promo-codes/:id")
	add(PermissionPromoCodes, http.MethodDelete, "/api/v1/admin/promo-codes/:id")

	add(PermissionUsageRead, http.MethodGet,
		"/api/v1/admin/usage", "/api/v1/admin/usage/stats",
		"/api/v1/admin/usage/search-users", "/api/v1/admin/usage/search-api-keys",
		"/api/v1/admin/usage/filter-options", "/api/v1/admin/usage/cleanup-tasks")

	return routes
}

func HasPermission(role string, permission Permission) bool {
	if role == domain.RoleAdmin {
		return true
	}
	if role != domain.RoleOperator {
		return false
	}
	_, ok := operatorPermissions[permission]
	return ok
}

// PermissionForRoute returns the explicit operator permission for a Gin route
// template. A missing entry is intentionally a denial.
func PermissionForRoute(method, fullPath string) (Permission, bool) {
	permission, ok := operatorRoutes[routeKey{method: method, path: fullPath}]
	return permission, ok
}

func CanAccessRoute(role, method, fullPath string) bool {
	if role == domain.RoleAdmin {
		return true
	}
	permission, ok := PermissionForRoute(method, fullPath)
	return ok && HasPermission(role, permission)
}
