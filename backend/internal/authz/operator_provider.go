package authz

import (
	"net/http"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/custom"
)

// RouteMatch describes how a management route declaration is matched.
// Exact declarations mirror the explicit table, while prefix declarations
// mirror the small set of module fallbacks used by PermissionForRoute.
type RouteMatch string

const (
	RouteMatchExact  RouteMatch = "exact"
	RouteMatchPrefix RouteMatch = "prefix"
)

// OperatorRouteDeclaration is the stable, read-only description of one
// operator route policy.  It is intentionally separate from Gin and the
// middleware implementation so a custom build can register or inspect the
// policy without importing the server package.
//
// Prefix declarations use Method "*" for all methods other than GET/HEAD;
// GET and HEAD are emitted as separate declarations with the read permission.
type OperatorRouteDeclaration struct {
	Method     string
	Path       string
	Permission Permission
	Match      RouteMatch
}

// OperatorPolicyProvider adapts the existing operator permission catalog and
// route table to the custom provider boundary. It is metadata only: creating
// this provider does not change authorization or register routes.
type OperatorPolicyProvider struct{}

func (OperatorPolicyProvider) ID() string { return "operator" }

// Permissions returns a fresh, deterministically ordered catalog. Immutable
// bootstrap permissions are included so consumers cannot accidentally build a
// partial policy surface and silently omit compliance or effective-policy
// access.
func (OperatorPolicyProvider) Permissions() []custom.Permission {
	permissions := make([]custom.Permission, 0, len(knownOperatorPermissions))
	for permission := range knownOperatorPermissions {
		permissions = append(permissions, custom.Permission{
			Name: string(permission),
		})
	}
	sort.Slice(permissions, func(i, j int) bool { return permissions[i].Name < permissions[j].Name })
	return permissions
}

// Routes returns a fresh, deterministic copy of the current operator route
// declarations. The route matcher continues to use the existing maps; this
// method only exposes a stable adapter for future custom wiring.
func (OperatorPolicyProvider) Routes() []OperatorRouteDeclaration {
	declarations := make([]OperatorRouteDeclaration, 0, len(operatorRoutes)+len(moduleRoutePermissions)*3+len(settingsSubRoutePermissions)*3)
	for key, permission := range operatorRoutes {
		declarations = append(declarations, OperatorRouteDeclaration{
			Method:     key.method,
			Path:       key.path,
			Permission: permission,
			Match:      RouteMatchExact,
		})
	}
	appendPrefixes := func(modules []moduleRoutePermission) {
		for _, module := range modules {
			declarations = append(declarations,
				OperatorRouteDeclaration{Method: http.MethodGet, Path: module.prefix, Permission: module.read, Match: RouteMatchPrefix},
				OperatorRouteDeclaration{Method: http.MethodHead, Path: module.prefix, Permission: module.read, Match: RouteMatchPrefix},
				OperatorRouteDeclaration{Method: "*", Path: module.prefix, Permission: module.write, Match: RouteMatchPrefix},
			)
		}
	}
	appendPrefixes(settingsSubRoutePermissions)
	appendPrefixes(moduleRoutePermissions)
	sort.Slice(declarations, func(i, j int) bool {
		left, right := declarations[i], declarations[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Method != right.Method {
			return left.Method < right.Method
		}
		if left.Match != right.Match {
			return left.Match < right.Match
		}
		return left.Permission < right.Permission
	})
	return declarations
}

// NewOperatorPolicyProvider returns the adapter as a custom permission
// provider. The concrete return type intentionally also exposes Routes for
// callers that need the route declaration catalog.
func NewOperatorPolicyProvider() OperatorPolicyProvider {
	return OperatorPolicyProvider{}
}
