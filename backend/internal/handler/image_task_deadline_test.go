package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAsyncImageTaskDeadlineDistinguishesUnknownFromCompleteSuccess(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		body       string
		wantStatus string
	}{
		{name: "gateway wrote 502 on timeout", statusCode: http.StatusBadGateway, body: `{"error":{"message":"upstream request failed"}}`, wantStatus: service.ImageTaskStatusFailed},
		{name: "incomplete 200 at timeout", statusCode: http.StatusOK, body: `{"data":[`, wantStatus: service.ImageTaskStatusFailed},
		{name: "complete success at deadline", statusCode: http.StatusOK, body: `{"data":[{"url":"https://example.test/saved.png"}]}`, wantStatus: service.ImageTaskStatusCompleted},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
			tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
			owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
			task, err := tasks.Create(context.Background(), owner)
			require.NoError(t, err)
			binding, enabled := tasks.CaptureCompletionBinding()
			require.True(t, enabled)
			complete := tasks.CompletionWriter(task, owner, "", "", binding)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			body := []byte(`{"model":"gpt-image-2","prompt":"cat"}`)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(string(body)))
			taskCtx, recorder, cancel, err := newAsyncImageContext(c, body, 20*time.Millisecond)
			require.NoError(t, err)
			h := &AsyncImageHandler{tasks: tasks}
			h.execute = func(_ string, output *gin.Context) {
				<-output.Request.Context().Done()
				output.Data(test.statusCode, "application/json", []byte(test.body))
			}
			h.run(task.ID, service.PlatformOpenAI, taskCtx, recorder, cancel, complete)
			got, err := tasks.Get(context.Background(), owner, task.ID)
			require.NoError(t, err)
			require.Equal(t, test.wantStatus, got.Status)
			if test.wantStatus == service.ImageTaskStatusCompleted {
				require.JSONEq(t, test.body, string(got.Result))
			} else {
				var taskError struct {
					Code string `json:"code"`
				}
				require.NoError(t, json.Unmarshal(got.Error, &taskError))
				require.Equal(t, "image_task_result_unknown", taskError.Code)
				require.Equal(t, http.StatusGatewayTimeout, got.HTTPStatus)
			}
		})
	}
}
