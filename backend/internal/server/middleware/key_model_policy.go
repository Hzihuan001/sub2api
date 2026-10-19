package middleware

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Model restrictions concern new invocations, not access to resources the key
// already owns. Keep this exemption narrow: an unrecognized POST must not gain
// a model-free bypass when a new generation endpoint is added upstream.
func keyModelPolicyRequiresModel(c *gin.Context) bool {
	if c.Request == nil {
		return false
	}
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		path := c.FullPath()
		return path != "/v1/images/batches/:id/cancel" && path != "/images/batches/:id/cancel"
	default:
		return false
	}
}

// These sessions can change model after admission without passing through the
// Responses frame guard. Deny only restricted keys until their frame semantics
// can be proved, rather than treating an initial query/SDP model as protection.
func keyModelPolicyUnverifiableSession(c *gin.Context) bool {
	path := c.FullPath()
	if requestmodel.IsLiveRequestRoute(path) || strings.HasSuffix(path, "/realtime") {
		return true
	}
	return strings.EqualFold(c.GetHeader("Upgrade"), "websocket") && !isResponsesWebSocketRoute(c)
}

func rejectKeyModelPolicy(c *gin.Context, message string) {
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalModelConfiguration)
	MarkIngressRejected(c, IngressRejectModelNotAllowed)
	groupModelAllowlistErrorWriter(c)(c, http.StatusNotFound, message)
	c.Abort()
}
