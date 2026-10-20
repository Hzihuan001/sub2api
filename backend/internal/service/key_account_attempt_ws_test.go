package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestKeyAccountWSNativeHTTPAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name         string
		frames       []string
		waitCancel   bool
		wantReplay   bool
		wantUsage    bool
		wantSuccess  bool
		prewarm      bool
		clientCancel bool
	}{
		{name: "success", frames: []string{`{"type":"response.output_text.delta","delta":"hello"}`, `{"type":"response.completed","response":{"id":"resp_test","model":"gpt-wire","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`}, wantSuccess: true, wantUsage: true},
		{name: "structured_503_without_http_status", frames: []string{`{"type":"error","error":{"code":"503","type":"server_error","message":"busy"}}`}, wantReplay: true},
		{name: "partial_usage_keeps_native_settlement", frames: []string{`{"type":"response.in_progress","response":{"id":"resp_test","usage":{"input_tokens":3}}}`, `{"type":"error","error":{"code":"503","message":"busy"}}`}, wantUsage: true},
		{name: "error_with_late_usage_tail_keeps_native_settlement", frames: []string{`{"type":"error","error":{"code":"503","message":"busy"}}` + "\n" + `{"type":"response.in_progress","response":{"id":"resp_test","usage":{"input_tokens":7}}}`}, wantUsage: true},
		{name: "unknown_prefix_veto", frames: []string{`{"type":"response.future_event","payload":{}}`, `{"type":"error","error":{"code":"503","message":"busy"}}`}},
		{name: "tool_veto", frames: []string{`{"type":"response.output_item.added","item":{"type":"function_call","name":"do_work","arguments":"{}"}}`, `{"type":"error","error":{"code":"503","message":"busy"}}`}},
		{name: "reasoning_veto", frames: []string{`{"type":"response.reasoning.delta","delta":"reasoning"}`, `{"type":"error","error":{"code":"503","message":"busy"}}`}},
		{name: "ttft_safe_prefix_cleanup", frames: []string{`{"type":"response.created","response":{"id":"resp_test","status":"in_progress","output":[]}}`}, waitCancel: true, wantReplay: true},
		{name: "ordinary_eof_is_not_proof", frames: []string{`{"type":"response.created","response":{"id":"resp_test","status":"in_progress","output":[]}}`}},
		{name: "completed_empty_is_not_proof", frames: []string{`{"type":"response.completed","response":{"id":"resp_test","status":"completed","output":[]}}`}, wantSuccess: true},
		{name: "prewarm_not_counted_or_timed", prewarm: true, frames: []string{`{"type":"response.completed","response":{"id":"resp_test","model":"gpt-wire","status":"completed","output":[]}}`}, wantSuccess: true},
		{name: "client_cancel_keeps_native_usage_drain", clientCancel: true, frames: []string{`{"type":"response.created","response":{"id":"resp_test","status":"in_progress","output":[]}}`}, wantSuccess: true, wantUsage: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			var sends atomic.Int32
			var admissions atomic.Int32
			var prewarms atomic.Int32
			var premature atomic.Bool
			var wireModel atomic.Value
			serverDone := make(chan struct{})
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(serverDone)
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				var payload map[string]any
				if conn.ReadJSON(&payload) != nil {
					return
				}
				if generate, exists := payload["generate"]; exists && generate == false {
					prewarms.Add(1)
					if admissions.Load() != 0 {
						premature.Store(true)
					}
					time.Sleep(60 * time.Millisecond) // Longer than TTFT: no timer yet.
					if conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp_prewarm","output":[]}}`)) != nil {
						return
					}
					if conn.ReadJSON(&payload) != nil {
						return
					}
				}
				sends.Add(1)
				wireModel.Store(payload["model"])
				for _, frame := range tc.frames {
					if conn.WriteMessage(websocket.TextMessage, []byte(frame)) != nil {
						return
					}
				}
				if tc.clientCancel {
					cancelParent()
					if conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp_test","status":"completed","output":[],"usage":{"input_tokens":4}}}`)) != nil {
						return
					}
				}
				if tc.waitCancel {
					_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
					_, _, _ = conn.ReadMessage()
				} else if strings.Contains(tc.name, "503") || tc.wantUsage || strings.Contains(tc.name, "veto") {
					// Reply to the native graceful close without introducing a sleep.
					_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
					_, _, _ = conn.ReadMessage()
				}
			}))
			defer server.Close()
			cfg := newOpenAIWSV2TestConfig()
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = tc.prewarm
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector()}
			defer svc.getOpenAIWSConnPool().Close()
			account := &Account{ID: 991, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2, Credentials: map[string]any{"api_key": "synthetic", "base_url": server.URL, "model_mapping": map[string]any{"gpt-public": "gpt-wire"}}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}
			var samples []keyrouting.AccountAttemptSample
			timeout := time.Duration(0)
			if tc.waitCancel || tc.prewarm {
				timeout = 35 * time.Millisecond
			}
			ctx, scope := WithKeyAccountForwardScope(parent, KeyAccountForwardOptions{Metadata: keyrouting.AccountAttemptMetadata{Site: "synthetic", APIKeyID: 1, GroupID: 2, Model: "gpt-public", Endpoint: "/v1/responses", PolicyRevision: 3}, FirstContentTimeout: timeout, ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
				samples = append(samples, sample)
				return nil
			}, BeforeDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) error { admissions.Add(1); return nil }})
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			c.Request.Header.Set("User-Agent", "synthetic-test/1.0")
			result, err := svc.Forward(ctx, c, account, []byte(`{"model":"gpt-public","stream":false,"input":"hello"}`))
			if tc.wantSuccess {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, int32(1), sends.Load())
			require.Equal(t, int32(1), admissions.Load())
			require.False(t, premature.Load())
			if tc.prewarm {
				require.Equal(t, int32(1), prewarms.Load())
			}
			require.Equal(t, "gpt-wire", wireModel.Load())
			if tc.wantUsage {
				require.True(t, KeyAccountForwardResultHasUsage(result), "native billing result must survive")
				require.Equal(t, "gpt-wire", result.BillingModel, "partial result keeps native mapped billing identity")
			}
			terminal := keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure}
			if err == nil {
				terminal.Outcome = keyrouting.OutcomeSuccess
			}
			_, finishErr := scope.Finish(result, terminal)
			require.NoError(t, finishErr)
			require.True(t, scope.CleanupAck())
			require.Equal(t, tc.wantReplay, scope.CanStartNext())
			require.Len(t, samples, 1)
			require.NoError(t, uuid.Validate(samples[0].Metadata.AttemptID))
			require.Equal(t, "gpt-wire", samples[0].Metadata.UpstreamModel)
			require.Equal(t, "/v1/responses", samples[0].Metadata.UpstreamEndpoint)
			require.Zero(t, samples[0].StatusCode, "WS must never manufacture an HTTP upstream status")
			require.Equal(t, keyrouting.AttemptStatusNone, samples[0].StatusSource)
			if tc.waitCancel {
				require.True(t, samples[0].VerifiedFirstContentCancellation)
				require.True(t, samples[0].FirstContentExpired)
			}
			if tc.clientCancel {
				require.Equal(t, keyrouting.OutcomeIgnored, samples[0].Outcome)
			}
			if tc.wantReplay && !tc.waitCancel {
				var failover *UpstreamFailoverError
				require.True(t, errors.As(err, &failover))
				require.True(t, samples[0].VerifiedFailure)
			}
			select {
			case <-serverDone:
			case <-time.After(3 * time.Second):
				t.Fatal("native WS reader/connection not cleaned up")
			}
		})
	}
}

