package authz

import (
	"net/http"
	"sort"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/custom"
	"github.com/stretchr/testify/require"
)

func TestOperatorPolicyProviderExposesStablePermissionCatalog(t *testing.T) {
	provider := NewOperatorPolicyProvider()
	permissions := provider.Permissions()
	require.NotEmpty(t, permissions)
	require.Equal(t, "compliance", permissions[0].Name)

	names := make([]string, 0, len(permissions))
	seen := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		require.NotEmpty(t, permission.Name)
		require.NotContains(t, permission.Name, " ")
		require.NotContains(t, seen, permission.Name)
		seen[permission.Name] = struct{}{}
		names = append(names, permission.Name)
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	require.Equal(t, sorted, names)
	require.Contains(t, names, string(PermissionRolePolicyRead))
	require.Contains(t, names, string(PermissionDashboardRead))
}

func TestOperatorPolicyProviderExposesExactAndPrefixRoutesWithoutChangingMatcher(t *testing.T) {
	provider := NewOperatorPolicyProvider()
	routes := provider.Routes()
	require.NotEmpty(t, routes)

	var exact, prefix bool
	for _, route := range routes {
		switch route.Match {
		case RouteMatchExact:
			exact = true
		case RouteMatchPrefix:
			prefix = true
			require.NotEmpty(t, route.Path)
			require.Contains(t, []string{http.MethodGet, http.MethodHead, "*"}, route.Method)
		default:
			t.Fatalf("unknown route match %q", route.Match)
		}
	}
	require.True(t, exact)
	require.True(t, prefix)

	require.Contains(t, routes, OperatorRouteDeclaration{
		Method: http.MethodGet, Path: "/api/v1/admin/groups", Permission: PermissionGroupsRead, Match: RouteMatchPrefix,
	})
	require.Contains(t, routes, OperatorRouteDeclaration{
		Method: http.MethodGet, Path: "/api/v1/admin/dashboard/stats", Permission: PermissionDashboardRead, Match: RouteMatchExact,
	})
	permission, ok := PermissionForRoute(http.MethodGet, "/api/v1/admin/groups/42")
	require.True(t, ok)
	require.Equal(t, PermissionGroupsRead, permission)
}

func TestOperatorPolicyProviderCanBeRegisteredThroughCustomPermissionBoundary(t *testing.T) {
	provider := NewOperatorPolicyProvider()
	var _ custom.PermissionProvider = provider
	set, err := custom.NewCustomProviderSet(operatorProviderHooks{permissions: []custom.PermissionProvider{provider}})
	require.NoError(t, err)
	require.Len(t, set.Permissions(), 1)
	require.Equal(t, provider.ID(), set.Permissions()[0].ID())
}

type operatorProviderHooks struct {
	permissions []custom.PermissionProvider
}

func (h operatorProviderHooks) RouteProviders() []custom.RouteProvider { return nil }
func (h operatorProviderHooks) PermissionProviders() []custom.PermissionProvider {
	return h.permissions
}
func (h operatorProviderHooks) SettingProviders() []custom.SettingProvider { return nil }
func (h operatorProviderHooks) WorkerProviders() []custom.WorkerProvider { return nil }
func (h operatorProviderHooks) UsageEnrichers() []custom.UsageEnricher { return nil }
