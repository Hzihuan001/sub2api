package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIWSIngressOverloadRepo struct {
	openAIWSIngressCapacityShedRepo
	overloadCalls atomic.Int32
}

func (r *openAIWSIngressOverloadRepo) SetOverloaded(context.Context, int64, time.Time) error {
	r.overloadCalls.Add(1)
	return nil
}

func TestProxyResponsesWebSocketFromClient_CountsOverloadOncePerUpstreamTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		bareErrors int
		generic    bool
	}{
		{name: "error_and_response_failed", bareErrors: 1},
		{name: "repeated_error_and_response_failed", bareErrors: 2},
		{name: "response_failed_only"},
		{name: "server_error_and_response_failed", bareErrors: 1, generic: true},
		{name: "repeated_server_error_and_response_failed", bareErrors: 2, generic: true},
		{name: "server_response_failed_only", generic: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newOpenAIWSV2TestConfig()
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModeCtxPool
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0

			var events [][]byte
			code, message := "server_is_overloaded", "Our servers are currently overloaded. Please try again later."
			if tc.generic {
				code, message = "server_error", "temporary upstream failure"
			}
			for turn := 1; turn <= 3; turn++ {
				for i := 0; i < tc.bareErrors; i++ {
					events = append(events, []byte(fmt.Sprintf(`{"type":"error","error":{"code":%q,"message":%q}}`, code, message)))
				}
				events = append(events, []byte(fmt.Sprintf(`{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":%q},"usage":{"input_tokens":7,"output_tokens":0}}}`, message)))
			}
			captureConn := &openAIWSCaptureConn{events: events}
			pool := newOpenAIWSConnPool(cfg)
			defer pool.Close()
			pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: captureConn})
			account := &Account{
				ID: 5403, Name: "openai-ingress-overload", Platform: PlatformOpenAI,
				Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test", "pool_mode": true},
				Extra: map[string]any{
					"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModeCtxPool,
				},
			}
			repo := &openAIWSIngressOverloadRepo{}
			limiter := NewRateLimitService(repo, nil, cfg, nil, nil)
			svc := &OpenAIGatewayService{
				accountRepo: repo, rateLimitService: limiter,
				cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{},
				openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
				toolCorrector:    NewCodexToolCorrector(), openaiWSPool: pool,
			}
			limiter.SetAccountRuntimeBlocker(svc)
			type turnObservation struct {
				turn          int
				result        *OpenAIForwardResult
				err           error
				cooling       bool
				modelFailures int
			}
			observed := make(chan turnObservation, 3)
			serverErrCh := make(chan error, 1)
			wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					serverErrCh <- err
					return
				}
				defer conn.CloseNow()
				readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
				_, firstMessage, err := conn.Read(readCtx)
				cancelRead()
				if err != nil {
					serverErrCh <- err
					return
				}
				ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ginCtx.Request = r.Clone(r.Context())
				hooks := &OpenAIWSIngressHooks{AfterTurn: func(turn int, result *OpenAIForwardResult, err error) {
					state := svc.getOpenAIAccountModelTransientState()
					state.mu.Lock()
					modelFailures := state.entries[openAIAccountModelKey{AccountID: account.ID, Model: "gpt-5.1"}].failureStreak
					state.mu.Unlock()
					observed <- turnObservation{turn, result, err, limiter.IsAccountOverloadCooling(account.ID, time.Now()), modelFailures}
				}}
				serverErrCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "sk-test", firstMessage, hooks)
			}))
			defer wsServer.Close()
			dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
			clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
			cancelDial()
			require.NoError(t, err)
			defer clientConn.CloseNow()

			for turn := 1; turn <= 3; turn++ {
				writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
				err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":true}`))
				cancelWrite()
				require.NoError(t, err)
				for i := 0; i <= tc.bareErrors; i++ {
					readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
					_, message, err := clientConn.Read(readCtx)
					cancelRead()
					require.NoError(t, err)
					if i == tc.bareErrors {
						require.Equal(t, "response.failed", gjson.GetBytes(message, "type").String())
					}
					require.NotContains(t, string(message), "server_is_overloaded")
				}
				select {
				case got := <-observed:
					require.NoError(t, got.err)
					require.Equal(t, turn, got.turn)
					require.NotNil(t, got.result)
					require.Equal(t, "response.failed", got.result.UpstreamTerminalEvent)
					require.Equal(t, 7, got.result.Usage.InputTokens)
					if tc.generic {
						require.False(t, got.cooling)
						require.Equal(t, turn, got.modelFailures, "paired/repeated 5xx frames count as one model failure per send")
					} else {
						require.False(t, got.cooling, "without an explicit group rule, paired overload frames must remain request-local")
					}
				case <-time.After(3 * time.Second):
					t.Fatal("waiting for overload turn observation timed out")
				}
			}
			if tc.generic {
				require.Zero(t, repo.overloadCalls.Load())
			} else {
				require.Zero(t, repo.overloadCalls.Load())
			}
			_ = clientConn.Close(coderws.StatusNormalClosure, "done")
			select {
			case err := <-serverErrCh:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("waiting for overload ingress shutdown timed out")
			}
			require.Len(t, captureConn.writes, 3, "each turn must send one upstream request without replay")
		})
	}
}
