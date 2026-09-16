package handler

import (
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSecurityAuditRequestUsesUsageRequestType(t *testing.T) {
	for _, raw := range []int16{1, 2, 3, 4, 5} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		setOpsEndpointContext(c, "", raw)
		request := buildSecurityAuditRequest(c, nil, middleware2.AuthSubject{UserID: 7},
			"openai_responses", "test-model", []byte(`{"input":"hello"}`), "http")
		require.Equal(t, service.RequestTypeFromInt16(raw).String(), request.RequestType)
	}
}
