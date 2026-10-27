//go:build unit

package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func batchImageBindTestRouter(t *testing.T, limit int64) (*gin.Engine, *observer.ObservedLogs) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log, logs := newObservedLogger(t)
	router := gin.New()
	if limit > 0 {
		router.Use(middleware.RequestBodyLimit(limit))
	}
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), log.With(zap.String("request_id", "batch-bind-test"))))
	})
	// A successfully bound request reaches the authorization check and returns
	// 401. Nil services prove binding failures never touch storage or billing.
	router.POST("/v1/images/batches", NewBatchImageHandler(nil, nil, nil).Submit)
	return router, logs
}

func batchImageBindTestSubmit(t *testing.T, router *gin.Engine, payload []byte, wantStatus int, wantCode string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/batches", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	require.NotPanics(t, func() { router.ServeHTTP(res, req) })
	require.Equal(t, wantStatus, res.Code, res.Body.String())
	var response struct {
		Error struct{ Code string } `json:"error"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
	require.Equal(t, wantCode, response.Error.Code)
	require.NotContains(t, res.Body.String(), "private-prompt-canary")
}

func TestBatchImageSubmitBindingSharedReferencesAvoidRepeatedUpload(t *testing.T) {
	refs := make([]any, 16)
	for i := range refs {
		refs[i] = map[string]any{
			"id": fmt.Sprintf("ref-%02d.png", i), "mime_type": "image/png",
			"data": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{byte(i)}, 256)),
		}
	}
	items := []any{
		map[string]any{"custom_id": "one", "prompt": "private-prompt-canary"},
		map[string]any{"custom_id": "two", "prompt": "another prompt", "output_count": 2},
	}
	shared := map[string]any{
		"model": "gpt-image-2.5-sunburst", "provider": "app_managed", "image_size": "3840x2160",
		"shared_reference_images": refs, "items": items,
	}
	sharedPayload, err := json.Marshal(shared)
	require.NoError(t, err)
	delete(shared, "shared_reference_images")
	for _, item := range items {
		item.(map[string]any)["reference_images"] = refs
	}
	repeatedPayload, err := json.Marshal(shared)
	require.NoError(t, err)
	require.Greater(t, len(repeatedPayload), len(sharedPayload))
	limit := int64(len(sharedPayload))

	router, logs := batchImageBindTestRouter(t, limit)
	batchImageBindTestSubmit(t, router, sharedPayload, http.StatusUnauthorized, "API_KEY_REQUIRED")
	require.Empty(t, logs.All())
	batchImageBindTestSubmit(t, router, repeatedPayload, http.StatusRequestEntityTooLarge, "BATCH_IMAGE_REQUEST_TOO_LARGE")
	entries := logs.All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.Equal(t, "max_bytes", fields["error_kind"])
	require.EqualValues(t, limit, fields["limit_bytes"])
	require.EqualValues(t, len(repeatedPayload), fields["content_length"])
	require.Equal(t, "batch-bind-test", fields["request_id"])
	require.NotContains(t, fmt.Sprint(fields), "private-prompt-canary")
	require.NotContains(t, fmt.Sprint(fields), refs[0].(map[string]any)["data"])
}

func TestBatchImageSubmitBindingErrorsAreSpecificAndPayloadFree(t *testing.T) {
	for _, tc := range []struct {
		name, payload, code, kind string
	}{
		{"syntax", `{"model":"private-prompt-canary",!}`, "BATCH_IMAGE_INVALID_JSON", "json_syntax"},
		{"empty", "", "BATCH_IMAGE_INVALID_JSON", "incomplete_json"},
		{"truncated", `{"items":[{"prompt":"private-prompt-canary"`, "BATCH_IMAGE_INVALID_JSON", "incomplete_json"},
		{"wrong_items_type", `{"items":"private-prompt-canary"}`, "BATCH_IMAGE_INVALID_REQUEST", "json_type"},
		{"wrong_count_type", `{"items":[{"prompt":"private-prompt-canary","output_count":"two"}]}`, "BATCH_IMAGE_INVALID_REQUEST", "json_type"},
		{"reference_data_url", `{"items":[{"prompt":"private-prompt-canary","reference_images":[{"mime_type":"image/png","data":"data:image/png;base64,abcd"}]}]}`, "BATCH_IMAGE_INVALID_REFERENCE_IMAGE", "reference_base64"},
		{"shared_reference_base64", `{"items":[{"prompt":"private-prompt-canary"}],"shared_reference_images":[{"mime_type":"image/png","data":"private-prompt-canary"}]}`, "BATCH_IMAGE_INVALID_REFERENCE_IMAGE", "reference_base64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, logs := batchImageBindTestRouter(t, 0)
			batchImageBindTestSubmit(t, router, []byte(tc.payload), http.StatusBadRequest, tc.code)
			entries := logs.All()
			require.Len(t, entries, 1)
			require.Equal(t, "batch image request binding failed", entries[0].Message)
			fields := entries[0].ContextMap()
			require.Equal(t, tc.kind, fields["error_kind"])
			require.NotContains(t, fmt.Sprint(fields), "private-prompt-canary")
			require.NotContains(t, fields, "error")
			require.NotContains(t, fields, "body")
			require.NotContains(t, fields, "body_head")
		})
	}
}

func TestBatchImageSubmitBindingBodyLimitWithoutContentLength(t *testing.T) {
	router, logs := batchImageBindTestRouter(t, 64)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/batches", strings.NewReader(`{"model":"gpt-image-2","items":[{"prompt":"`+strings.Repeat("x", 100)+`"}]}`))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, res.Code)
	require.Contains(t, res.Body.String(), "BATCH_IMAGE_REQUEST_TOO_LARGE")
	require.Contains(t, res.Body.String(), "split the batch")
	require.Len(t, logs.All(), 1)
	require.EqualValues(t, -1, logs.All()[0].ContextMap()["content_length"])
}
