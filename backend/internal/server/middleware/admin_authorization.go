package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/gin-gonic/gin"
)

// AdminRouteAuthorization applies the fixed operator allowlist. Admins retain
// their existing access; every unregistered management route is denied to an
// operator, including routes added by a future upstream upgrade.
func AdminRouteAuthorization() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := GetUserRoleFromContext(c)
		if !ok {
			AbortWithError(c, 401, "UNAUTHORIZED", "User not found in context")
			return
		}
		if !authz.CanAccessRoute(role, c.Request.Method, c.FullPath()) {
			AbortWithError(c, 403, "FORBIDDEN", "Management permission denied")
			return
		}
		c.Next()
	}
}
