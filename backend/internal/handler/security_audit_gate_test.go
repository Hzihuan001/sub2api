package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func batchAuditTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	c, recorder := securityAuditErrorTestContext(t)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/batches", nil)
	securityAuditMediaTestMiddleware(c)
	return c, recorder
}

func TestBatchImageAuditGateKeepsOnlyAuditDependency(t *testing.T) {
	field, ok := reflect.TypeOf(BatchImageHandler{}).FieldByName("auditGate")
	require.True(t, ok)
	require.Equal(t, reflect.Interface, field.Type.Kind())
	require.Equal(t, 1, field.Type.NumMethod())
	require.Equal(t, "Check", field.Type.Method(0).Name)
	for _, field := range reflect.VisibleFields(reflect.TypeOf(BatchImageHandler{})) {
		require.NotEqual(t, reflect.TypeOf((*OpenAIGatewayHandler)(nil)), field.Type)
	}
	engine := blockingHandlerPromptEngine()
	h := ProvideBatchImageHandler(nil, nil, nil, securityaudit.NewCoordinator(nil, engine), nil)
	c, recorder := batchAuditTestContext(t)
	require.False(t, h.checkSecurityAuditBeforeSubmit(c, &service.BatchImageSubmitRequest{
		Model: "gpt-image-2", Items: []service.BatchImageSubmitItem{{CustomID: "one", Prompt: "blocked"}},
	}))
	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestBatchImageAuditGateModesAndHTTPDeduplication(t *testing.T) {
	for _, tt := range []struct {
		name                string
		mode                securityaudit.Mode
		decision            *securityaudit.PromptDecision
		err                 error
		allow               bool
		evaluated, enqueued int
	}{
		{name: "off", mode: securityaudit.ModeOff, allow: true},
		{name: "capture_only", mode: securityaudit.ModeCaptureOnly, allow: true, enqueued: 1},
		{name: "capture_only_write_failure", mode: securityaudit.ModeCaptureOnly, err: errors.New("database unavailable"), allow: true, enqueued: 1},
		{name: "async", mode: securityaudit.ModeAsync, allow: true, enqueued: 1},
		{name: "async_queue_failure", mode: securityaudit.ModeAsync, err: securityaudit.ErrQueueFull, allow: true, enqueued: 1},
		{name: "blocking_allow", mode: securityaudit.ModeBlocking, decision: &securityaudit.PromptDecision{Kind: securityaudit.DecisionAllow, AllowNextStage: true}, allow: true, evaluated: 1},
		{name: "blocking_reject", mode: securityaudit.ModeBlocking, decision: &securityaudit.PromptDecision{Kind: securityaudit.DecisionBlock}, evaluated: 1},
		{name: "blocking_failure", mode: securityaudit.ModeBlocking, err: errors.New("guard unavailable"), evaluated: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			engine := &handlerPromptEngine{mode: tt.mode, decision: tt.decision, err: tt.err}
			h := NewBatchImageHandler(nil, nil, nil, NewSecurityAuditGate(securityaudit.NewCoordinator(nil, engine), nil))
			c, recorder := batchAuditTestContext(t)
			req := &service.BatchImageSubmitRequest{Model: "gpt-image-2", Items: []service.BatchImageSubmitItem{
				{CustomID: "one", Prompt: " first prompt "}, {CustomID: "two", Prompt: "second prompt"},
			}}
			require.Equal(t, tt.allow, h.checkSecurityAuditBeforeSubmit(c, req))
			if tt.allow {
				require.False(t, c.Writer.Written(), "allowed audit must not alter the response")
				require.True(t, h.checkSecurityAuditBeforeSubmit(c, req), "same HTTP request reuses the audit result")
			} else {
				require.GreaterOrEqual(t, recorder.Code, 400)
			}
			evaluated, enqueued, requests := engine.snapshot()
			require.Equal(t, tt.evaluated, evaluated)
			require.Equal(t, tt.enqueued, enqueued)
			if len(requests) > 0 {
				require.Equal(t, int64(7), requests[0].UserID)
				require.Equal(t, int64(9), requests[0].APIKeyID)
				require.Equal(t, "http", requests[0].Stage)
				require.Equal(t, service.ContentModerationProtocolOpenAIImages, requests[0].Protocol)
				require.JSONEq(t, `{"request":{"items":[{"prompt":"first prompt"},{"prompt":"second prompt"}]}}`, string(requests[0].Body))
			}
		})
	}
}

