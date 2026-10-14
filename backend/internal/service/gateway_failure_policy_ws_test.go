package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type gatewayFailurePolicyWSRepo struct {
	openAIWSIngressCapacityShedRepo
	rateLimits  int
	modelLimits int
	model       string
}

func (r *gatewayFailurePolicyWSRepo) SetRateLimited(context.Context, int64, time.Time) error {
	r.rateLimits++
	return nil
}

func (r *gatewayFailurePolicyWSRepo) SetModelRateLimit(_ context.Context, _ int64, model string, _ time.Time, _ ...string) error {
	r.modelLimits++
	r.model = model
	return nil
}

func TestGatewayFailurePolicyWSAppliesQuotaAndModelTerminalState(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		quota      bool
	}{
		{"quota bare error", `{"type":"error","error":{"code":"insufficient_quota","type":"invalid_request_error"}}`, true},
		{"quota failed", `{"type":"response.failed","response":{"error":{"code":"insufficient_quota","type":"invalid_request_error"}}}`, true},
		{"model bare error", `{"type":"error","error":{"code":"model_not_found","type":"invalid_request_error"}}`, false},
		{"model failed", `{"type":"response.failed","response":{"error":{"code":"model_not_found","type":"invalid_request_error"}}}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &gatewayFailurePolicyWSRepo{}
			cfg := newOpenAIWSV2TestConfig()
			limiter := NewRateLimitService(repo, nil, cfg, nil, nil)
			svc := &OpenAIGatewayService{cfg: cfg, rateLimitService: limiter}
			limiter.SetAccountRuntimeBlocker(svc)
			account := &Account{ID: 5410, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
				Credentials: map[string]any{"pool_mode": true}}
			require.True(t, svc.handleOpenAIWSFailureAccountSideEffects(context.Background(), account, "gpt-test", nil, []byte(tt.body)))
			if tt.quota {
				require.Equal(t, 1, repo.rateLimits)
				require.Zero(t, repo.modelLimits)
			} else {
				require.Equal(t, 1, repo.modelLimits)
				require.Equal(t, "gpt-test", repo.model)
				require.Zero(t, repo.rateLimits)
			}
		})
	}
}

func TestGatewayFailurePolicyWSIgnoresRequestErrorsAndGeneric404(t *testing.T) {
	svc := &OpenAIGatewayService{rateLimitService: &RateLimitService{}}
	account := &Account{ID: 5411, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for _, body := range []string{
		`{"type":"error","error":{"code":"content_policy_violation","type":"rate_limit_error","status_code":429}}`,
		`{"type":"error","error":{"code":"invalid_parameter","type":"server_error","status_code":502}}`,
		`{"type":"response.failed","response":{"error":{"code":"context_length_exceeded","status_code":401}}}`,
		`{"type":"response.failed","response":{"error":{"status_code":404,"message":"Not Found"}}}`,
	} {
		require.False(t, svc.handleOpenAIWSFailureAccountSideEffects(context.Background(), account, "gpt-test", http.Header{}, []byte(body)))
		require.False(t, svc.handleOpenAIWSErrorEventTransientFailure(context.Background(), account, "gpt-test", nil, []byte(body)))
	}
	body := []byte(`{"type":"error","error":{"code":"content_policy_violation","type":"rate_limit_error","message":"rate limit exceeded"}}`)
	require.False(t, svc.persistOpenAIWSRateLimitSignal(context.Background(), account, nil, body, "content_policy_violation", "rate_limit_error", "rate limit exceeded", "gpt-test"))
}
