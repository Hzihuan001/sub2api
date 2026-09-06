package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ManagerTargetUserWriteGuard resolves an API key without mutating it and
// applies the same ordinary-user-only boundary as user management routes.
func (h *AdminAPIKeyHandler) ManagerTargetUserWriteGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isManagerContext(c) {
			c.Next()
			return
		}
		keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || keyID <= 0 {
			response.BadRequest(c, "Invalid API key ID")
			c.Abort()
			return
		}
		result, err := h.adminService.AdminUpdateAPIKeyGroupID(c.Request.Context(), keyID, nil)
		if err != nil {
			response.ErrorFrom(c, err)
			c.Abort()
			return
		}
		if result == nil || result.APIKey == nil {
			response.NotFound(c, "API key not found")
			c.Abort()
			return
		}
		target, err := h.adminService.GetUser(c.Request.Context(), result.APIKey.UserID)
		if err != nil {
			response.ErrorFrom(c, err)
			c.Abort()
			return
		}
		if target == nil || target.Role != service.RoleUser {
			response.Forbidden(c, "managers may only modify API keys owned by ordinary users")
			c.Abort()
			return
		}
		c.Next()
	}
}
