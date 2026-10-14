package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayFailureDecisionPreservesEnvelopeDistinctions(t *testing.T) {
	tests := []struct {
		name, body, message                              string
		status                                           int
		allowModel                                       bool
		wantHTTP, wantPassthrough, wantFailed, wantError bool
	}{
		{"unknown empty", `{}`, "", 400, false, false, false, true, false},
		{"unknown message", `{"error":{"message":"provider refused"}}`, "provider refused", 400, false, false, false, true, false},
		{"request rejection", `{"error":{"code":"invalid_parameter"}}`, "", 503, true, false, false, false, false},
		{"content rejection wins", `{"error":{"code":"invalid_api_key","type":"content_policy_violation","status_code":401}}`, "", 503, true, false, false, false, false},
		{"credential", `{"error":{"code":"invalid_api_key"}}`, "", 400, false, true, true, true, true},
		{"quota", `{"error":{"code":"insufficient_quota"}}`, "", 429, false, true, true, true, true},
		{"model managed", `{"error":{"code":"model_not_found"}}`, "", 404, true, true, true, true, true},
		{"model bare", `{"error":{"code":"model_not_found"}}`, "", 404, false, false, false, true, true},
		{"model 500 fallback", `{"error":{"code":"model_not_found"}}`, "", 500, false, true, false, true, true},
		{"body limit", `{"error":{"message":"Request payload is too large"}}`, "", 413, false, true, true, true, false},
		{"nested server", `{"error":{"status_code":503}}`, "", 400, false, false, false, true, true},
		{"nested rate", `{"error":{"type":"invalid_request_error","status_code":429}}`, "", 400, false, false, false, true, true},
		{"overload", `{"error":{"code":"server_is_overloaded"}}`, "", 400, false, true, true, true, true},
		{"forbidden request", `{"error":{"type":"permission_error","status_code":403}}`, "", 403, false, false, false, false, false},
		{"server signal", `{"error":{"type":"upstream_error"}}`, "", 500, false, true, true, true, true},
		{"temporary message", `{"error":{"message":"temporary failure"}}`, "temporary failure", 400, false, false, false, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(tt.body)
			before := append([]byte(nil), body...)
			for _, envelope := range []struct {
				kind openAIGatewayFailureDecisionKind
				want bool
			}{
				{openAIGatewayFailureHTTP, tt.wantHTTP},
				{openAIGatewayFailurePassthrough, tt.wantPassthrough},
				{openAIGatewayFailureResponseFailed, tt.wantFailed},
				{openAIGatewayFailureErrorEvent, tt.wantError},
			} {
				input := openAIGatewayFailureDecisionInput{kind: envelope.kind, statusCode: tt.status, body: body, message: tt.message, allowModelFailover: tt.allowModel}
				first := decideOpenAIGatewayFailure(input)
				require.Equal(t, envelope.want, first.shouldFailover, "envelope %d", envelope.kind)
				require.Equal(t, first, decideOpenAIGatewayFailure(input), "pure result must be repeatable")
				require.Equal(t, before, body, "policy must not rewrite provider bytes")
			}
		})
	}
}

func TestGatewayFailureDecisionPreservesHTTPAccountFacts(t *testing.T) {
	input := openAIGatewayFailureDecisionInput{kind: openAIGatewayFailureHTTP, statusCode: http.StatusBadRequest,
		body: []byte(`{"error":{"message":"unknown provider for model test"}}`)}
	require.False(t, decideOpenAIGatewayFailure(input).shouldFailover)
	input.allowModelFailover = true
	require.True(t, decideOpenAIGatewayFailure(input).shouldFailover)
	input.allowModelFailover = false
	input.body = []byte(`{"error":{"message":"upstream rejected"}}`)
	input.poolRetryable = true
	require.True(t, decideOpenAIGatewayFailure(input).shouldFailover)
	input.body = []byte(`{"error":{"code":"invalid_parameter"}}`)
	require.False(t, decideOpenAIGatewayFailure(input).shouldFailover, "request-scoped failure wins over pool policy")
}

