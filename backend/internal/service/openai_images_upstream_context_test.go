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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
)

func newOpenAIImagesTestContext(t *testing.T, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	return c, rec
}

func newOpenAIImagesTestService(upstream HTTPUpstream) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		httpUpstream: upstream,
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{Enabled: false},
			},
		},
	}
}

func newOpenAIImagesAPIKeyAccount() *Account {
	return &Account{
		ID:       31,
		Name:     "openai-apikey-images",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://api.openai.com/v1",
		},
	}
}

func openAIImagesJSONResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"req_img_ctx"},
		},
		Body: io.NopCloser(strings.NewReader(
			`{"created":1710000000,"data":[{"b64_json":"aGVsbG8="}],"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}`,
		)),
	}
}

// issue #5411：生图是长耗时、上游侧已经产生实际成本的操作。客户端中途断开时，
// 如果连带取消上游请求，就会出现「上游已出图并计费、网关记 502 context canceled、
// 用户不扣费」。非流式路径以前走 detachStreamUpstreamContext(ctx, false)，
// 该函数在非流式时原样返回请求 context，因此不脱钩。
func TestForwardOpenAIImagesAPIKey_NonStreamDetachesUpstreamContext(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","response_format":"b64_json"}`)
	c, _ := newOpenAIImagesTestContext(t, body)

	recorder := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
	svc := newOpenAIImagesTestService(recorder)

	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.False(t, parsed.Stream, "本用例覆盖非流式生图")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 客户端已断开

	result, err := svc.ForwardImages(ctx, c, newOpenAIImagesAPIKeyAccount(), body, parsed, "")

	require.NoError(t, err, "客户端断开不应把已在出图的上游调用打断成 context canceled")
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount, "图片已产出，必须带回结果供计费")

	require.NotNil(t, recorder.lastReq)
	require.NoError(t, recorder.lastReq.Context().Err(),
		"交给上游的请求 context 必须已脱钩，不随客户端断开取消")
}

// 流式路径本来就脱钩，这条守卫防止对齐时把它改坏。
func TestForwardOpenAIImagesAPIKey_StreamKeepsDetachedUpstreamContext(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","stream":true,"response_format":"b64_json"}`)
	c, _ := newOpenAIImagesTestContext(t, body)

	recorder := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"req_img_ctx_stream"},
		},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.created\",\"response\":{\"created_at\":1710000000}}\n\n" +
				"data: {\"type\":\"response.image_generation_call.completed\",\"result\":\"aGVsbG8=\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":20}}}\n\n",
		)),
	}}
	svc := newOpenAIImagesTestService(recorder)

	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.True(t, parsed.Stream)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = svc.ForwardImages(ctx, c, newOpenAIImagesAPIKeyAccount(), body, parsed, "")

	require.NotNil(t, recorder.lastReq)
	require.NoError(t, recorder.lastReq.Context().Err(),
		"流式路径原本就脱钩，不能被改回随客户端取消")
}

// 两个 detach 辅助函数的语义差异是本次修复的根据，锁死它们防止被悄悄改动。
func TestDetachUpstreamContextSemantics(t *testing.T) {
	t.Run("detachUpstreamContext_always_detaches", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		detached, release := detachUpstreamContext(ctx)
		defer release()
		require.NoError(t, detached.Err())
	})

	t.Run("detachStreamUpstreamContext_keeps_cancel_when_not_streaming", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		same, release := detachStreamUpstreamContext(ctx, false)
		defer release()
		require.ErrorIs(t, same.Err(), context.Canceled,
			"非流式时该函数原样返回请求 context —— 生图路径不能用它")
	})

	t.Run("detachStreamUpstreamContext_detaches_when_streaming", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		detached, release := detachStreamUpstreamContext(ctx, true)
		defer release()
		require.NoError(t, detached.Err())
	})
}

