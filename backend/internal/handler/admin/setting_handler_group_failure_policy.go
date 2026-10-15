package admin

import (
	"encoding/json"
	"io"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetGroupFailurePolicy handles GET /api/v1/admin/groups/:id/failure-policy.
func (h *SettingHandler) GetGroupFailurePolicy(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || groupID <= 0 {
		response.BadRequest(c, "group ID must be a positive integer")
		return
	}
	policy, err := h.settingService.GetGroupFailurePolicy(c.Request.Context(), groupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

// UpdateGroupFailurePolicy handles PUT /api/v1/admin/groups/:id/failure-policy.
func (h *SettingHandler) UpdateGroupFailurePolicy(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || groupID <= 0 {
		response.BadRequest(c, "group ID must be a positive integer")
		return
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var policy *service.GroupFailurePolicy
	if err := decoder.Decode(&policy); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.BadRequest(c, "Invalid request: expected one JSON object")
		return
	}
	updated, err := h.settingService.UpdateGroupFailurePolicy(c.Request.Context(), groupID, policy)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, updated)
}
