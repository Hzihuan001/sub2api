package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AdminRouteAuthorization applies the explicit operator route table and the
// persisted global operator policy. Admins retain their existing access;
// every unregistered management route is denied to an operator.
func AdminRouteAuthorization(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := GetUserRoleFromContext(c)
		if !ok {
			AbortWithError(c, 401, "UNAUTHORIZED", "User not found in context")
			return
		}
		policy := authz.DefaultOperatorPolicy()
		if role == service.RoleOperator && settingService != nil {
			policy = settingService.GetOperatorRolePolicyCached(c.Request.Context())
		}
		if !authz.CanAccessRouteWithPolicy(role, c.Request.Method, c.FullPath(), policy) {
			AbortWithError(c, 403, "FORBIDDEN", "Management permission denied")
			return
		}
		c.Next()
	}
}
