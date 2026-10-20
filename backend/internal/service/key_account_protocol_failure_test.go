package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func keyAccountProtocolFixture(t *testing.T, body string) (context.Context, *KeyAccountForwardScope, *Account, *http.Response) {
	t.Helper()
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	account.Type = AccountTypeAPIKey
	account.Credentials = map[string]any{}
	response, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "wire-model", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	return ctx, scope, account, response
}

func TestKeyAccountProtocolFailureRequiresDecodedError(t *testing.T) {
	cases := []struct {
		name, payload string
		mark          func(context.Context, []byte) bool
		verified      bool
	}{
		{"openai typed", `{"type":"response.failed","error":{"code":"server_error","message":"unavailable"}}`, markKeyAccountOpenAIProtocolFailure, true},
		{"openai nested", `{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error"},"output":[]}}`, markKeyAccountOpenAIProtocolFailure, true},
		{"openai flat", `{"type":"error","code":"server_error","message":"unavailable"}`, markKeyAccountOpenAIProtocolFailure, true},
		{"openai event only", `{"type":"response.failed"}`, markKeyAccountOpenAIProtocolFailure, false},
		{"openai empty error", `{"type":"error","error":{}}`, markKeyAccountOpenAIProtocolFailure, false},
		{"openai text error", `{"error":"overload"}`, markKeyAccountOpenAIProtocolFailure, false},
		{"anthropic typed", `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`, markKeyAccountAnthropicProtocolFailure, true},
		{"anthropic event only", `{"type":"error"}`, markKeyAccountAnthropicProtocolFailure, false},
		{"anthropic missing type", `{"error":{"message":"busy","type":"overloaded_error"}}`, markKeyAccountAnthropicProtocolFailure, false},
		{"bad JSON", `{"type":"error","error":`, markKeyAccountAnthropicProtocolFailure, false},
		{"empty", ``, markKeyAccountOpenAIProtocolFailure, false},
		{"trailing JSON", `{"type":"error","code":"bad"}{}`, markKeyAccountOpenAIProtocolFailure, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, scope, _, response := keyAccountProtocolFixture(t, tc.payload)
			proofContext := KeyAccountResponseContext(response)
			_, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, tc.verified, tc.mark(proofContext, []byte(tc.payload)))
			require.NoError(t, response.Body.Close())
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.Equal(t, tc.verified, sample.VerifiedFailure)
			require.Equal(t, 200, sample.UpstreamStatusCode, "semantic error must not fabricate an HTTP status")
			require.True(t, scope.CleanupAck())
			require.Equal(t, tc.verified, scope.CanStartNext())
		})
	}
}

