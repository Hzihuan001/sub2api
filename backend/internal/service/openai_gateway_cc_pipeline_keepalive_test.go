//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// notifyResponseWriter lets the test observe a downstream write without
// peeking at ResponseRecorder while the heartbeat goroutine is still writing.
type notifyResponseWriter struct {
	gin.ResponseWriter
	writes chan []byte
}

func (w *notifyResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if n > 0 && w.writes != nil {
		copyOfWrite := append([]byte(nil), p[:n]...)
		select {
		case w.writes <- copyOfWrite:
		default:
		}
	}
	return n, err
}

func TestScanCCStreamWithKeepaliveWritesWhileUpstreamIsIdle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	writes := make(chan []byte, 8)
	c.Writer = &notifyResponseWriter{ResponseWriter: c.Writer, writes: writes}

	upstreamReader, upstreamWriter := io.Pipe()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       upstreamReader,
	}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{StreamKeepaliveInterval: 1}},
	}

	resultCh := make(chan ccStreamScanState, 1)
	go func() {
		resultCh <- svc.scanCCStreamWithKeepalive(
			c, resp, "cc fallback keepalive test", "fallback-request", time.Now(), nil,
		)
	}()

	select {
	case write := <-writes:
		require.Contains(t, string(write), ": keepalive\n\n")
		require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
		require.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
	case <-time.After(2 * time.Second):
		t.Fatal("fallback scanner did not emit an idle SSE heartbeat")
	}

	_, err := io.WriteString(upstreamWriter, "data: {\"id\":\"chatcmpl_keepalive\",\"choices\":[]}\n\n")
	require.NoError(t, err)
	_, err = io.WriteString(upstreamWriter, "data: [DONE]\n\n")
	require.NoError(t, err)
	require.NoError(t, upstreamWriter.Close())

	select {
	case result := <-resultCh:
		require.True(t, result.SawDone)
	case <-time.After(time.Second):
		t.Fatal("fallback scanner did not finish after upstream [DONE]")
	}
	require.Contains(t, recorder.Body.String(), ": keepalive\n\n")
}
