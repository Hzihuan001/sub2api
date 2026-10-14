package service

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayFailurePolicyMatchesHTTPPassthroughAndStream(t *testing.T) {
	for _, tc := range []struct {
		name, code, errorType string
		status                int
		want                  bool
	}{
		{"safety", "content_policy_violation", "invalid_request_error", 503, false},
		{"parameter", "invalid_parameter", "invalid_request_error", 502, false},
		{"context", "context_length_exceeded", "invalid_request_error", 500, false},
		{"credential", "invalid_api_key", "invalid_request_error", 400, true},
		{"quota", "insufficient_quota", "invalid_request_error", 429, true},
		{"model", "model_not_found", "invalid_request_error", 404, true},
		{"rate", "rate_limit_exceeded", "rate_limit_error", 429, true},
		{"server", "server_error", "server_error", 503, true},
		{"timeout", "", "server_error", 504, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"error":{"code":%q,"type":%q,"status_code":%d,"message":"fixture"}}`, tc.code, tc.errorType, tc.status))
			stream := []byte(fmt.Sprintf(`{"type":"response.failed","response":%s}`, body))
			svc := &OpenAIGatewayService{accountRepo: &overloadGatewayRepo{}}
			for _, kind := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: kind}
				require.Equal(t, tc.want, svc.shouldFailoverOpenAIUpstreamResponse(account, tc.status, "fixture", body), "HTTP %s", kind)
				require.Equal(t, tc.want, shouldFailoverOpenAIPassthroughResponse(account, tc.status, body), "passthrough %s", kind)
			}
			require.Equal(t, tc.want, openAIStreamFailedEventShouldFailover(stream, "fixture"), "response.failed")
			require.Equal(t, tc.want, openAIStreamErrorEventShouldFailover(body, "fixture"), "bare error")
		})
	}
}

func TestGatewayFailurePolicyStreamSemanticStatus(t *testing.T) {
	for _, tc := range []struct {
		code string
		want int
	}{
		{"insufficient_quota", http.StatusPaymentRequired},
		{"invalid_api_key", http.StatusUnauthorized},
		{"account_suspended", http.StatusForbidden},
		{"model_not_found", http.StatusNotFound},
	} {
		body := []byte(fmt.Sprintf(`{"type":"response.failed","response":{"error":{"code":%q,"type":"invalid_request_error"}}}`, tc.code))
		require.Equal(t, tc.want, openAIStreamFailureStatus(body, ""))
		require.False(t, openAIStreamFailedEventRetryableOnSameAccount(&Account{Credentials: map[string]any{"pool_mode": true}}, body, ""))
	}
}

func TestGatewayFailurePolicyStreamRequestDoesNotPenalizeAccount(t *testing.T) {
	// A non-nil limiter with no repository would panic if an auth/cooldown write
	// escaped the request-scoped guard. Wrapped safety errors must not reach it.
	svc := &OpenAIGatewayService{rateLimitService: &RateLimitService{}}
	for _, code := range []string{"content_policy_violation", "invalid_parameter", "context_length_exceeded"} {
		body := []byte(fmt.Sprintf(`{"type":"response.failed","response":{"error":{"code":%q,"status_code":401}}}`, code))
		_, blocked := svc.handleOpenAIStreamTerminalAccountSideEffects(nil, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, body, "", nil, "gpt-test")
		require.False(t, blocked)
	}
}
