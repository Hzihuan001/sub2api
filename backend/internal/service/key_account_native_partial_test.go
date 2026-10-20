//go:build unit

package service

import (
	"context"
	"errors"
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

func TestKeyAccountNativePartialParsers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type parser func(context.Context, *gin.Context, *Account, *http.Response) (*ClaudeUsage, error)
	gemini := &GeminiMessagesCompatService{cfg: &config.Config{}}
	ag := &AntigravityGatewayService{settingService: &SettingService{cfg: &config.Config{}}}
	cases := []struct {
		name, family string
		parse        parser
	}{
		{"Anthropic SSE", "anthropic", func(ctx context.Context, c *gin.Context, a *Account, resp *http.Response) (*ClaudeUsage, error) {
			result, err := (&GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}).handleStreamingResponse(ctx, resp, c, a, time.Now(), "m", "m", false)
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
		{"Gemini Messages SSE", "gemini", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) (*ClaudeUsage, error) {
			result, err := gemini.handleStreamingResponse(c, resp, time.Now(), "m")
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
		{"Gemini Chat SSE", "gemini", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) (*ClaudeUsage, error) {
			result, err := gemini.handleChatCompletionsStreamingResponseFromGemini(c, resp, time.Now(), "m", false, true)
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
		{"Gemini native SSE", "gemini", func(_ context.Context, c *gin.Context, a *Account, resp *http.Response) (*ClaudeUsage, error) {
			result, err := gemini.handleNativeStreamingResponse(c, resp, time.Now(), false, a, "")
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
		{"Gemini collected SSE", "gemini", func(_ context.Context, _ *gin.Context, _ *Account, resp *http.Response) (*ClaudeUsage, error) {
			_, usage, _, err := collectGeminiSSEForKeyAccount(resp, false, nil)
			return usage, err
		}},
		{"Antigravity Gemini SSE", "antigravity", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) (*ClaudeUsage, error) {
			result, err := ag.handleGeminiStreamingResponse(c, resp, time.Now())
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
		{"Antigravity Gemini collect", "antigravity", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) (*ClaudeUsage, error) {
			result, err := ag.handleGeminiStreamToNonStreaming(c, resp, time.Now())
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
		{"Antigravity Claude SSE", "antigravity", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) (*ClaudeUsage, error) {
			result, err := ag.handleClaudeStreamingResponse(c, resp, time.Now(), "m")
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
		{"Antigravity Claude collect", "antigravity", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) (*ClaudeUsage, error) {
			_, result, err := ag.collectClaudeStreamResponse(c, resp, time.Now(), "m")
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
		{"Antigravity Claude JSON bridge", "antigravity", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) (*ClaudeUsage, error) {
			result, err := ag.handleClaudeStreamToNonStreaming(c, resp, time.Now(), "m")
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
		{"Antigravity Chat JSON bridge", "antigravity", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) (*ClaudeUsage, error) {
			result, err := ag.handleChatCompletionsNonStreamingFromAntigravity(c, resp, time.Now(), "m")
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
		{"Antigravity Responses JSON bridge", "antigravity", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) (*ClaudeUsage, error) {
			result, err := ag.handleResponsesNonStreamingFromAntigravity(c, resp, time.Now(), "m")
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
		{"Antigravity Chat SSE bridge", "antigravity", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) (*ClaudeUsage, error) {
			result, err := ag.handleChatCompletionsStreamingFromAntigravity(c, resp, time.Now(), "m", true)
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
		{"Antigravity Responses SSE bridge", "antigravity", func(_ context.Context, c *gin.Context, _ *Account, resp *http.Response) (*ClaudeUsage, error) {
			result, err := ag.handleResponsesStreamingFromAntigravity(c, resp, time.Now(), "m")
			if result != nil {
				return result.usage, err
			}
			return nil, err
		}},
	}
	for _, tc := range cases {
		for _, mode := range []string{"usage", "zero_usage_error", "output", "immediate", "read_error"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				body := keyAccountNativePartialWire(tc.family, mode)
				ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
				account.Type = AccountTypeAPIKey
				account.Credentials = map[string]any{}
				var reader io.Reader = strings.NewReader(body)
				if mode == "read_error" {
					reader = &antigravityCompatErrorReader{data: []byte(body), err: io.ErrUnexpectedEOF}
				}
				raw := &keyAccountTrackedTestBody{Reader: reader}
				calls := 0
				resp, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "wire-model", func(*http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: raw}, nil
				})
				require.NoError(t, err)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
				usage, parseErr := tc.parse(ctx, c, account, resp)
				require.Error(t, parseErr)
				billable := mode == "usage" || mode == "zero_usage_error" || mode == "read_error"
				if billable {
					require.NotNil(t, usage)
					require.Equal(t, 7, usage.InputTokens)
					require.Equal(t, 3, usage.OutputTokens)
					var failover *UpstreamFailoverError
					require.False(t, errors.As(parseErr, &failover), "reported usage must not become a native retry signal")
				} else {
					require.False(t, usage.hasObservedTokens())
				}
				require.NotContains(t, recorder.Body.String(), `response.completed`)
				require.NotContains(t, recorder.Body.String(), `[DONE]`)
				require.NotContains(t, recorder.Body.String(), `message_stop`)
				require.NoError(t, resp.Body.Close())
				var result *ForwardResult
				if billable {
					result = &ForwardResult{Usage: *usage}
				}
				sample, err := scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure, UsageKnown: billable})
				require.NoError(t, err)
				require.True(t, scope.CleanupAck())
				require.Equal(t, mode == "immediate", scope.CanStartNext(), "sample=%+v", sample)
				require.Equal(t, 1, calls)
				require.EqualValues(t, 1, raw.closes.Load())
			})
		}
	}
}

func keyAccountNativePartialWire(family, mode string) string {
	if family == "anthropic" {
		body := ""
		if mode == "usage" || mode == "zero_usage_error" || mode == "read_error" {
			body = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7}}}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\n"
		} else if mode == "output" {
			body = "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n"
		}
		if mode != "read_error" {
			body += "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"busy\"}}\n\n"
		}
		return body
	}
	wrap := func(payload string) string {
		if family == "antigravity" {
			payload = `{"response":` + payload + `}`
		}
		return "data: " + payload + "\n\n"
	}
	body := ""
	if mode == "usage" || mode == "zero_usage_error" || mode == "read_error" {
		body = wrap(`{"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3,"totalTokenCount":10}}`)
	} else if mode == "output" {
		body = wrap(`{"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}`)
	}
	if mode != "read_error" {
		if mode == "zero_usage_error" {
			body += wrap(`{"error":{"code":503,"message":"busy","status":"UNAVAILABLE"},"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0}}`)
		} else {
			body += wrap(`{"error":{"code":503,"message":"busy","status":"UNAVAILABLE"}}`)
		}
	}
	return body
}

func TestKeyAccountPartialUsageResultIsScopedAndNotFailover(t *testing.T) {
	ctx, scope, _, resp := keyAccountProtocolFixture(t, "{}")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	nativeErr := &UpstreamFailoverError{StatusCode: 503}
	usage := &ClaudeUsage{InputTokens: 7, OutputTokens: 3}
	result, err := keyAccountPartialUsageResult(resp, c, usage, nil, "public", "mapped", true, time.Now(), nativeErr)
	require.NotNil(t, result)
	require.Equal(t, *usage, result.Usage)
	var failover *UpstreamFailoverError
	require.False(t, errors.As(err, &failover))
	result, err = keyAccountPartialUsageResult(&http.Response{}, c, usage, nil, "public", "mapped", true, time.Now(), nativeErr)
	require.Nil(t, result)
	require.Same(t, nativeErr, err, "unscoped native error identity is unchanged")
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
}
