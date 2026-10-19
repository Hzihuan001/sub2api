package handler

import (
	"fmt"
	"net/http"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Background creation must validate before persistence, queueing or billing,
// even when a handler is mounted separately from the public gateway chain.
// Existing task reads/cancellations intentionally do not call this guard.
func checkKeyModelCreation(c *gin.Context, apiKey *service.APIKey, models []string) bool {
	if !apiKey.KeyModelPolicyEnabled() {
		return true
	}
	message := "An explicit model is required by the API key model policy"
	if len(models) > 0 {
		message = ""
		for _, model := range models {
			if !apiKey.AllowsPublicModel(model) {
				message = fmt.Sprintf("Model %q is not available for this API key", model)
				break
			}
		}
	}
	if message == "" {
		return true
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalModelConfiguration)
	middleware2.MarkIngressRejected(c, middleware2.IngressRejectModelNotAllowed)
	c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "invalid_request_error", "message": message}})
	return false
}
