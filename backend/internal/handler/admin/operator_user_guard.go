package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func isOperatorContext(c *gin.Context) bool {
	role, ok := middleware.GetUserRoleFromContext(c)
	return ok && role == service.RoleOperator
}

// OperatorTargetUserWriteGuard prevents operators from mutating admin
// accounts. Operators may manage ordinary users and same-level operators.
func (h *UserHandler) OperatorTargetUserWriteGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isOperatorContext(c) {
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
		if target.Role == service.RoleAdmin {
			response.Forbidden(c, "operators may not modify super administrators")
			c.Abort()
			return
		}
		c.Next()
	}
}

// operatorMayMutateUsers validates an entire batch before any write occurs.
// A privileged or missing target rejects the whole request.
func (h *UserHandler) operatorMayMutateUsers(c *gin.Context, userIDs []int64) bool {
	if !isOperatorContext(c) {
		return true
	}
	for _, userID := range userIDs {
		target, err := h.adminService.GetUser(c.Request.Context(), userID)
		if err != nil {
			response.ErrorFrom(c, err)
			return false
		}
		if target.Role == service.RoleAdmin {
			response.Forbidden(c, "batch contains a super administrator")
			return false
		}
	}
	return true
}
