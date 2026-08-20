package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// OperatorTargetUserWriteGuard resolves an API key without changing it, then
// applies the same ordinary-user-only rule as the user management endpoints.
func (h *AdminAPIKeyHandler) OperatorTargetUserWriteGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isOperatorContext(c) {
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
		if target.Role != service.RoleUser {
			response.Forbidden(c, "operators may only modify API keys owned by ordinary users")
			c.Abort()
			return
		}
		c.Next()
	}
}
