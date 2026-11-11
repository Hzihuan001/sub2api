package handler

import (
	"context"
	"encoding/json"
	"fmt"
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

const imageContentTestTaskID = "imgtask_0123456789abcdef0123456789abcdef"

func imageContentTestRouter(t *testing.T, externalResult bool) (*gin.Engine, []byte, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	data := []byte("the complete immutable image bytes")
	storage := service.NewLocalImageStorage(t.TempDir())
	imageURL, err := storage.Save(context.Background(), imageContentTestTaskID+"-0.png", "image/png", data)
	require.NoError(t, err)
	resultURL := imageURL
	if externalResult {
		resultURL = "https://external.example/private/generated.png"
	}
	result, err := json.Marshal(map[string]any{"data": []map[string]string{{"url": resultURL}}})
	require.NoError(t, err)
	store := &asyncImageMemoryStore{tasks: map[string]*service.ImageTaskRecord{
		imageContentTestTaskID: {
			ID: imageContentTestTaskID, UserID: 7, APIKeyID: 9, Status: service.ImageTaskStatusCompleted,
			Result: result, CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
		},
	}}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, 0, 0)
	tasks.SetLocalStorage(storage)
	h := &AsyncImageHandler{tasks: tasks}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		key := &service.APIKey{ID: 9, UserID: 7}
		switch c.GetHeader("X-Test-Owner") {
		case "other-user":
			key.UserID = 8
		case "other-key":
			key.ID = 10
		case "anonymous":
			c.Next()
			return
		}
		c.Set(string(middleware2.ContextKeyAPIKey), key)
		c.Next()
	})
	router.GET("/v1/images/tasks/:task_id/images/:filename", h.GetImage)
	return router, data, imageURL
}

func TestAsyncImageHandlerGetImageFullAndRangeReconnect(t *testing.T) {
	router, data, imageURL := imageContentTestRouter(t, false)
	full := httptest.NewRecorder()
	router.ServeHTTP(full, httptest.NewRequest(http.MethodGet, imageURL, nil))
	require.Equal(t, http.StatusOK, full.Code)
	require.Equal(t, data, full.Body.Bytes())
	require.Equal(t, "image/png", full.Header().Get("Content-Type"))
	require.Equal(t, "private, no-store", full.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", full.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "bytes", full.Header().Get("Accept-Ranges"))

	var reconnected []byte
	for _, part := range []struct {
		rangeHeader string
		start       int
		end         int
	}{
		{rangeHeader: "bytes=0-8", start: 0, end: 8},
		{rangeHeader: "bytes=9-", start: 9, end: len(data) - 1},
	} {
		req := httptest.NewRequest(http.MethodGet, imageURL, nil)
		req.Header.Set("Range", part.rangeHeader)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusPartialContent, rec.Code)
		require.Equal(t, fmt.Sprintf("bytes %d-%d/%d", part.start, part.end, len(data)), rec.Header().Get("Content-Range"))
		require.Equal(t, data[part.start:part.end+1], rec.Body.Bytes())
		reconnected = append(reconnected, rec.Body.Bytes()...)
	}
	require.Equal(t, data, reconnected, "a reconnect retrieves the saved remaining bytes without generation")

	req := httptest.NewRequest(http.MethodGet, imageURL, nil)
	req.Header.Set("Range", "bytes=99999-")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusRequestedRangeNotSatisfiable, rec.Code)
	require.Equal(t, fmt.Sprintf("bytes */%d", len(data)), rec.Header().Get("Content-Range"))
}

func TestAsyncImageHandlerGetImageRejectsOtherOwnersKeysAndInvalidPaths(t *testing.T) {
	router, data, imageURL := imageContentTestRouter(t, false)
	for _, owner := range []string{"other-user", "other-key", "anonymous"} {
		t.Run(owner, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, imageURL, nil)
			req.Header.Set("X-Test-Owner", owner)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			wantStatus := http.StatusNotFound
			if owner == "anonymous" {
				wantStatus = http.StatusForbidden
			}
			require.Equal(t, wantStatus, rec.Code)
			require.NotContains(t, rec.Body.String(), string(data))
		})
	}
	for _, path := range []string{
		strings.Replace(imageURL, "0.png", "1.png", 1),
		strings.Replace(imageURL, "0.png", "..%5c0.png", 1),
		strings.Replace(imageURL, imageContentTestTaskID, "imgtask_ffffffffffffffffffffffffffffffff", 1),
		imageURL + "/extra",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusNotFound, rec.Code, "path=%s", path)
		require.NotContains(t, rec.Body.String(), string(data))
	}
}

func TestAsyncImageHandlerGetImageDoesNotExposeExternalResultThroughLocalRoute(t *testing.T) {
	router, data, imageURL := imageContentTestRouter(t, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, imageURL, nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.NotContains(t, rec.Body.String(), string(data))
	require.NotContains(t, rec.Body.String(), "external.example")
	require.Empty(t, rec.Header().Get("Location"), "external results must not be redirected through a local image route")
}
