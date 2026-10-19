package service

import "github.com/gin-gonic/gin"

// RecordKeyRouteFirstContentTimeout reuses the existing error log, not usage
// records or account-health counters. A Key-local deadline is not provider 504.
func RecordKeyRouteFirstContentTimeout(c *gin.Context, platform, model string) {
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform: platform, RequestedModel: model,
		Kind: "key_route_timeout", Stage: "routing", Scope: "api_key",
		Reason: "first_content_timeout", Message: "Key first-content wait expired before usable output",
		SkipMonitoring: true,
	})
}