func TestBatchImageAuditGateMissingAndExplicitOff(t *testing.T) {
	t.Run("missing_wiring_is_not_off", func(t *testing.T) {
		c, recorder := batchAuditTestContext(t)
		h := NewBatchImageHandler(nil, nil, nil, nil)
		require.False(t, h.checkSecurityAuditBeforeSubmit(c, &service.BatchImageSubmitRequest{}))
		require.Equal(t, http.StatusInternalServerError, recorder.Code)
		require.Contains(t, recorder.Body.String(), "SECURITY_AUDIT_GATE_MISSING")
	})
	t.Run("typed_nil_is_not_off", func(t *testing.T) {
		c, recorder := batchAuditTestContext(t)
		var gate *coordinatorAuditGate
		h := NewBatchImageHandler(nil, nil, nil, gate)
		require.False(t, h.checkSecurityAuditBeforeSubmit(c, &service.BatchImageSubmitRequest{Items: []service.BatchImageSubmitItem{{Prompt: "hello"}}}))
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	})
	t.Run("missing_dependencies_are_not_off", func(t *testing.T) {
		c, recorder := batchAuditTestContext(t)
		h := NewBatchImageHandler(nil, nil, nil, NewSecurityAuditGate(nil, nil))
		require.False(t, h.checkSecurityAuditBeforeSubmit(c, &service.BatchImageSubmitRequest{Items: []service.BatchImageSubmitItem{{Prompt: "hello"}}}))
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		require.Contains(t, recorder.Body.String(), securityaudit.ErrorCodeUnavailable)
	})
	t.Run("explicit_off", func(t *testing.T) {
		c, _ := batchAuditTestContext(t)
		engine := &handlerPromptEngine{mode: securityaudit.ModeOff}
		h := NewBatchImageHandler(nil, nil, nil, NewSecurityAuditGate(securityaudit.NewCoordinator(nil, engine), nil))
		require.True(t, h.checkSecurityAuditBeforeSubmit(c, &service.BatchImageSubmitRequest{Items: []service.BatchImageSubmitItem{{Prompt: "hello"}}}))
		require.False(t, c.Writer.Written())
	})
}

func TestBatchImageAuditRunsAfterJSONAndOwnershipValidation(t *testing.T) {
	for _, tt := range []struct {
		name, body   string
		authenticate bool
		status       int
	}{
		{name: "invalid_json", body: `{`, authenticate: true, status: http.StatusBadRequest},
		{name: "missing_owner", body: `{"model":"gpt-image-2","items":[{"prompt":"hello"}]}`, status: http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			engine := blockingHandlerPromptEngine()
			h := ProvideBatchImageHandler(nil, nil, nil, securityaudit.NewCoordinator(nil, engine), nil)
			router := gin.New()
			if tt.authenticate {
				router.Use(securityAuditMediaTestMiddleware)
			}
			router.POST("/v1/images/batches", h.Submit)
			req := httptest.NewRequest(http.MethodPost, "/v1/images/batches", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			require.NotPanics(t, func() { router.ServeHTTP(recorder, req) })
			require.Equal(t, tt.status, recorder.Code)
			evaluated, enqueued, _ := engine.snapshot()
			require.Zero(t, evaluated)
			require.Zero(t, enqueued)
		})
	}
}

func TestSecurityAuditSharedOpenAIErrorPreservesLegacyJSONAndCompactSSE(t *testing.T) {
	decision := &securityaudit.Decision{Kind: securityaudit.DecisionBlock, HTTPStatus: http.StatusForbidden,
		ErrorCode: "content_policy_violation", Legacy: &securityaudit.LegacyDecision{Blocked: true, Message: "legacy exact message"}}
	t.Run("json", func(t *testing.T) {
		c, recorder := securityAuditErrorTestContext(t)
		writeOpenAISecurityAuditError(c, decision)
		require.Equal(t, http.StatusForbidden, recorder.Code)
		require.JSONEq(t, `{"error":{"type":"content_policy_violation","message":"legacy exact message"}}`, recorder.Body.String())
	})
	t.Run("committed_compact_sse", func(t *testing.T) {
		c, recorder := securityAuditErrorTestContext(t)
		service.MarkOpenAICompactClientStream(c)
		stop := service.StartOpenAICompactSSEKeepalive(c, time.Millisecond)
		defer stop()
		require.Eventually(t, c.Writer.Written, time.Second, time.Millisecond)
		writeOpenAISecurityAuditError(c, decision)
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed\n"))
		require.Contains(t, recorder.Body.String(), "legacy exact message")
		require.NotContains(t, recorder.Body.String(), `{"error":{"message"`)
	})
}