func TestGatewayFailureDecisionStreamStatusUsesPayloadNotTransport200(t *testing.T) {
	for _, tt := range []struct {
		code string
		want int
	}{
		{"insufficient_quota", http.StatusPaymentRequired},
		{"invalid_api_key", http.StatusUnauthorized},
		{"account_suspended", http.StatusForbidden},
		{"model_not_found", http.StatusNotFound},
	} {
		for _, kind := range []openAIGatewayFailureDecisionKind{openAIGatewayFailureResponseFailed, openAIGatewayFailureErrorEvent} {
			decision := decideOpenAIGatewayFailure(openAIGatewayFailureDecisionInput{kind: kind, statusCode: http.StatusOK,
				body: []byte(`{"response":{"error":{"code":"` + tt.code + `","type":"invalid_request_error"}}}`)})
			require.Equal(t, tt.want, decision.semanticStatus)
			require.True(t, decision.shouldFailover)
		}
	}
}

func TestGatewayFailureDecisionMatchesTransportAdapters(t *testing.T) {
	managed := &OpenAIGatewayService{accountRepo: &modelNotFoundManagedAccountRepo{}}
	bare := &OpenAIGatewayService{}
	accounts := []*Account{
		nil,
		{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{Platform: PlatformDeepseek, Type: AccountTypeAPIKey},
		{Platform: PlatformKimi, Type: AccountTypeAPIKey},
		{Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
		{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"pool_mode": true}},
	}
	bodies := []string{
		`{}`,
		`{"error":{"message":"temporary failure"}}`,
		`{"error":{"code":"invalid_parameter"}}`,
		`{"error":{"code":"model_not_found"}}`,
		`{"error":{"code":"account_suspended"}}`,
		`{"error":{"code":"invalid_api_key"}}`,
		`{"error":{"code":"insufficient_quota"}}`,
		`{"error":{"code":"server_is_overloaded"}}`,
		`{"error":{"type":"permission_error","status_code":403}}`,
		`{"response":{"error":{"type":"invalid_request_error","status_code":429}}}`,
	}
	for _, status := range []int{200, 400, 401, 402, 403, 404, 413, 429, 500, 503, 504, 524, 529} {
		for _, bodyText := range bodies {
			body := []byte(bodyText)
			for _, account := range accounts {
				for _, svc := range []*OpenAIGatewayService{managed, bare} {
					input := openAIGatewayFailureDecisionInput{kind: openAIGatewayFailureHTTP, statusCode: status, body: body,
						allowModelFailover: svc.accountRepo != nil && account != nil && account.IsOpenAICompatible() &&
							(status == http.StatusBadRequest || status == http.StatusNotFound)}
					require.Equal(t, svc.shouldFailoverOpenAIUpstreamResponse(account, status, "", body),
						decideOpenAIGatewayFailure(input).shouldFailover, "HTTP status=%d body=%s account=%v", status, bodyText, account)
				}
				input := openAIGatewayFailureDecisionInput{kind: openAIGatewayFailurePassthrough, statusCode: status, body: body,
					allowModelFailover: account != nil && account.IsOpenAICompatible(),
					poolRetryable:      account != nil && account.IsPoolMode() && account.IsPoolModeRetryableStatus(status)}
				require.Equal(t, shouldFailoverOpenAIPassthroughResponse(account, status, body),
					decideOpenAIGatewayFailure(input).shouldFailover, "passthrough status=%d body=%s account=%v", status, bodyText, account)
			}
			failed := decideOpenAIGatewayFailure(openAIGatewayFailureDecisionInput{kind: openAIGatewayFailureResponseFailed, statusCode: status, body: body})
			require.Equal(t, openAIStreamFailedEventShouldFailover(body, ""), failed.shouldFailover)
			streamError := decideOpenAIGatewayFailure(openAIGatewayFailureDecisionInput{kind: openAIGatewayFailureErrorEvent, statusCode: status, body: body})
			require.Equal(t, openAIStreamErrorEventShouldFailover(body, ""), streamError.shouldFailover)
		}
	}
}
