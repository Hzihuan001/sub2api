//go:build unit

package authz

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestOperatorPolicyExplicitAllowAndDefaultDeny(t *testing.T) {
	allowed := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/dashboard/stats"},
		{http.MethodGet, "/api/v1/admin/ops/ws/qps"},
		{http.MethodPut, "/api/v1/admin/ops/errors/:id/resolve"},
		{http.MethodPost, "/api/v1/admin/users/batch-limits"},
		{http.MethodPut, "/api/v1/admin/api-keys/:id"},
		{http.MethodGet, "/api/v1/admin/groups/all"},
		{http.MethodPost, "/api/v1/admin/announcements"},
		{http.MethodDelete, "/api/v1/admin/redeem-codes/:id"},
		{http.MethodPut, "/api/v1/admin/promo-codes/:id"},
		{http.MethodGet, "/api/v1/admin/usage/cleanup-tasks"},
		{http.MethodGet, "/api/v1/admin/usage/filter-options"},
		{http.MethodPost, "/api/v1/admin/compliance/accept"},
	}
	for _, route := range allowed {
		require.True(t, CanAccessRoute(domain.RoleOperator, route.method, route.path), "%s %s", route.method, route.path)
	}

	denied := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/admin/dashboard/aggregation/backfill"},
		{http.MethodPut, "/api/v1/admin/ops/alert-rules/:id"},
		{http.MethodPut, "/api/v1/admin/ops/runtime/logging"},
		{http.MethodGet, "/api/v1/admin/ops/advanced-settings"},
		{http.MethodPost, "/api/v1/admin/ops/system-logs/cleanup"},
		{http.MethodPost, "/api/v1/admin/usage/cleanup-tasks"},
		{http.MethodPost, "/api/v1/admin/usage/cleanup-tasks/:id/cancel"},
		{http.MethodGet, "/api/v1/admin/settings"},
		{http.MethodGet, "/api/v1/admin/accounts"},
		{http.MethodGet, "/api/v1/admin/plugins"},
		{http.MethodPost, "/api/v1/admin/plugins/:id/enable"},
		{http.MethodGet, "/api/v1/admin/audit-logs"},
		{http.MethodGet, "/api/v1/admin/future-upstream-route"},
	}
	for _, route := range denied {
		require.False(t, CanAccessRoute(domain.RoleOperator, route.method, route.path), "%s %s", route.method, route.path)
	}
}

func TestAdminBypassesRouteTableAndUserCannotUseIt(t *testing.T) {
	unknown := "/api/v1/admin/future-upstream-route"
	require.True(t, CanAccessRoute(domain.RoleAdmin, http.MethodPatch, unknown))
	require.False(t, CanAccessRoute(domain.RoleUser, http.MethodGet, "/api/v1/admin/dashboard/stats"))
}