func TestKeyAccountWSCleanupWaitNeverFakesAck(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	attempt, _, err := beginKeyAccountWSAttempt(ctx, account, "wss://synthetic.invalid/v1/responses", "wire-model")
	require.NoError(t, err)
	attempt.sent(nil)
	attempt.observe([]byte(`{"type":"error","error":{"code":"503","message":"busy"}}`))
	// Model a native reader that did not exit after lease release. A cleanup
	// timeout is not permission to replay, even with a verified transient error.
	lease := &openAIWSConnLease{conn: &openAIWSConn{readerLoopResults: make(chan []byte)}}
	started := time.Now()
	attempt.complete(lease, false, nil, errors.New("native error"), nil)
	require.Less(t, time.Since(started), time.Second)
	require.ErrorIs(t, attempt.cleanupErr, ErrKeyAccountBodyPending)
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.False(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountWSLateMeteringOverridesLocalCancellation(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{FirstContentTimeout: 5 * time.Millisecond})
	attempt, wireCtx, err := beginKeyAccountWSAttempt(ctx, account, "wss://synthetic.invalid/v1/responses", "wire-model")
	require.NoError(t, err)
	attempt.sent(nil)
	select {
	case <-wireCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("TTFT did not cancel this wire")
	}
	attempt.readFailed(context.Canceled)
	require.True(t, attempt.wire.firstContentCancellationVerified())
	usage := &OpenAIUsage{}
	attempt.nativeObserve = func(message []byte) { parseOpenAIWSResponseUsageFromCompletedEvent(message, usage) }
	attempt.nativeResult = func() *OpenAIForwardResult { return &OpenAIForwardResult{Usage: *usage} }
	frames := make(chan []byte, 1)
	frames <- []byte(`{"type":"response.in_progress","response":{"usage":{"input_tokens":9}}}`)
	close(frames)
	result := attempt.complete(&openAIWSConnLease{conn: &openAIWSConn{readerLoopResults: frames}}, false, nil, context.Canceled, nil)
	require.NotNil(t, result)
	require.Equal(t, 9, result.Usage.InputTokens)
	sample, err := scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.True(t, scope.CleanupAck())
	require.True(t, sample.FirstContentExpired)
	require.True(t, sample.UsageKnown)
	require.False(t, sample.VerifiedFirstContentCancellation)
	require.False(t, scope.CanStartNext(), "late metering must veto duplicate billable inference")
}
