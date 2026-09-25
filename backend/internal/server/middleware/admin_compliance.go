package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AdminComplianceGuard is retained as a middleware seam for deployments that
// still register the legacy compliance endpoints.  Station deployments do
// not require an administrator acknowledgement, so the middleware is
// intentionally non-blocking.  Keeping the route and service APIs intact
// avoids breaking clients and upgrades while removing the modal/423 gate.
func AdminComplianceGuard(_ *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}
