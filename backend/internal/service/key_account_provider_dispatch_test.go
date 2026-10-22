package service

import (
	"bytes"
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

func keyAccountProviderTestContext(ctx context.Context, path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil).WithContext(ctx)
	return c, recorder
}

func TestKeyAccountSystemOneDispatchRecordsNativeTerminalAndUsage(t *testing.T) {
	requestBody := []byte(`{"model":"jev-latest","state":"hello","questions":{"q":{"type":"noul"}}}`)
	responseBody := `{"model":"jev-1.13.0","answers":{"q":{"answer":"ok"}},"usage":{"input_tokens":12,"output_tokens":3}}`
	var sends int
	upstream := &systemOneHTTPUpstream{do: func(req *http.Request) (*http.Response, error) {
		sends++
		require.Equal(t, "/v1/systemone", req.URL.Path)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(responseBody)), Request: req}, nil
	}}
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
		Metadata: keyrouting.AccountAttemptMetadata{Site: "test", APIKeyID: 11, GroupID: 21, Model: "jev-latest", Endpoint: "/v1/systemone", PolicyRevision: 4},
	})
	account.Platform = PlatformTypeSafe
	account.Type = AccountTypeAPIKey
	account.Credentials = map[string]any{"base_url": "http://typesafe.test", "api_key": "secret"}

	c, _ := keyAccountProviderTestContext(ctx, "/v1/systemone")
	result, err := newSystemOneTestService(upstream).ForwardSystemOne(ctx, c, account, requestBody)
	require.NoError(t, err)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.True(t, scope.ReplayBlocked(), "the consumed response must block replay before Finish")
	sample, err := scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess, CompleteResponse: true})
	require.NoError(t, err)
	require.Equal(t, 1, sends)
	require.True(t, sample.Dispatched)
	require.Equal(t, keyrouting.OutcomeSuccess, sample.Outcome)
	require.Equal(t, http.StatusOK, sample.UpstreamStatusCode)
	require.True(t, sample.UsageKnown)
	require.Equal(t, int64(7), sample.Metadata.AccountID)
	require.Equal(t, PlatformTypeSafe, sample.Metadata.Platform)
	require.Equal(t, "jev-latest", sample.Metadata.Model)
	require.Equal(t, "jev-latest", sample.Metadata.UpstreamModel)
	require.Equal(t, "/v1/systemone", sample.Metadata.UpstreamEndpoint)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountSystemOneUnknownAcceptedResponseCannotFailOver(t *testing.T) {
	var sends int
	upstream := &systemOneHTTPUpstream{do: func(req *http.Request) (*http.Response, error) {
		sends++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not-json")), Request: req}, nil
	}}
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	account.Platform = PlatformTypeSafe
	account.Type = AccountTypeAPIKey
	account.Credentials = map[string]any{"base_url": "http://typesafe.test", "api_key": "secret"}

	c, _ := keyAccountProviderTestContext(ctx, "/v1/systemone")
	svc := newSystemOneTestService(upstream)
	_, err := svc.ForwardSystemOne(ctx, c, account, []byte(`{"model":"jev-latest"}`))
	require.Error(t, err)
	_, err = svc.ForwardSystemOne(ctx, c, account, []byte(`{"model":"jev-latest"}`))
	require.Error(t, err, "an unknown accepted payload must not be dispatched twice")
	require.Equal(t, 1, sends)
	require.True(t, scope.ReplayBlocked())
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.Equal(t, 1, sends)
	require.True(t, sample.Dispatched)
	require.Equal(t, http.StatusOK, sample.UpstreamStatusCode)
	require.NotZero(t, sample.ReplayVeto&keyrouting.AttemptVetoUnknownProtocol)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountAnthropicPassthroughDispatchRecordsNativeTerminalAndUsage(t *testing.T) {
	const publicModel = "claude-sonnet-4-20250514"
	const responseBody = `{"id":"msg_1","type":"message","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":7,"cache_read_input_tokens":4}}`
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid-key-route"}},
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	}}
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
		Metadata: keyrouting.AccountAttemptMetadata{Site: "test", APIKeyID: 11, GroupID: 21, Model: publicModel, Endpoint: "/v1/messages", PolicyRevision: 4},
	})
	account = newAnthropicAPIKeyAccountForTest()
	account.Credentials["base_url"] = "http://anthropic.test"
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hello"}]}`))).WithContext(ctx)
	svc := &GatewayService{cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}}, httpUpstream: upstream, tlsFPProfileService: &TLSFingerprintProfileService{}}

	result, err := svc.forwardAnthropicAPIKeyPassthrough(ctx, c, account, []byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hello"}]}`), publicModel, publicModel, false, time.Now())
	require.NoError(t, err)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.Equal(t, 4, result.Usage.CacheReadInputTokens)
	require.True(t, scope.ReplayBlocked(), "the consumed response must block replay before Finish")
	sample, err := scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess, CompleteResponse: true, ResponseCommitted: true})
	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
	require.True(t, sample.Dispatched)
	require.Equal(t, keyrouting.OutcomeSuccess, sample.Outcome)
	require.Equal(t, http.StatusOK, sample.UpstreamStatusCode)
	require.True(t, sample.UsageKnown)
	require.True(t, sample.ResponseCommitted)
	require.Equal(t, account.ID, sample.Metadata.AccountID)
	require.Equal(t, PlatformAnthropic, sample.Metadata.Platform)
	require.Equal(t, publicModel, sample.Metadata.UpstreamModel)
	require.Equal(t, "/v1/messages", sample.Metadata.UpstreamEndpoint)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountGeminiChatCompatDispatchRecordsTerminalAndUsage(t *testing.T) {
	const model = "gemini-2.5-flash"
	body := `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}`
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}}
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
		Metadata: keyrouting.AccountAttemptMetadata{Site: "test", APIKeyID: 11, GroupID: 21, Model: model, Endpoint: "/v1/chat/completions", PolicyRevision: 4},
	})
	account.Platform = PlatformGemini
	account.Type = AccountTypeAPIKey
	account.Credentials = map[string]any{"api_key": "gemini-key"}
	c, _ := keyAccountProviderTestContext(ctx, "/v1/chat/completions")
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	result, err := svc.ForwardAsChatCompletions(ctx, c, account, []byte(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	require.Equal(t, 2, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
	require.True(t, scope.ReplayBlocked(), "the consumed response must block replay before Finish")
	sample, err := scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess, CompleteResponse: true})
	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
	require.True(t, sample.Dispatched)
	require.Equal(t, keyrouting.OutcomeSuccess, sample.Outcome)
	require.Equal(t, http.StatusOK, sample.UpstreamStatusCode)
	require.True(t, sample.UsageKnown)
	require.Equal(t, account.ID, sample.Metadata.AccountID)
	require.Equal(t, PlatformGemini, sample.Metadata.Platform)
	require.Equal(t, model, sample.Metadata.UpstreamModel)
	require.NotEmpty(t, sample.Metadata.UpstreamEndpoint)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountGeminiUnknownAcceptedResponseCannotFailOver(t *testing.T) {
	upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not-json"))}}
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	account.Platform = PlatformGemini
	account.Type = AccountTypeAPIKey
	account.Credentials = map[string]any{"api_key": "gemini-key"}
	c, _ := keyAccountProviderTestContext(ctx, "/v1/chat/completions")
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	_, err := svc.ForwardAsChatCompletions(ctx, c, account, []byte(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}]}`))
	require.Error(t, err)
	_, err = svc.ForwardAsChatCompletions(ctx, c, account, []byte(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"retry"}]}`))
	require.Error(t, err, "a second attempt must be rejected by the native gateway boundary")
	require.Equal(t, 1, upstream.calls)
	require.True(t, scope.ReplayBlocked())
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, http.StatusOK, sample.UpstreamStatusCode)
	require.NotZero(t, sample.ReplayVeto&keyrouting.AttemptVetoUnknownProtocol)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountGeminiNativeDispatchRecordsTerminalAndUsage(t *testing.T) {
	const model = "gemini-2.5-flash"
	const response = `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}`
	for _, stream := range []bool{false, true} {
		name, action, contentType, body := "http", "generateContent", "application/json", response
		if stream {
			name, action, contentType, body = "sse", "streamGenerateContent", "text/event-stream", "data: "+response+"\n\n"
		}
		t.Run(name, func(t *testing.T) {
			upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}}
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
				Metadata: keyrouting.AccountAttemptMetadata{Site: "test", APIKeyID: 11, GroupID: 21, Model: model, Endpoint: "/v1beta/models/" + model + ":" + action, PolicyRevision: 4},
			})
			account.Platform = PlatformGemini
			account.Type = AccountTypeAPIKey
			account.Credentials = map[string]any{"api_key": "gemini-key"}
			c, recorder := keyAccountProviderTestContext(ctx, "/v1beta/models/"+model+":"+action)
			svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}

			result, err := svc.ForwardNative(ctx, c, account, model, action, stream, []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
			require.NoError(t, err)
			require.Equal(t, 2, result.Usage.InputTokens)
			require.Equal(t, 1, result.Usage.OutputTokens)
			require.True(t, scope.ReplayBlocked(), "the consumed response must block replay before Finish")
			sample, err := scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess, CompleteResponse: true, ResponseCommitted: true})
			require.NoError(t, err)
			require.Equal(t, 1, upstream.calls)
			require.Equal(t, body, recorder.Body.String())
			require.True(t, sample.Dispatched)
			require.Equal(t, keyrouting.OutcomeSuccess, sample.Outcome)
			require.Equal(t, http.StatusOK, sample.UpstreamStatusCode)
			require.True(t, sample.UsageKnown)
			require.True(t, sample.ResponseCommitted)
			require.Equal(t, account.ID, sample.Metadata.AccountID)
			require.Equal(t, PlatformGemini, sample.Metadata.Platform)
			require.Equal(t, model, sample.Metadata.UpstreamModel)
			require.Contains(t, sample.Metadata.UpstreamEndpoint, action)
			require.True(t, scope.CleanupAck())
			require.False(t, scope.CanStartNext())
		})
	}
}

