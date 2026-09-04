// Package authz contains the explicit, default-deny management authorization policy.
package authz

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

type Permission string

const (
	PermissionCompliance         Permission = "compliance"
	PermissionRolePolicyRead     Permission = "role_policy.read"
	PermissionDashboardRead      Permission = "dashboard.read"
	PermissionOpsRead            Permission = "ops.read"
	PermissionOpsDisposition     Permission = "ops.disposition"
	PermissionUsersRead          Permission = "users.read"
	PermissionUsersWrite         Permission = "users.write"
	PermissionUsersBalanceWrite  Permission = "users.balance.write"
	PermissionUsersSupport       Permission = "users.support"
	PermissionAnnouncementsRead  Permission = "announcements.read"
	PermissionAnnouncementsWrite Permission = "announcements.write"
	PermissionRedeemCodesRead    Permission = "redeem_codes.read"
	PermissionRedeemCodesWrite   Permission = "redeem_codes.write"
	PermissionPromoCodesRead     Permission = "promo_codes.read"
	PermissionPromoCodesWrite    Permission = "promo_codes.write"
	PermissionUsageRead          Permission = "usage.read"

	PermissionFinanceUserBalanceRead  Permission = "finance.user_balance.read"
	PermissionFinanceUserChargeRead   Permission = "finance.user_charge.read"
	PermissionFinanceStandardCostRead Permission = "finance.standard_cost.read"
	PermissionFinanceUpstreamCostRead Permission = "finance.upstream_cost.read"
	PermissionFinanceProfitRead       Permission = "finance.profit.read"
)

type routeKey struct {
	method string
	path   string
}

var configurableOperatorPermissions = []Permission{
	PermissionDashboardRead,
	PermissionOpsRead,
	PermissionOpsDisposition,
	PermissionUsersRead,
	PermissionUsersWrite,
	PermissionUsersBalanceWrite,
	PermissionUsersSupport,
	PermissionAnnouncementsRead,
	PermissionAnnouncementsWrite,
	PermissionRedeemCodesRead,
	PermissionRedeemCodesWrite,
	PermissionPromoCodesRead,
	PermissionPromoCodesWrite,
	PermissionUsageRead,
	PermissionFinanceUserBalanceRead,
	PermissionFinanceUserChargeRead,
	PermissionFinanceStandardCostRead,
	PermissionFinanceUpstreamCostRead,
	PermissionFinanceProfitRead,
}

var knownOperatorPermissions = func() map[Permission]struct{} {
	out := make(map[Permission]struct{}, len(configurableOperatorPermissions)+2)
	out[PermissionCompliance] = struct{}{}
	out[PermissionRolePolicyRead] = struct{}{}
	for _, permission := range configurableOperatorPermissions {
		out[permission] = struct{}{}
	}
	return out
}()

// OperatorPolicy is the persisted, global permission template for every
// operator. Admin authorization remains unconditional and is not affected by
// this policy.
type OperatorPolicy struct {
	Permissions map[string]bool `json:"permissions"`
}

func DefaultOperatorPolicy() OperatorPolicy {
	permissions := make(map[string]bool, len(configurableOperatorPermissions))
	for _, permission := range configurableOperatorPermissions {
		permissions[string(permission)] = true
	}
	// Financial data is opt-in. A fresh upgrade therefore keeps the current
	// operational pages available without exposing monetary information.
	permissions[string(PermissionFinanceUserBalanceRead)] = false
	permissions[string(PermissionFinanceUserChargeRead)] = false
	permissions[string(PermissionFinanceStandardCostRead)] = false
	permissions[string(PermissionFinanceUpstreamCostRead)] = false
	permissions[string(PermissionFinanceProfitRead)] = false
	return OperatorPolicy{Permissions: permissions}
}

// FailClosedOperatorPolicy is used only when the persisted policy cannot be
// read and no previously cached value exists. Immutable bootstrap permissions
// such as compliance and reading one's effective policy remain available via
// Allows, while every configurable permission is denied.
func FailClosedOperatorPolicy() OperatorPolicy {
	permissions := make(map[string]bool, len(configurableOperatorPermissions))
	for _, permission := range configurableOperatorPermissions {
		permissions[string(permission)] = false
	}
	return OperatorPolicy{Permissions: permissions}
}

func NormalizeOperatorPolicy(input OperatorPolicy) OperatorPolicy {
	normalized := DefaultOperatorPolicy()
	for key, enabled := range input.Permissions {
		permission := Permission(key)
		if _, ok := knownOperatorPermissions[permission]; !ok || permission == PermissionCompliance || permission == PermissionRolePolicyRead {
			continue
		}
		normalized.Permissions[key] = enabled
	}
	return normalized
}

func IsConfigurableOperatorPermission(permission Permission) bool {
	if permission == PermissionCompliance || permission == PermissionRolePolicyRead {
		return false
	}
	_, ok := knownOperatorPermissions[permission]
	return ok
}

