package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestKeyModelPolicyResponsesWebSocketFirstAndSubsequentAdmission(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeDedicated} {
		t.Run(mode, func(t *testing.T) {
			policy := &service.KeyModelPolicy{Enabled: true, Models: []string{"gpt-5.4"}}
			for _, payload := range []string{
				`{"type":"response.create","model":"gpt-4.1","stream":false}`,
				`{"type":"response.create","model":"gpt-5.4","Model":"gpt-4.1","stream":false}`,
			} {
				runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
					firstPayload: payload, keyModelPolicy: policy, ingressMode: mode,
					firstFrameCloseExpected: true, closeReason: "not available for this API key",
				})
			}
			runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload:   `{"type":"response.create","model":"gpt-5.4","stream":false}`,
				secondPayload:  `{"type":"response.create","model":"gpt-4.1","stream":false}`,
				keyModelPolicy: policy, ingressMode: mode,
				secondTurnCloseExpected: true, closeReason: "not available for this API key",
			})
		})
	}
}

func TestKeyModelPolicyResponsesWebSocketSessionUpdateRejectedBeforeUpstream(t *testing.T) {
	runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload:          `{"type":"response.create","model":"gpt-5.4","stream":false}`,
		midPayload:            `{"type":"session.update","model":"gpt-5.4","session":{"model":"gpt-4.1"}}`,
		keyModelPolicy:        &service.KeyModelPolicy{Enabled: true, Models: []string{"gpt-5.4"}},
		ingressMode:           service.OpenAIWSIngressModePassthrough,
		midFrameCloseExpected: true, closeReason: "not available for this API key",
	})
}

func TestKeyModelPolicyBatchSubmissionRejectedBeforeAuditPersistenceOrBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &BatchImageHandler{}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID: 9, UserID: 7,
			KeyModelPolicy: &service.KeyModelPolicy{Enabled: true, Models: []string{"allowed-image"}},
		})
	})
	router.POST("/v1/images/batches", h.Submit)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/batches", strings.NewReader(`{"model":"blocked-image","items":[{"custom_id":"one","prompt":"cat"}]}`))
	req.Header.Set("Content-Type", "application/json")
	require.NotPanics(t, func() { router.ServeHTTP(w, req) }, "nil services prove creation/audit are not reached")
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "API key")
}

func TestKeyModelPolicyAsyncCreationDeniedButExistingTaskReadable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	h := &AsyncImageHandler{tasks: tasks, execute: func(string, *gin.Context) { t.Error("denied task must not execute") }}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID: 9, UserID: 7,
			Group:          &service.Group{ID: 3, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
			KeyModelPolicy: &service.KeyModelPolicy{Enabled: true, Models: []string{"allowed-image"}},
		})
	})
	router.POST("/v1/images/generations/async", h.Submit)
	router.GET("/v1/images/tasks/:task_id", h.Get)
	for _, body := range []string{`{"model":"blocked-image","prompt":"cat"}`, `{"prompt":"cat"}`, `{"model":"allowed-image","model":"blocked-image","prompt":"cat"}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		require.Empty(t, store.tasks)
	}
	task, err := tasks.Create(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/images/tasks/"+task.ID, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestBatchImageModelDirectoryRespectsKeyPolicy(t *testing.T) {
	models := []service.BatchImagePublicModel{{ID: "allowed"}, {ID: "blocked"}, {ID: "Allowed"}}
	key := &service.APIKey{KeyModelPolicy: &service.KeyModelPolicy{Enabled: true, Models: []string{"allowed"}}}
	require.Equal(t, models[:1], filterBatchImageModelsByKeyPolicy(models, key))
	require.Len(t, models, 3, "filter must not overwrite the source directory")
	key.KeyModelPolicy.Enabled = false
	require.Equal(t, models, filterBatchImageModelsByKeyPolicy(models, key))
}
