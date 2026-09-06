package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AdminOnly 管理员权限中间件
// 必须在JWTAuth中间件之后使用
func AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := GetUserRoleFromContext(c)
		if !ok {
			AbortWithError(c, 401, "UNAUTHORIZED", "User not found in context")
			return
		}

		// 超级管理员和管理员均可进入常规管理后台。
		if role != service.RoleAdmin && role != service.RoleManager {
			AbortWithError(c, 403, "FORBIDDEN", "Admin access required")
			return
		}

		c.Next()
	}
}

// SuperAdminOnly restricts sensitive operational and data-management surfaces
// to the owner-level role. It must run after an authentication middleware.
func SuperAdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := GetUserRoleFromContext(c)
		if !ok {
			AbortWithError(c, 401, "UNAUTHORIZED", "User not found in context")
			return
		}
		if role != service.RoleAdmin {
			AbortWithError(c, 403, "FORBIDDEN", "Super admin access required")
			return
		}
		c.Next()
	}
}
