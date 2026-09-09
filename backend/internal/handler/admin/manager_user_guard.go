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

func managerMayMutateTarget(target *service.User) bool {
	return target != nil && target.Role != service.RoleAdmin
}

func managerMayAssignRole(role string) bool {
	return role == "" || role == service.RoleUser || role == service.RoleManager
}

// ManagerTargetUserWriteGuard lets a manager administer ordinary users and
// peer managers, while retaining a hard boundary around the super admin role.
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
		if !managerMayMutateTarget(target) {
			response.Forbidden(c, "managers cannot modify super admin users")
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
		if !managerMayMutateTarget(target) {
			response.Forbidden(c, "batch contains a super admin user")
			return false
		}
	}
	return true
}
