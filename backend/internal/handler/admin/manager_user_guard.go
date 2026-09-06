package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func isManagerContext(c *gin.Context) bool {
	role, ok := middleware.GetUserRoleFromContext(c)
	return ok && role == service.RoleManager
}

// ManagerTargetUserWriteGuard prevents a manager from mutating either of the
// two administrative roles through any single-user management sub-route.
func (h *UserHandler) ManagerTargetUserWriteGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isManagerContext(c) {
			c.Next()
			return
		}
		userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || userID <= 0 {
			response.BadRequest(c, "Invalid user ID")
			c.Abort()
			return
		}
		target, err := h.adminService.GetUser(c.Request.Context(), userID)
		if err != nil {
			response.ErrorFrom(c, err)
			c.Abort()
			return
		}
		if target == nil || target.Role != service.RoleUser {
			response.Forbidden(c, "managers may only modify ordinary users")
			c.Abort()
			return
		}
		c.Next()
	}
}

// managerMayMutateUsers validates the whole batch before any write. This is
// intentionally all-or-nothing so a mixed privileged batch cannot partially
// update ordinary users before being rejected.
func (h *UserHandler) managerMayMutateUsers(c *gin.Context, userIDs []int64) bool {
	if !isManagerContext(c) {
		return true
	}
	for _, userID := range userIDs {
		target, err := h.adminService.GetUser(c.Request.Context(), userID)
		if err != nil {
			response.ErrorFrom(c, err)
			return false
		}
		if target == nil || target.Role != service.RoleUser {
			response.Forbidden(c, "batch contains an administrative user")
			return false
		}
	}
	return true
}
