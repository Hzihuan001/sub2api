package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type firstContentTestSelector struct {
	*dispatchTestSelector
	latencies []time.Duration
}

func (s *firstContentTestSelector) FirstContentTimeout(context.Context, *service.APIKey, service.KeyRoutingRequest, int64) (time.Duration, error) {
	return 20 * time.Millisecond, nil
}
func (s *firstContentTestSelector) RecordFirstContent(_ context.Context, _ *service.APIKey, _ service.KeyRoutingRequest, _ int64, elapsed time.Duration) (keyrouting.RouteFailureResult, error) {
	s.latencies = append(s.latencies, elapsed)
	return keyrouting.RouteFailureResult{}, nil
}

func TestFirstContentDispatchWaitsForCleanupAndDiscardsPrelude(t *testing.T) {
	c, base := newDispatchTestContext(t)
	selector := &firstContentTestSelector{dispatchTestSelector: base}
	c.Set(keyRoutingSelectorContextKey, selector)
	var groups []int64
	cleaned := false
	KeyRouteDispatch(func(c *gin.Context) {
		key, _ := servermiddleware.GetAPIKeyFromContext(c)
		groups = append(groups, *key.GroupID)
		guard := keyrouting.GetFirstContentGuard(c.Request.Context())
		require.NotNil(t, guard)
		child := guard.Detach(c.Request.Context())
		if *key.GroupID == 1 {
			defer func() { cleaned = true }()
			c.Header("Content-Type", "text/event-stream")
			_, _ = c.Writer.Write([]byte("data: {\"type\":\"response.created\"}\n\n: ping\n\n"))
			c.Writer.Flush()
			require.False(t, c.Writer.Written())
			select {
			case <-child.Done():
			case <-time.After(time.Second):
				t.Fatal("guard did not cancel")
			}
			require.NoError(t, c.Request.Context().Err(), "root must stay live")
			return
		}
		require.True(t, cleaned)
		c.JSON(http.StatusOK, gin.H{"backup": true})
	})(c)
	require.Equal(t, []int64{1, 2}, groups)
	require.Equal(t, http.StatusOK, c.Writer.Status())
	require.Contains(t, c.Writer.Header().Get("Content-Type"), "application/json")
	require.Len(t, selector.latencies, 2)
	events, ok := c.MustGet(service.OpsUpstreamErrorsKey).([]*service.OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "first_content_timeout", events[0].Reason)
	require.Zero(t, events[0].UpstreamStatusCode, "Key-local deadline is not a provider 504")
	require.True(t, events[0].SkipMonitoring)
	require.NotEmpty(t, service.OpsRouteSwitchLabel(c.Request.Context()))
}

func TestFirstContentDispatchKnownUsageAndPartialContentPreventReplay(t *testing.T) {
	for _, frame := range []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":1}}}`,
		`{"choices":[{"delta":{"content":"partial"}}]}`,
		`{"type":"content_block_start","content_block":{"type":"tool_use","id":"tool"}}`,
	} {
		t.Run(frame, func(t *testing.T) {
			c, base := newDispatchTestContext(t)
			selector := &firstContentTestSelector{dispatchTestSelector: base}
			c.Set(keyRoutingSelectorContextKey, selector)
			calls := 0
			KeyRouteDispatch(func(c *gin.Context) {
				calls++
				guard := keyrouting.GetFirstContentGuard(c.Request.Context())
				child := guard.Detach(c.Request.Context())
				c.Header("Content-Type", "text/event-stream")
				_, _ = c.Writer.Write([]byte("data: " + frame + "\n\n"))
				c.Writer.Flush()
				select {
				case <-child.Done():
					t.Fatal("billable output was canceled")
				case <-time.After(35 * time.Millisecond):
				}
			})(c)
			require.Equal(t, 1, calls)
			require.True(t, c.Writer.Written())
		})
	}
}

func TestFirstContentDispatchLastEligibleGroupNotCutShort(t *testing.T) {
	c, base := newDispatchTestContext(t)
	base.next = nil
	selector := &firstContentTestSelector{dispatchTestSelector: base}
	c.Set(keyRoutingSelectorContextKey, selector)
	KeyRouteDispatch(func(c *gin.Context) {
		guard := keyrouting.GetFirstContentGuard(c.Request.Context())
		child := guard.Detach(c.Request.Context())
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.Write([]byte(": ping\n\n"))
		c.Writer.Flush()
		require.True(t, c.Writer.Written(), "last group preserves heartbeats")
		select {
		case <-child.Done():
			t.Fatal("last group truncated")
		case <-time.After(35 * time.Millisecond):
		}
		_, _ = c.Writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
	})(c)
	require.False(t, c.GetBool("custom.key_route_restart"))
	require.Len(t, selector.latencies, 1)
}