func (p OperatorPolicy) Allows(permission Permission) bool {
	if permission == PermissionCompliance || permission == PermissionRolePolicyRead {
		return true
	}
	if !p.Permissions[string(permission)] {
		return false
	}
	switch permission {
	case PermissionOpsDisposition:
		return p.Permissions[string(PermissionOpsRead)]
	case PermissionUsersWrite, PermissionUsersSupport:
		return p.Permissions[string(PermissionUsersRead)]
	case PermissionUsersBalanceWrite:
		return p.Permissions[string(PermissionUsersRead)] && p.Permissions[string(PermissionUsersWrite)]
	case PermissionAnnouncementsWrite:
		return p.Permissions[string(PermissionAnnouncementsRead)]
	case PermissionRedeemCodesWrite:
		return p.Permissions[string(PermissionRedeemCodesRead)]
	case PermissionPromoCodesWrite:
		return p.Permissions[string(PermissionPromoCodesRead)]
	default:
		return true
	}
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
	add(PermissionRolePolicyRead, http.MethodGet, "/api/v1/admin/roles/operator/permissions")

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
		"/api/v1/admin/users/:id/replace-group",
		"/api/v1/admin/users/batch-concurrency", "/api/v1/admin/users/batch-limits",
		"/api/v1/admin/users/:id/platform-quotas/reset")
	add(PermissionUsersBalanceWrite, http.MethodPost, "/api/v1/admin/users/:id/balance")
	add(PermissionUsersWrite, http.MethodPut,
		"/api/v1/admin/users/:id", "/api/v1/admin/users/:id/platform-quotas",
		"/api/v1/admin/users/:id/attributes")
	add(PermissionUsersWrite, http.MethodDelete, "/api/v1/admin/users/:id")
	add(PermissionUsersSupport, http.MethodGet,
		"/api/v1/admin/groups/all", "/api/v1/admin/user-attributes")
	add(PermissionUsersSupport, http.MethodPost, "/api/v1/admin/user-attributes/batch")
	add(PermissionUsersWrite, http.MethodPut, "/api/v1/admin/api-keys/:id")

	add(PermissionAnnouncementsRead, http.MethodGet,
		"/api/v1/admin/announcements", "/api/v1/admin/announcements/:id",
		"/api/v1/admin/announcements/:id/read-status")
	add(PermissionAnnouncementsWrite, http.MethodPost, "/api/v1/admin/announcements")
	add(PermissionAnnouncementsWrite, http.MethodPut, "/api/v1/admin/announcements/:id")
	add(PermissionAnnouncementsWrite, http.MethodDelete, "/api/v1/admin/announcements/:id")

	add(PermissionRedeemCodesRead, http.MethodGet,
		"/api/v1/admin/redeem-codes", "/api/v1/admin/redeem-codes/stats",
		"/api/v1/admin/redeem-codes/export", "/api/v1/admin/redeem-codes/:id")
	add(PermissionRedeemCodesWrite, http.MethodPost,
		"/api/v1/admin/redeem-codes/create-and-redeem", "/api/v1/admin/redeem-codes/generate",
		"/api/v1/admin/redeem-codes/batch-delete", "/api/v1/admin/redeem-codes/batch-update",
		"/api/v1/admin/redeem-codes/:id/expire")
	add(PermissionRedeemCodesWrite, http.MethodDelete, "/api/v1/admin/redeem-codes/:id")

	add(PermissionPromoCodesRead, http.MethodGet,
		"/api/v1/admin/promo-codes", "/api/v1/admin/promo-codes/:id",
		"/api/v1/admin/promo-codes/:id/usages")
	add(PermissionPromoCodesWrite, http.MethodPost, "/api/v1/admin/promo-codes")
	add(PermissionPromoCodesWrite, http.MethodPut, "/api/v1/admin/promo-codes/:id")
	add(PermissionPromoCodesWrite, http.MethodDelete, "/api/v1/admin/promo-codes/:id")

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
	return DefaultOperatorPolicy().Allows(permission)
}

// PermissionForRoute returns the explicit operator permission for a Gin route
// template. A missing entry is intentionally a denial.
func PermissionForRoute(method, fullPath string) (Permission, bool) {
	permission, ok := operatorRoutes[routeKey{method: method, path: fullPath}]
	return permission, ok
}

func CanAccessRoute(role, method, fullPath string) bool {
	return CanAccessRouteWithPolicy(role, method, fullPath, DefaultOperatorPolicy())
}

func CanAccessRouteWithPolicy(role, method, fullPath string, policy OperatorPolicy) bool {
	if role == domain.RoleAdmin {
		return true
	}
	permission, ok := PermissionForRoute(method, fullPath)
	return role == domain.RoleOperator && ok && policy.Allows(permission)
}
