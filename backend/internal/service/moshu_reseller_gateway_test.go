//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestMoshuResellerChatPreservesProtocolAndUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "stream"}[stream], func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.6-sol","messages":[{"role":"user","content":"OK"}],"stream":false,"temperature":0.2}`)
			response := `{"model":"gpt-5.6-sol","choices":[{"message":{"content":"OK"}}],"usage":{"prompt_tokens":9,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":3}}}`
			ct := "application/json"
			if stream {
				body = bytes.Replace(body, []byte(`"stream":false`), []byte(`"stream":true`), 1)
				response = "data: {\"model\":\"gpt-5.6-sol\",\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4,\"prompt_tokens_details\":{\"cached_tokens\":3}}}\n\ndata: [DONE]\n\n"
				ct = "text/event-stream"
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(response))}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := rawChatCompletionsTestAccount()
			account.Extra = map[string]any{"moshu_reseller_managed": true, "openai_passthrough": true}
			result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			require.NoError(t, err)
			require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
			require.Equal(t, stream, gjson.GetBytes(upstream.lastBody, "stream").Bool())
			require.Equal(t, "OK", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
			require.Equal(t, 9, result.Usage.InputTokens)
			require.Equal(t, 4, result.Usage.OutputTokens)
			require.Equal(t, 3, result.Usage.CacheReadInputTokens)
			require.Len(t, upstream.requests, 1)
			require.Contains(t, rec.Body.String(), "OK")
		})
	}
}

func TestMoshuResellerDoesNotReplayRejectedReservation(t *testing.T) {
	svc := &OpenAIGatewayService{}
	for _, managed := range []bool{false, true} {
		account := rawChatCompletionsTestAccount()
		account.Extra = map[string]any{"moshu_reseller_managed": managed}
		err := svc.newOpenAIAccountFailoverError(account, 503, nil, []byte(`{"error":{"message":"No available accounts"}}`), "No available accounts", false, true)
		require.Equal(t, !managed, err.ShouldRetryNextAccount())
		if managed {
			require.False(t, err.RetryableOnSameAccount)
		}
	}
}

func TestMoshuResellerMessagesPreserveNativeProtocol(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","messages":[{"role":"user","content":"OK"}],"stream":false,"max_tokens":32}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicBufferedResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{"moshu_reseller_managed": true}
	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.Equal(t, "/v1/messages", upstream.lastReq.URL.Path)
	require.Equal(t, "OK", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
	require.Equal(t, 93, result.Usage.InputTokens)
	require.Equal(t, 16, result.Usage.OutputTokens)
	require.Len(t, upstream.requests, 1)
}

func TestMoshuResellerResponsesDoesNotRetryRejectedFields(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":"OK","instructions":"Be concise","stream":false,"temperature":0.2}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"error":{"code":"unsupported_parameter","param":"temperature","message":"Unsupported parameter: 'temperature' is not supported with this model."}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{"moshu_reseller_managed": true, "openai_passthrough": true}
	_, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMoshuResellerCompactDoesNotReplayReservation(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	svc.cfg.Gateway.OpenAICompactModel = "gpt-5.4"
	c := newOpenAICompactFallbackTestContext(t, "/v1/responses/compact")
	body := []byte(`{"model":"gpt-5.5","input":"OK"}`)
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{"moshu_reseller_managed": true}
	_, _, retry := svc.prepareOpenAICompactFallbackRetry(c, account, "gpt-5.5", body, 400, "context window exceeded", []byte(`{"error":{"code":"context_length_exceeded"}}`), false)
	require.False(t, retry)
}