func TestFirstContentFrameSemantics(t *testing.T) {
	for _, frame := range []string{": heartbeat", `data: {"type":"response.created"}`, `data: {"choices":[{"delta":{"role":"assistant"}}]}`, `data: {"type":"content_block_start","content_block":{"type":"text","text":""}}`, `data: {"type":"response.failed","error":{"message":"fail"}}`, `data: {"type":"message_start","message":{"usage":{"input_tokens":10}}}`, `data: unknown`, `data: [DONE]`} {
		require.False(t, firstContentFrame([]byte(frame)), frame)
	}
	for _, frame := range []string{`data: {"type":"response.output_text.delta","delta":"text"}`, `data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"thought"}}`, `data: {"type":"response.output_item.added","item":{"type":"function_call"}}`} {
		require.True(t, firstContentFrame([]byte(frame)), frame)
	}
}

func TestFirstContentWriterDisarmedGuardPreservesNativeHeartbeats(t *testing.T) {
	for _, flushOnly := range []bool{false, true} {
		c, _ := newDispatchTestContext(t)
		guard := keyrouting.NewFirstContentGuard(time.Hour)
		defer guard.Finish()
		guard.Detach(c.Request.Context())
		w := newFirstContentWriter(c.Writer, guard, true)
		w.Header().Set("Content-Type", "text/event-stream")
		if flushOnly {
			_, err := w.Write([]byte(": ping\n\n"))
			require.NoError(t, err)
			require.False(t, w.Written())
		}
		guard.PreventReplay() // provider tool injection or hidden raw usage
		if flushOnly {
			w.Flush()
		} else {
			_, err := w.Write([]byte(": ping\n\n"))
			require.NoError(t, err)
		}
		require.True(t, w.Written())
		_, observed := guard.FirstContentElapsed()
		require.False(t, observed, "a heartbeat is not useful content")
	}
}

func TestFirstContentWriterUsageCommitsWithoutPretendingToBeContent(t *testing.T) {
	for _, prefix := range []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
		`{"type":"response.created","response":{"usage":{"input_tokens":10}}}`,
		`{"choices":[],"usage":{"prompt_tokens":10}}`,
	} {
		t.Run(prefix, func(t *testing.T) {
			c, _ := newDispatchTestContext(t)
			guard := keyrouting.NewFirstContentGuard(time.Hour)
			defer guard.Finish()
			child := guard.Detach(c.Request.Context())
			w := newFirstContentWriter(c.Writer, guard, true)
			w.Header().Set("Content-Type", "text/event-stream")
			_, err := w.Write([]byte("data: " + prefix + "\n\n"))
			require.NoError(t, err)
			require.True(t, w.Written(), "usage must not be discarded")
			require.True(t, guard.ReplayBlocked())
			_, observed := guard.FirstContentElapsed()
			require.False(t, observed, "input-token metadata is not the first useful content")
			time.Sleep(20 * time.Millisecond)
			_, err = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"actual content\"}\n\n"))
			require.NoError(t, err)
			elapsed, observed := guard.FirstContentElapsed()
			require.True(t, observed)
			require.GreaterOrEqual(t, elapsed, 20*time.Millisecond)
			require.NoError(t, child.Err())
		})
	}
}

func TestFirstContentWriterErrorsAndSafetyCommitsDoNotObserveTTFT(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{name: "HTTP error", status: http.StatusBadRequest, body: `{"error":"invalid input"}`},
		{name: "unknown SSE", status: http.StatusOK, body: "data: unknown extension\n\n"},
		{name: "terminal-only SSE", status: http.StatusOK, body: "data: [DONE]\n\n"},
		{name: "bounded prelude", status: http.StatusOK, body: strings.Repeat(":", firstContentPreludeLimit+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newDispatchTestContext(t)
			guard := keyrouting.NewFirstContentGuard(time.Hour)
			defer guard.Finish()
			guard.Detach(c.Request.Context())
			w := newFirstContentWriter(c.Writer, guard, true)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(tc.status)
			_, err := w.Write([]byte(tc.body))
			require.NoError(t, err)
			_, observed := guard.FirstContentElapsed()
			require.False(t, observed)
			w.Finalize(false)
			require.True(t, w.Written(), "normal finalization must still return the native result")
		})
	}
}

func TestFirstContentWriterBoundedPreludeAndSplitFrames(t *testing.T) {
	c, _ := newDispatchTestContext(t)
	guard := keyrouting.NewFirstContentGuard(time.Second)
	guard.Detach(c.Request.Context())
	defer guard.Finish()
	w := newFirstContentWriter(c.Writer, guard, true)
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\","))
	require.False(t, w.Written())
	_, _ = w.Write([]byte("\"delta\":\"ok\"}\r\n\r\n"))
	require.True(t, w.Written())
	c2, _ := newDispatchTestContext(t)
	g2 := keyrouting.NewFirstContentGuard(time.Second)
	defer g2.Finish()
	w2 := newFirstContentWriter(c2.Writer, g2, true)
	w2.Header().Set("Content-Type", "text/event-stream")
	_, _ = w2.Write([]byte(strings.Repeat(":", firstContentPreludeLimit+1)))
	require.True(t, w2.Written())
	require.False(t, g2.ReplaySafe())
}