func TestKeyAccountProtocolFailureNeverClearsSafetyVetoes(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
		veto        keyrouting.AccountAttemptReplayVeto
	}{
		{"usage", `,"usage":{"input_tokens":7}`, keyrouting.AttemptVetoUsage},
		{"wrapped Gemini usage", `,"response":{"usageMetadata":{"promptTokenCount":7}}`, keyrouting.AttemptVetoUsage},
		{"output", `,"response":{"output":[{"type":"message","content":[{"text":"done"}]}]}`, keyrouting.AttemptVetoFirstContent},
		{"side effect", `,"tool_calls":[{"id":"tool-1"}]`, keyrouting.AttemptVetoSideEffect},
		{"completed", `,"response":{"status":"completed"}`, keyrouting.AttemptVetoCompleteResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{"type":"error","code":"server_error","message":"busy"` + tc.extra + `}`
			_, scope, _, response := keyAccountProtocolFixture(t, payload)
			proofContext := KeyAccountResponseContext(response)
			_, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.True(t, markKeyAccountOpenAIProtocolFailure(proofContext, []byte(payload)))
			require.NoError(t, response.Body.Close())
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.True(t, sample.VerifiedFailure)
			require.NotZero(t, sample.ReplayVeto&tc.veto)
			require.True(t, scope.CleanupAck())
			require.False(t, scope.CanStartNext())
		})
	}
}

func TestKeyAccountProtocolNativeParsersProvideFailureProof(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const anthropic = `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`
	const openai = `{"type":"response.failed","error":{"code":"server_error","message":"upstream temporarily unavailable"}}`
	const gemini = `{"error":{"code":503,"message":"busy","status":"UNAVAILABLE"}}`
	cases := []struct {
		name, body string
		parse      func(context.Context, *gin.Context, *Account, *http.Response) error
	}{
		{"anthropic SSE", "event: error\ndata: " + anthropic + "\n\n", func(ctx context.Context, c *gin.Context, a *Account, resp *http.Response) error {
			svc := &GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}
			_, err := svc.handleStreamingResponse(ctx, resp, c, a, time.Now(), "m", "m", false)
			return err
		}},
		{"OpenAI SSE to JSON", "event: response.failed\ndata: " + openai + "\n\n", func(_ context.Context, c *gin.Context, a *Account, resp *http.Response) error {
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			_, err = (&OpenAIGatewayService{cfg: &config.Config{}}).handleSSEToJSON(resp, c, a, body, "m", "m")
			return err
		}},
		{"OpenAI passthrough SSE to JSON", "event: response.failed\ndata: " + openai + "\n\n", func(_ context.Context, c *gin.Context, a *Account, resp *http.Response) error {
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			_, err = (&OpenAIGatewayService{cfg: &config.Config{}}).handlePassthroughSSEToJSON(resp, c, a, body, "m", "m")
			return err
		}},
		{"Gemini messages JSON", gemini, func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) error {
			_, err := (&GeminiMessagesCompatService{cfg: &config.Config{}}).handleNonStreamingResponse(c, resp, "m")
			return err
		}},
		{"Gemini chat JSON", gemini, func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) error {
			_, err := (&GeminiMessagesCompatService{cfg: &config.Config{}}).handleChatCompletionsNonStreamingResponseFromGemini(c, resp, "m", false)
			return err
		}},
		{"Gemini native JSON", gemini, func(_ context.Context, c *gin.Context, a *Account, resp *http.Response) error {
			_, err := (&GeminiMessagesCompatService{cfg: &config.Config{}}).handleNativeNonStreamingResponse(c, resp, false, a, "")
			return err
		}},
		{"Gemini collected SSE", "data: " + gemini + "\n\n", func(_ context.Context, _ *gin.Context, _ *Account, resp *http.Response) error {
			_, _, _, err := collectGeminiSSEForKeyAccount(resp, false, nil)
			return err
		}},
		{"Antigravity Gemini SSE", "data: {\"response\":" + gemini + "}\n\n", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) error {
			svc := &AntigravityGatewayService{settingService: &SettingService{cfg: &config.Config{}}}
			_, err := svc.handleGeminiStreamToNonStreaming(c, resp, time.Now())
			return err
		}},
		{"Antigravity Claude SSE", "data: {\"response\":" + gemini + "}\n\n", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) error {
			svc := &AntigravityGatewayService{settingService: &SettingService{cfg: &config.Config{}}}
			_, _, err := svc.collectClaudeStreamResponse(c, resp, time.Now(), "m")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, scope, account, response := keyAccountProtocolFixture(t, tc.body)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
			require.Error(t, tc.parse(ctx, c, account, response))
			require.NoError(t, response.Body.Close())
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.True(t, sample.VerifiedFailure)
			require.Equal(t, 200, sample.UpstreamStatusCode)
			require.True(t, scope.CleanupAck())
			require.True(t, scope.CanStartNext(), "proven transient failure before output is replay-safe")
		})
	}
}

func TestKeyAccountProtocolGeminiUsageKeepsPartialBillingAndPreventsReplay(t *testing.T) {
	const payload = `{"error":{"code":503,"message":"busy","status":"UNAVAILABLE"},"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3,"totalTokenCount":10}}`
	ctx, scope, account, response := keyAccountProtocolFixture(t, payload)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	usage, err := (&GeminiMessagesCompatService{cfg: &config.Config{}}).handleNativeNonStreamingResponse(c, response, false, account, "")
	require.Error(t, err, "a decoded provider error must not synthesize a successful generation")
	require.Equal(t, 7, usage.InputTokens)
	require.Equal(t, 3, usage.OutputTokens)
	require.NoError(t, response.Body.Close())
	sample, err := scope.Finish(&ForwardResult{Usage: *usage}, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.True(t, sample.VerifiedFailure)
	require.Equal(t, keyrouting.OutcomeFailure, sample.Outcome)
	require.True(t, sample.UsageKnown)
	require.NotZero(t, sample.ReplayVeto&keyrouting.AttemptVetoUsage)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountProtocolNativeUnknownResponsesNeverGiveProof(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		parse      func(context.Context, *gin.Context, *Account, *http.Response) error
	}{
		{"Anthropic malformed event", "event: error\ndata: not-json\n\n", func(ctx context.Context, c *gin.Context, a *Account, resp *http.Response) error {
			_, err := (&GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}).handleStreamingResponse(ctx, resp, c, a, time.Now(), "m", "m", false)
			return err
		}},
		{"Anthropic event name only", "event: error\n\n", func(ctx context.Context, c *gin.Context, a *Account, resp *http.Response) error {
			_, err := (&GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}).handleStreamingResponse(ctx, resp, c, a, time.Now(), "m", "m", false)
			return err
		}},
		{"Gemini malformed JSON", "not-json", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) error {
			_, err := (&GeminiMessagesCompatService{cfg: &config.Config{}}).handleNonStreamingResponse(c, resp, "m")
			return err
		}},
		{"Antigravity empty SSE", "", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) error {
			svc := &AntigravityGatewayService{settingService: &SettingService{cfg: &config.Config{}}}
			_, _, err := svc.collectClaudeStreamResponse(c, resp, time.Now(), "m")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, scope, account, response := keyAccountProtocolFixture(t, tc.body)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
			require.Error(t, tc.parse(ctx, c, account, response))
			require.NoError(t, response.Body.Close())
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.False(t, sample.VerifiedFailure)
			require.True(t, scope.CleanupAck())
			require.False(t, scope.CanStartNext(), "a synthetic error is not proof that replay is safe")
		})
	}
}

func TestKeyAccountProtocolUnscopedGeminiKeepsNativeBehavior(t *testing.T) {
	const payload = `{"error":{"code":503,"message":"busy","status":"UNAVAILABLE"}}`
	response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	usage, err := (&GeminiMessagesCompatService{cfg: &config.Config{}}).handleNonStreamingResponse(c, response, "m")
	require.NoError(t, err, "the opt-in adapter must not rewrite unscoped native parsing")
	require.NotNil(t, usage)
	require.Nil(t, KeyAccountResponseContext(response))
}

func TestKeyAccountProtocolFailureDoesNotMarkNewWire(t *testing.T) {
	ctx, scope, account, response := keyAccountProtocolFixture(t, `{"type":"error","code":"server_error"}`)
	oldContext := KeyAccountResponseContext(response)
	_, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.True(t, markKeyAccountOpenAIProtocolFailure(oldContext, []byte(`{"type":"error","code":"server_error"}`)))
	newResponse, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m2", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = newResponse.Body.Close() })
	require.False(t, markKeyAccountOpenAIProtocolFailure(oldContext, []byte(`{"type":"error","code":"server_error"}`)))
	require.False(t, scope.CurrentAttempt().VerifiedFailure())
}