func TestOpenAIImageDrainTimeoutStaysBoundedWhenIdleTimeoutDisabled(t *testing.T) {
	var unavailable *OpenAIGatewayService
	require.Equal(t, 30*time.Minute, unavailable.openAIImageDrainTimeout())
	svc := newOpenAIImagesTestService(nil)
	require.Zero(t, svc.openAIImageStreamDataInterval())
	require.Equal(t, 30*time.Minute, svc.openAIImageDrainTimeout())
	svc.cfg.Gateway.ImageStreamDataIntervalTimeout = 60
	require.Equal(t, time.Minute, svc.openAIImageDrainTimeout(), "explicit shorter image timeout must remain effective")
	svc.cfg.Gateway.ImageStreamDataIntervalTimeout = 1800
	require.Equal(t, 30*time.Minute, svc.openAIImageDrainTimeout())
}

type imageDeadlineHTTPUpstream struct {
	HTTPUpstream
	client  *http.Client
	attempt context.Context
}

type imageDeadlineResponseBody struct {
	io.ReadCloser
	release context.CancelFunc
}

func (b *imageDeadlineResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.release()
	return err
}

func (u *imageDeadlineHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	ctx, release := pkghttputil.NewUpstreamAttemptContext(req.Context(), time.Second)
	u.attempt = ctx
	resp, err := u.client.Do(req.WithContext(ctx))
	if err != nil {
		release()
		return nil, err
	}
	resp.Body = &imageDeadlineResponseBody{ReadCloser: resp.Body, release: release}
	return resp, nil
}

func TestForwardOpenAIImagesAPIKey_TaskDeadlineSurvivesDetachAndTransport(t *testing.T) {
	for _, guarded := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary context", true: "first content guard"}[guarded], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				select {
				case <-req.Context().Done():
				case <-time.After(500 * time.Millisecond):
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"data":[{"b64_json":"aGVsbG8="}]}`)
				}
			}))
			defer server.Close()
			upstream := &imageDeadlineHTTPUpstream{client: &http.Client{Timeout: 2 * time.Second}}
			svc := newOpenAIImagesTestService(upstream)
			svc.cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			svc.cfg.Security.URLAllowlist.AllowPrivateHosts = true
			account := newOpenAIImagesAPIKeyAccount()
			account.Credentials["base_url"] = server.URL
			body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat"}`)
			c, _ := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			taskCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			deadline, _ := taskCtx.Deadline()
			if guarded {
				taskCtx = keyrouting.WithFirstContentGuard(taskCtx, keyrouting.NewFirstContentGuard(5*time.Second))
			}
			taskCtx = pkghttputil.PreserveUpstreamDeadline(taskCtx)

			result, err := svc.ForwardImages(taskCtx, c, account, body, parsed, "")
			require.Error(t, err)
			require.Nil(t, result)
			require.NotNil(t, upstream.attempt)
			actualDeadline, ok := upstream.attempt.Deadline()
			require.True(t, ok, "an async task must not receive another drain window at its deadline")
			require.Equal(t, deadline, actualDeadline)
			require.ErrorIs(t, upstream.attempt.Err(), context.DeadlineExceeded)
		})
	}
}

func TestForwardOpenAIImagesAPIKey_OrdinaryClientDisconnectStillDrainsAndCompletes(t *testing.T) {
	clientCtx, cancelClient := context.WithCancel(context.Background())
	defer cancelClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		cancelClient()
		select {
		case <-req.Context().Done():
			return
		case <-time.After(20 * time.Millisecond):
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"aGVsbG8="}]}`)
	}))
	defer server.Close()
	upstream := &imageDeadlineHTTPUpstream{client: &http.Client{Timeout: 2 * time.Second}}
	svc := newOpenAIImagesTestService(upstream)
	svc.cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	svc.cfg.Security.URLAllowlist.AllowPrivateHosts = true
	svc.cfg.Gateway.ImageStreamDataIntervalTimeout = 1
	account := newOpenAIImagesAPIKeyAccount()
	account.Credentials["base_url"] = server.URL
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat"}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	result, err := svc.ForwardImages(clientCtx, c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.ErrorIs(t, clientCtx.Err(), context.Canceled)
	_, hasDeadline := upstream.attempt.Deadline()
	require.False(t, hasDeadline, "the synchronous billing drain remains cancellation-triggered")
}
