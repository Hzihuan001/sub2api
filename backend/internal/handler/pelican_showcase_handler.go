package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type PelicanShowcaseHandler struct {
	showcaseService *service.PelicanShowcaseService
	apiKeyService   *service.APIKeyService
	settingService  *service.SettingService
}

func NewPelicanShowcaseHandler(
	showcaseService *service.PelicanShowcaseService,
	apiKeyService *service.APIKeyService,
	settingService *service.SettingService,
) *PelicanShowcaseHandler {
	return &PelicanShowcaseHandler{showcaseService: showcaseService, apiKeyService: apiKeyService, settingService: settingService}
}

func (h *PelicanShowcaseHandler) Get(c *gin.Context) {
	if h.settingService == nil {
		response.NotFound(c, "Pelican showcase is not enabled")
		return
	}
	enabled, description := h.settingService.GetPelicanShowcaseRuntime(c.Request.Context())
	if !enabled {
		response.NotFound(c, "Pelican showcase is not enabled")
		return
	}
	subject, authed := middleware.GetAuthSubjectFromContext(c)
	if !authed {
		response.Unauthorized(c, "Authentication required")
		return
	}
	allowed, restrictPublic, err := h.apiKeyService.GetUserGroupVisibility(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	groups, err := h.showcaseService.ListLatest(c.Request.Context(), allowed, restrictPublic)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	groups = service.OrderPelicanShowcaseGroups(groups, h.settingService.GetPelicanShowcaseGroupOrder(c.Request.Context()))
	response.Success(c, gin.H{"groups": groups, "description": description})
}

func (h *PelicanShowcaseHandler) GetOrder(c *gin.Context) {
	response.Success(c, gin.H{"group_ids": h.settingService.GetPelicanShowcaseGroupOrder(c.Request.Context())})
}

func (h *PelicanShowcaseHandler) UpdateOrder(c *gin.Context) {
	var req struct {
		GroupIDs []int64 `json:"group_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := h.settingService.SetPelicanShowcaseGroupOrder(c.Request.Context(), req.GroupIDs); err != nil {
		response.ErrorWithDetails(c, http.StatusInternalServerError, err.Error(), "PELICAN_SHOWCASE_ORDER_SAVE_FAILED", nil)
		return
	}
	response.Success(c, gin.H{"group_ids": h.settingService.GetPelicanShowcaseGroupOrder(c.Request.Context())})
}
