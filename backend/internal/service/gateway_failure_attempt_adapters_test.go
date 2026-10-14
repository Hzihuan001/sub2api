package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise the real relay and service executor, not just the state object's
// methods. Passthrough keeps .5's acknowledged-turn boundary; no claim is made
// here about missing response.created or uncorrelated late cross-turn frames.
func TestGatewayFailureAttemptPassthroughCountsPairedFramesAndNextAcknowledgedTurn(t *testing.T) {
	t.Run("response_created_boundary", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(context.Canceled)
		upstream := newStagedPassthroughConn()
		cfg := passthroughLifecycleConfig()
		svc := newPassthroughLifecycleService(cfg, upstream)
		account := passthroughLifecycleAccount()
		repo := &openAIWSIngressOverloadRepo{}
		limiter := NewRateLimitService(repo, nil, cfg, nil, nil)
		svc.accountRepo, svc.rateLimitService = repo, limiter
		limiter.SetAccountRuntimeBlocker(svc)
		observed := make(chan bool, 3)
		hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, _ *OpenAIForwardResult, _ error) {
			observed <- limiter.IsAccountOverloadCooling(account.ID, time.Now())
		}}
		server, serverErr := startPassthroughHookRecordingServer(t, ctx, svc, account, hooks)
		defer server.Close()
		client := dialPassthroughLifecycleClient(t, server)
		defer func() { _ = client.CloseNow() }()
		for turn := 1; turn <= 3; turn++ {
			if turn > 1 {
				writeCtx, cancelWrite := context.WithTimeout(ctx, time.Second)
				err := client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1"}`))
				cancelWrite()
				require.NoError(t, err)
			}
			require.Equal(t, "response.create", gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream, time.Second), "type").String())
			upstream.Send(fmt.Sprintf(`{"type":"response.created","response":{"id":"r%d"}}`, turn))
			_, err := readPassthroughLifecycleFrame(t, client, time.Second)
			require.NoError(t, err)
			for i := 0; i < 2; i++ {
				upstream.Send(`{"type":"error","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`)
			}
			upstream.Send(fmt.Sprintf(`{"type":"response.failed","response":{"id":"r%d","status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`, turn))
			for i := 0; i < 3; i++ {
				payload, err := readPassthroughLifecycleFrame(t, client, time.Second)
				require.NoError(t, err)
				if i == 2 {
					require.Equal(t, "response.failed", gjson.GetBytes(payload, "type").String())
				}
			}
			select {
			case cooling := <-observed:
				require.Equal(t, turn == 3, cooling, "paired frames count once, every acknowledged turn counts")
			case <-time.After(time.Second):
				t.Fatal("turn did not complete")
			}
		}
		require.Equal(t, int32(1), repo.overloadCalls.Load())
		upstream.Fail(io.EOF)
		_ = client.Close(coderws.StatusNormalClosure, "done")
		select {
		case err := <-serverErr:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("relay did not stop")
		}
	})
}

func TestGatewayFailureAttemptHTTPBridgeCountsOneFailurePerSend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIWSV2TestConfig()
	repo := &openAIWSIngressOverloadRepo{}
	limiter := NewRateLimitService(repo, nil, cfg, nil, nil)
	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, rateLimitService: limiter, httpUpstream: upstream}
	limiter.SetAccountRuntimeBlocker(svc)
	account := &Account{ID: 5912, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test"}}
	payload := []byte(`{"type":"response.create","model":"gpt-5.1","input":"hi"}`)
	for turn := 1; turn <= 3; turn++ {
		body := strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"partial"}`,
			``,
			`data: {"type":"error","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`,
			``,
			`data: {"type":"error","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`,
			``,
			`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`,
			``,
		}, "\n")
		upstream.resp = &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		writes := 0
		result, err := svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "sk-test", payload, len(payload),
			"gpt-5.1", "", "", "", "", turn, func([]byte) error { writes++; return nil })
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, "response.failed", result.UpstreamTerminalEvent)
		require.Equal(t, 2, writes, "bare error frames stay deferred to the authoritative failure")
		require.Equal(t, turn == 3, limiter.IsAccountOverloadCooling(account.ID, time.Now()))
	}
	require.Equal(t, int32(1), repo.overloadCalls.Load())
}

func TestGatewayFailureSemanticStatusAdaptersPreservePublishedPolicy(t *testing.T) {
	for _, tc := range []struct {
		body                      string
		wantSemantic, wantFailure int
	}{
		{`{"error":{"code":"invalid_api_key","status_code":403}}`, 401, 401},
		{`{"error":{"code":"workspace_suspended"}}`, 403, 403},
		{`{"error":{"code":"insufficient_quota"}}`, 402, 402},
		{`{"error":{"code":"invalid_parameter","status_code":503}}`, 400, 502},
		{`{"error":{"code":"model_not_found"}}`, 404, 404},
		{`{"error":{"type":"permission_error","status":403}}`, 403, 403},
		{`{"response":{"error":{"status":500,"code":"server_error"}}}`, 500, 500},
		{`{"error":{"status":429}}`, 429, 429},
		{`{`, 502, 502},
	} {
		payload := []byte(tc.body)
		message := extractOpenAISSEErrorMessage(payload)
		require.Equal(t, tc.wantSemantic, openAIStreamFailedEventSemanticStatus(payload, message), tc.body)
		require.Equal(t, tc.wantFailure, openAIStreamFailureStatus(payload, message), tc.body)
	}
}
