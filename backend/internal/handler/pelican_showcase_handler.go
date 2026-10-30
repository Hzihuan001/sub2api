package handler

import (
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
	response.Success(c, gin.H{"groups": groups, "description": description})
}
