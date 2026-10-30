package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
)

// RegisterPelicanShowcaseRoutes exposes the read-only group comparison page.
// It intentionally reuses normal user authentication and panel rate limits.
func RegisterPelicanShowcaseRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	panelRateLimiter *middleware.PanelRateLimiter,
) {
	showcase := v1.Group("/pelican-showcase")
	showcase.Use(gin.HandlerFunc(jwtAuth))
	showcase.Use(panelRateLimiter.Global())
	showcase.GET("", h.PelicanShowcase.Get)
}
