package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetOperatorRolePolicy(c *gin.Context) {
	policy, err := h.settingService.GetOperatorRolePolicy(c.Request.Context())
	if err != nil {
		response.InternalError(c, "Failed to load operator role permissions")
		return
	}
	response.Success(c, policy)
}

func (h *SettingHandler) UpdateOperatorRolePolicy(c *gin.Context) {
	var requested authz.OperatorPolicy
	if err := c.ShouldBindJSON(&requested); err != nil || requested.Permissions == nil {
		response.BadRequest(c, "Invalid operator role permissions")
		return
	}
	for key := range requested.Permissions {
		if !authz.IsConfigurableOperatorPermission(authz.Permission(key)) {
			response.BadRequest(c, "Unknown operator permission: "+key)
			return
		}
	}

	policy, err := h.settingService.SetOperatorRolePolicy(c.Request.Context(), requested)
	if err != nil {
		response.InternalError(c, "Failed to save operator role permissions")
		return
	}
	response.Success(c, policy)
}
