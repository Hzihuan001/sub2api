//go:build unit

package service

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardAsResponsesKiroDirectUsesResponsesCacheProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetKiroCacheTracker()

	account := &Account{
		ID:          301,
		Name:        "kiro-responses-cache",
		Platform:    PlatformKiro,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "kiro-access-token",
			"profile_arn":  "arn:aws:codewhisperer:us-east-1:123456789012:profile/RESPONSECACHE",
		},
	}
	group := kiroCacheGroup(1)
	body := kiroResponsesCacheRequestBody("gateway", "workspace-gateway", "resp-gateway")
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), "responses")
	require.NoError(t, err)
	parsed.Group = group

	upstream := &queuedHTTPUpstream{responses: []*http.Response{
		kiroResponsesCacheUpstreamResponse(t, 5),
		kiroResponsesCacheUpstreamResponse(t, 7),
	}}
	svc := &GatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			StreamDataIntervalTimeout: 0,
			MaxLineSize:               defaultMaxLineSize,
		}},
		httpUpstream:        upstream,
		kiroCooldownStore:   &stubKiroCooldownStore{},
		tlsFPProfileService: &TLSFingerprintProfileService{},
		rateLimitService:    &RateLimitService{},
	}

	firstCtx, firstRec := newResponsesGatewayTestContext()
	firstResult, err := svc.ForwardAsResponses(firstCtx.Request.Context(), firstCtx, account, body, parsed)
	require.NoError(t, err)
	require.Equal(t, 0, firstResult.Usage.CacheReadInputTokens)
	require.Greater(t, firstResult.Usage.CacheCreationInputTokens, 0)
	require.Equal(t, firstResult.Usage.CacheCreationInputTokens, int(gjson.Get(firstRec.Body.String(), "usage.cache_creation_input_tokens").Int()))
	require.False(t, gjson.Get(firstRec.Body.String(), "usage.input_tokens_details.cached_tokens").Exists())

	secondCtx, secondRec := newResponsesGatewayTestContext()
	secondResult, err := svc.ForwardAsResponses(secondCtx.Request.Context(), secondCtx, account, body, parsed)
	require.NoError(t, err)
	require.Greater(t, secondResult.Usage.CacheReadInputTokens, 0)
	require.Equal(t, 0, secondResult.Usage.CacheCreationInputTokens)
	require.Equal(t, secondResult.Usage.CacheReadInputTokens, int(gjson.Get(secondRec.Body.String(), "usage.input_tokens_details.cached_tokens").Int()))
	require.Equal(t, 0, int(gjson.Get(secondRec.Body.String(), "usage.cache_creation_input_tokens").Int()))
	require.Len(t, upstream.requests, 2)
}

func TestExtractResponsesReasoningEffortFromBody(t *testing.T) {
	t.Parallel()

	got := ExtractResponsesReasoningEffortFromBody([]byte(`{"model":"claude-sonnet-4.5","reasoning":{"effort":"HIGH"}}`))
	require.NotNil(t, got)
	require.Equal(t, "high", *got)

	maxGot := ExtractResponsesReasoningEffortFromBody([]byte(`{"model":"deepseek-v4-pro","reasoning":{"effort":"max"}}`))
	require.NotNil(t, maxGot)
	require.Equal(t, "xhigh", *maxGot)

	require.Nil(t, ExtractResponsesReasoningEffortFromBody([]byte(`{"model":"claude-sonnet-4.5"}`)))
}

func newResponsesGatewayTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, rec
}

func kiroResponsesCacheUpstreamResponse(t *testing.T, outputTokens int) *http.Response {
	t.Helper()
	var upstreamBody bytes.Buffer
	_, _ = upstreamBody.Write(buildKiroEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{"content": "hello"},
	}))
	_, _ = upstreamBody.Write(buildKiroEventStreamFrame(t, "messageMetadataEvent", map[string]any{
		"messageMetadataEvent": map[string]any{
			"tokenUsage": map[string]any{
				"uncachedInputTokens": 99,
				"outputTokens":        outputTokens,
			},
		},
	}))
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/vnd.amazon.eventstream"}},
		Body:       io.NopCloser(&upstreamBody),
	}
}

func TestHandleResponsesBufferedStreamingResponse_PreservesMessageStartCacheUsage(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_buffered"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","stop_reason":"","usage":{"input_tokens":12,"cache_read_input_tokens":9,"cache_creation_input_tokens":3}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7,"_sub2api_kiro_credits":0.17}}`,
			``,
		}, "\n"))),
	}

	svc := &GatewayService{}
	result, err := svc.handleResponsesBufferedStreamingResponse(resp, c, "claude-sonnet-4.5", "claude-sonnet-4.5", nil, time.Now(), nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.Equal(t, 9, result.Usage.CacheReadInputTokens)
	require.Equal(t, 3, result.Usage.CacheCreationInputTokens)
	require.InDelta(t, 0.17, result.Usage.KiroCredits, 0.000001)
	require.Contains(t, rec.Body.String(), `"cached_tokens":9`)
	require.NotContains(t, rec.Body.String(), "_sub2api_kiro_credits")
}

func TestHandleResponsesStreamingResponse_PreservesMessageStartCacheUsage(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	resp := &http.Response{
		Header: http.Header{"x-request-id": []string{"rid_stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","stop_reason":"","usage":{"input_tokens":20,"cache_read_input_tokens":11,"cache_creation_input_tokens":4}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8,"_sub2api_kiro_credits":0.23}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}

	svc := &GatewayService{}
	result, err := svc.handleResponsesStreamingResponse(resp, c, "claude-sonnet-4.5", "claude-sonnet-4.5", nil, time.Now(), nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Equal(t, 8, result.Usage.OutputTokens)
	require.Equal(t, 11, result.Usage.CacheReadInputTokens)
	require.Equal(t, 4, result.Usage.CacheCreationInputTokens)
	require.InDelta(t, 0.23, result.Usage.KiroCredits, 0.000001)
	require.Contains(t, rec.Body.String(), `response.completed`)
	require.NotContains(t, rec.Body.String(), "_sub2api_kiro_credits")
}
