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
}

func NewPelicanShowcaseHandler(
	showcaseService *service.PelicanShowcaseService,
	apiKeyService *service.APIKeyService,
) *PelicanShowcaseHandler {
	return &PelicanShowcaseHandler{showcaseService: showcaseService, apiKeyService: apiKeyService}
}

func (h *PelicanShowcaseHandler) Get(c *gin.Context) {
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
	response.Success(c, gin.H{"groups": groups})
}