func TestKeyAccountAntigravityUpstreamDispatchRecordsNativeTerminalAndUsage(t *testing.T) {
	const publicModel = "public-claude"
	const responseBody = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2,"cache_read_input_tokens":1}}`
	var sends int
	upstream := &systemOneHTTPUpstream{do: func(req *http.Request) (*http.Response, error) {
		sends++
		require.Equal(t, "https://upstream.example/v1/messages", req.URL.String())
		require.Equal(t, "Bearer secret", req.Header.Get("Authorization"))
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(responseBody)), Request: req}, nil
	}}
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
		Metadata: keyrouting.AccountAttemptMetadata{Site: "test", APIKeyID: 11, GroupID: 21, Model: publicModel, Endpoint: "/v1/messages", PolicyRevision: 4},
	})
	account.Platform = PlatformAntigravity
	account.Type = AccountTypeUpstream
	account.Credentials = map[string]any{"base_url": "https://upstream.example", "api_key": "secret"}
	c, recorder := keyAccountProviderTestContext(ctx, "/v1/messages")

	result, err := (&AntigravityGatewayService{httpUpstream: upstream}).ForwardUpstream(ctx, c, account, []byte(`{"model":"claude-sonnet-4-20250514","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, 1, result.Usage.CacheReadInputTokens)
	require.True(t, scope.ReplayBlocked(), "the consumed response must block replay before Finish")
	sample, err := scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess, CompleteResponse: true, ResponseCommitted: true})
	require.NoError(t, err)
	require.Equal(t, 1, sends)
	require.Equal(t, responseBody, recorder.Body.String())
	require.True(t, sample.Dispatched)
	require.Equal(t, keyrouting.OutcomeSuccess, sample.Outcome)
	require.Equal(t, http.StatusOK, sample.UpstreamStatusCode)
	require.True(t, sample.UsageKnown)
	require.True(t, sample.ResponseCommitted)
	require.Equal(t, PlatformAntigravity, sample.Metadata.Platform)
	require.Equal(t, "claude-sonnet-4-20250514", sample.Metadata.UpstreamModel)
	require.Equal(t, "/v1/messages", sample.Metadata.UpstreamEndpoint)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}
