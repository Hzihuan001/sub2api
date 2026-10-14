package handler

import (
	"net/http"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SecurityAuditGate is the deliberately narrow audit dependency used by media
// submission handlers. It cannot select accounts, reserve money or submit work.
// The full OpenAI gateway is not part of this contract.
type SecurityAuditGate interface {
	Check(c *gin.Context, reqLog *zap.Logger, apiKey *service.APIKey, subject middleware.AuthSubject, protocol, model string, body []byte) *securityaudit.Decision
}

// coordinatorAuditGate owns only the coordinator and legacy moderation adapter.
// Missing dependencies are a wiring error, not a disabled audit mode. Off mode
// must still come from the configured coordinator.
type coordinatorAuditGate struct {
	coordinator *securityaudit.Coordinator
	legacy      *service.ContentModerationService
}

// NewSecurityAuditGate constructs the media audit gate. The legacy adapter may
// be used without a coordinator, but an empty gate rejects the request.
func NewSecurityAuditGate(coordinator *securityaudit.Coordinator, legacy *service.ContentModerationService) SecurityAuditGate {
	return &coordinatorAuditGate{coordinator: coordinator, legacy: legacy}
}

func (g *coordinatorAuditGate) Check(c *gin.Context, reqLog *zap.Logger, apiKey *service.APIKey, subject middleware.AuthSubject, protocol, model string, body []byte) *securityaudit.Decision {
	if g == nil || (g.coordinator == nil && g.legacy == nil) {
		return &securityaudit.Decision{
			Kind: securityaudit.DecisionUnavailable, HTTPStatus: http.StatusServiceUnavailable,
			ErrorCode: securityaudit.ErrorCodeUnavailable, ClientMessage: "security audit gate is not configured",
			AllowNextStage: false,
		}
	}
	return runSecurityAudit(c, reqLog, g.coordinator, g.legacy, apiKey, subject, protocol, model, body, "http")
}

var errMissingSecurityAuditGate = infraerrors.New(http.StatusInternalServerError, "SECURITY_AUDIT_GATE_MISSING", "security audit gate is not configured")
