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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type overloadGatewayRepo struct {
	AccountRepository
	overloadCalls int
}

func (r *overloadGatewayRepo) SetOverloaded(context.Context, int64, time.Time) error {
	r.overloadCalls++
	return nil
}

const overloadGatewayPayload = `{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"Our servers are currently overloaded. Please try again later."}},"sequence_number":0}`

func newOverloadGatewayFixture() (*OpenAIGatewayService, *overloadGatewayRepo, *Account) {
	repo := &overloadGatewayRepo{}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	limiter := NewRateLimitService(repo, nil, cfg, nil, nil)
	svc := &OpenAIGatewayService{cfg: cfg, rateLimitService: limiter}
	limiter.SetAccountRuntimeBlocker(svc)
	account := &Account{
		ID: 49, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 10, Priority: 0,
		Credentials: map[string]any{"pool_mode": true, "pool_mode_retry_count": 10},
	}
	return svc, repo, account
}

func TestOpenAIRepeatedOverloadWithoutGroupRuleDoesNotPauseAccount(t *testing.T) {
	svc, repo, account := newOverloadGatewayFixture()
	for attempt := 1; attempt <= 4; attempt++ {
		// Each invocation represents a different incoming request; no handler
		// request-local retry map is shared across these failures. Without an
		// explicit group status rule, repeated overloads remain request-local.
		blocked := svc.handleOpenAIAccountUpstreamError(context.Background(), account, 503, nil, []byte(overloadGatewayPayload))
		err := svc.newOpenAIAccountFailoverError(account, 503, nil, []byte(overloadGatewayPayload), "", blocked, true)
		require.False(t, blocked)
		require.True(t, err.RetryableOnSameAccount)
		require.True(t, err.ShouldRetryNextAccount())
		require.True(t, err.RequestScopedTransient)
	}
	require.Zero(t, repo.overloadCalls, "no implicit account overload cooldown")
	require.Nil(t, account.OverloadUntil, "do not mutate the shared scheduler account snapshot")
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-test"))
}

func TestOpenAIRepeatedOverloadIgnoresGenericErrorsAndEchoedContent(t *testing.T) {
	svc, repo, account := newOverloadGatewayFixture()
	for _, body := range []string{
		`{"error":{"code":"server_error","message":"internal server error"}}`,
		`{"error":{"message":"invalid request"},"prompt":"Our servers are currently overloaded"}`,
	} {
		for i := 0; i < 4; i++ {
			_, blocked := svc.handleOpenAIStreamTerminalAccountSideEffects(nil, account, []byte(body), "", nil)
			require.False(t, blocked)
		}
	}
	require.Zero(t, repo.overloadCalls)
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-test"))
}

func TestOpenAIRepeatedOverloadAcrossStreams(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, output := range []bool{false, true} {
			for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
				name := map[bool]string{false: "native", true: "passthrough"}[passthrough] + "/" +
					map[bool]string{false: "queue_heartbeat_only", true: "semantic_output"}[output] + "/" + accountType
				t.Run(name, func(t *testing.T) {
					svc, repo, account := newOverloadGatewayFixture()
					account.Type = accountType
					for attempt := 1; attempt <= 3; attempt++ {
						rec := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(rec)
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
						n, err := c.Writer.WriteString(":\n\n")
						require.NoError(t, err)
						c.Set(GatewayStreamHeartbeatBytesKey, n)
						stream := ""
						if output {
							stream = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
							// OAuth providers commonly send both terminal error frames.
							// They must still count as one failed upstream attempt.
							stream += "event: error\ndata: {\"type\":\"error\",\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"Our servers are currently overloaded\"}}\n\n"
						}
						stream += "event: response.failed\ndata: " + overloadGatewayPayload + "\n\n"
						resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))}
						if passthrough {
							_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "gpt-test", "gpt-test")
						} else {
							_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-test", "gpt-test")
						}
						require.Error(t, err)
						var failoverErr *UpstreamFailoverError
						if output {
							require.False(t, errors.As(err, &failoverErr), "do not replay a partially delivered response")
							require.Contains(t, rec.Body.String(), "partial")
						} else {
							require.ErrorAs(t, err, &failoverErr)
							require.True(t, failoverErr.RetryableOnSameAccount, "without a group rule, repeated overload must not disable same-account retry")
							require.Equal(t, ":\n\n", rec.Body.String(), "failed attempt must not leak generated content")
						}
						require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-test"))
					}
					require.Zero(t, repo.overloadCalls)
				})
			}
		}
	}
}

func TestOpenAIQueueHeartbeatAccountingKeepsSemanticOutput(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	n, err := c.Writer.WriteString(":\n\n")
	require.NoError(t, err)
	c.Set(GatewayStreamHeartbeatBytesKey, n)
	require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c))
	n, err = c.Writer.WriteString("data: partial\n\n")
	require.NoError(t, err)
	require.Equal(t, n, OpenAICompactKeepaliveAdjustedWrittenSize(c))
}
