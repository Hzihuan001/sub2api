package service

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayFailurePolicyRequestRejectionPrecedesStatus(t *testing.T) {
	bodies := []string{
		`{"error":{"code":"content_policy_violation","message":"blocked"}}`,
		`{"response":{"error":{"type":"safety_error","message":"blocked"}}}`,
		`{"error":{"code":"cyber_policy","message":"blocked"}}`,
		`{"error":{"code":"context_length_exceeded","message":"too long"}}`,
		`{"error":{"code":"invalid_function_parameters","type":"invalid_request_error","message":"bad schema"}}`,
		`{"error":{"code":"unknown_parameter","message":"Unknown parameter: invalid_api_key"}}`,
		`{"error":{"message":"Invalid schema for function 'test': unsupported shape"}}`,
		`{"error":{"message":"This request violates our content policy"}}`,
		`{"error":{"type":"permission_error","message":"request access denied"}}`,
		`{"error":{"code":"invalid_api_key","type":"content_policy_violation"}}`,
		`{"error":{"code":"invalid_parameter","message":"Our servers are currently overloaded"}}`,
	}
	svc := &OpenAIGatewayService{accountRepo: &modelNotFoundManagedAccountRepo{}}
	account := newOpenAIUpstreamErrorTestAccount()
	for i, body := range bodies {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			for _, status := range []int{400, 401, 402, 403, 404, 413, 429, 500, 502, 503, 529} {
				require.Equal(t, openAIGatewayFailureRequest, classifyOpenAIGatewayFailure(status, []byte(body), ""), "status %d", status)
				require.False(t, svc.shouldFailoverOpenAIUpstreamResponse(account, status, "", []byte(body)), "status %d", status)
			}
			require.True(t, isOpenAIRequestScopedFailure([]byte(body), ""))
		})
	}
}

func TestOpenAIStoredResponseReferenceRejectionIsNarrow(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"explicit code", http.StatusBadRequest, `{"error":{"code":"previous_response_not_found","message":"not found"}}`, true},
		{"explicit param", http.StatusBadRequest, `{"error":{"param":"previous_response_id","message":"not available for this user"}}`, true},
		{"response envelope", http.StatusNotFound, `{"response":{"error":{"code":"previous_response_id_not_found"}}}`, true},
		{"ordinary invalid request", http.StatusBadRequest, `{"error":{"param":"input","message":"previous_response_id is not available"}}`, false},
		{"generic bad request", http.StatusBadRequest, `{"error":{"message":"invalid request"}}`, false},
		{"wrong status", http.StatusForbidden, `{"error":{"code":"previous_response_not_found"}}`, false},
		{"non json", http.StatusBadRequest, `previous_response_id not found`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsOpenAIStoredResponseReferenceRejection(tt.status, []byte(tt.body)))
		})
	}
}

func TestOpenAIStoredResponseReferenceRejectionSkipsAccountFailureSideEffects(t *testing.T) {
	requestWithReference := []byte(`{"model":"gpt-5.6-sol","previous_response_id":"resp_old"}`)
	staleReference := []byte(`{"error":{"code":"previous_response_not_found","message":"Response not found"}}`)
	require.False(t, shouldApplyOpenAIFailoverSideEffects(requestWithReference, http.StatusBadRequest, staleReference))
	require.True(t, shouldApplyOpenAIFailoverSideEffects(requestWithReference, http.StatusBadRequest, []byte(`{"error":{"code":"invalid_parameter"}}`)))
	require.True(t, shouldApplyOpenAIFailoverSideEffects([]byte(`{"model":"gpt-5.6-sol","input":"hello"}`), http.StatusBadRequest, staleReference))
}

func TestOpenAIGatewayFailurePolicyExplicitAccountSignalsIgnoreStatus(t *testing.T) {
	tests := []struct {
		name string
		body string
		want openAIGatewayFailureClass
	}{
		{"invalid key in generic wrapper", `{"error":{"code":"invalid_api_key","type":"invalid_request_error","message":"credential rejected"}}`, openAIGatewayFailureCredential},
		{"revoked token", `{"response":{"error":{"code":"token_revoked"}}}`, openAIGatewayFailureCredential},
		{"authentication type", `{"error":{"type":"authentication_error"}}`, openAIGatewayFailureCredential},
		{"workspace deactivated", `{"detail":{"code":"deactivated_workspace"}}`, openAIGatewayFailureCredential},
		{"organization suspended", `{"error":{"code":"organization_suspended"}}`, openAIGatewayFailureCredential},
		{"key overrides permission wrapper", `{"error":{"code":"invalid_api_key","type":"permission_error"}}`, openAIGatewayFailureCredential},
		{"hard quota without reset", `{"error":{"code":"insufficient_quota","type":"invalid_request_error"}}`, openAIGatewayFailureQuota},
		{"hard quota as type", `{"error":{"type":"insufficient_quota"}}`, openAIGatewayFailureQuota},
		{"billing cap", `{"response":{"error":{"code":"billing_hard_limit_reached"}}}`, openAIGatewayFailureQuota},
		{"balance", `{"error":{"code":"insufficient_balance"}}`, openAIGatewayFailureQuota},
	}
	svc := &OpenAIGatewayService{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, status := range []int{400, 401, 402, 403, 404, 429, 502} {
				require.Equal(t, tt.want, classifyOpenAIGatewayFailure(status, []byte(tt.body), ""))
				require.True(t, svc.shouldFailoverOpenAIUpstreamResponse(newOpenAIUpstreamErrorTestAccount(), status, "", []byte(tt.body)))
			}
			require.False(t, isOpenAIRequestScopedFailure([]byte(tt.body), ""))
		})
	}
}

func TestOpenAIGatewayFailurePolicyDoesNotTrustEchoedAccountSignals(t *testing.T) {
	for _, body := range []string{
		`{"error":{"type":"invalid_request_error","message":"Invalid input"},"input":{"code":"invalid_api_key","type":"insufficient_quota"}}`,
		`{"error":{"code":"unknown_parameter","message":"Unknown parameter: token_revoked account_disabled insufficient_quota model_not_found"}}`,
		`{"error":{"type":"invalid_request_error","message":"The input contains invalid_api_key and insufficient_quota"}}`,
	} {
		require.Equal(t, openAIGatewayFailureRequest, classifyOpenAIGatewayFailure(http.StatusBadGateway, []byte(body), ""))
	}
	for _, body := range []string{
		`{"error":{"message":"account is disabled"}}`,
		`{"error":{"message":"insufficient_quota"}}`,
		`{"error":{"message":"temporary upstream outage"},"input":{"code":"content_policy_violation"}}`,
		`{"error":{"message":"temporary upstream outage"},"debug":{"error":{"code":"invalid_api_key"}}}`,
	} {
		require.Equal(t, openAIGatewayFailureUnknown, classifyOpenAIGatewayFailure(http.StatusBadGateway, []byte(body), ""))
	}
}

func TestOpenAIGatewayFailurePolicyModelAvailabilityKeepsManagedScope(t *testing.T) {
	managed := &OpenAIGatewayService{accountRepo: &modelNotFoundManagedAccountRepo{}}
	bare := &OpenAIGatewayService{}
	for _, body := range []string{
		`{"error":{"code":"model_not_found","type":"invalid_request_error"}}`,
		`{"error":{"type":"model_not_found","message":"unavailable"}}`,
		`{"response":{"error":{"code":"model_not_found","type":"invalid_request_error"}}}`,
		`{"error":{"message":"unknown provider for model test-model"}}`,
	} {
		for _, status := range []int{http.StatusBadRequest, http.StatusNotFound} {
			require.Equal(t, openAIGatewayFailureModelUnavailable, classifyOpenAIGatewayFailure(status, []byte(body), ""))
			require.True(t, managed.shouldFailoverOpenAIUpstreamResponse(newOpenAIUpstreamErrorTestAccount(), status, "", []byte(body)))
			require.False(t, bare.shouldFailoverOpenAIUpstreamResponse(newOpenAIUpstreamErrorTestAccount(), status, "", []byte(body)))
			require.False(t, managed.shouldFailoverOpenAIUpstreamResponse(&Account{Platform: PlatformAnthropic}, status, "", []byte(body)))
			require.False(t, managed.shouldFailoverOpenAIUpstreamResponse(nil, status, "", []byte(body)))
		}
	}
	for _, body := range []string{`{"error":{"message":"Not Found"}}`, `{"error":{"code":"route_not_found"}}`, `404 page not found`} {
		require.False(t, managed.shouldFailoverOpenAIUpstreamResponse(newOpenAIUpstreamErrorTestAccount(), http.StatusNotFound, "", []byte(body)))
	}
}

func TestOpenAIGatewayFailurePolicyPreservesTransientAndBodyLimitPaths(t *testing.T) {
	tests := []struct {
		status int
		body   string
	}{
		{400, `{"error":{"type":"invalid_request_error","code":"server_is_overloaded"}}`},
		{400, `{"error":{"type":"invalid_request_error","message":"An error occurred while processing your request"}}`},
		{429, `{"error":{"type":"invalid_request_error","code":"rate_limit_exceeded"}}`},
		{429, `{"error":{"type":"invalid_request_error","message":"try later"}}`},
		{413, `{"error":{"type":"invalid_request_error","message":"Request payload is too large"}}`},
		{502, `{"error":{"type":"server_error","message":"temporarily unavailable"}}`},
		{503, `{"error":{"type":"invalid_request_error","code":"server_error"}}`},
	}
	for _, tt := range tests {
		require.Equal(t, openAIGatewayFailureUnknown, classifyOpenAIGatewayFailure(tt.status, []byte(tt.body), ""))
		require.True(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(newOpenAIUpstreamErrorTestAccount(), tt.status, "", []byte(tt.body)))
	}
	for _, body := range []string{
		`{"error":{"type":"invalid_request_error","status_code":429,"message":"try later"}}`,
		`{"response":{"error":{"type":"invalid_request_error","code":"usage_limit_reached"}}}`,
	} {
		require.Equal(t, openAIGatewayFailureUnknown, classifyOpenAIGatewayFailure(200, []byte(body), ""))
		require.False(t, isOpenAIRequestScopedFailure([]byte(body), ""))
	}
}

func TestOpenAIGatewayFailurePolicyStopsSameAccountReplay(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"hard quota", 429, `{"error":{"code":"insufficient_quota"}}`},
		{"missing model", 404, `{"error":{"code":"model_not_found"}}`},
		{"invalid credential", 400, `{"error":{"code":"invalid_api_key"}}`},
		{"gateway timeout", 504, `{"error":{"code":"server_error"}}`},
		{"edge timeout", 524, `{"error":{"code":"server_error"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newOpenAIUpstreamFailoverError(tt.status, nil, []byte(tt.body), "", true)
			require.False(t, err.RetryableOnSameAccount)
			require.False(t, err.RequestScopedTransient)
			require.True(t, err.ShouldRetryNextAccount())
			if tt.name == "invalid credential" {
				require.True(t, err.IsCredentialFailure())
				require.Equal(t, http.StatusBadGateway, err.ClientStatusCode)
			}
		})
	}

	svc := &OpenAIGatewayService{}
	account := newOpenAIUpstreamErrorTestAccount()
	for _, status := range []int{429, 502, 503, 529} {
		err := svc.newOpenAIAccountFailoverErrorWithClassificationHeaders(account, status, http.Header{"Retry-After": {"2"}}, nil, []byte(`{"error":{"message":"temporary failure"}}`), "", true, true)
		require.False(t, err.RetryableOnSameAccount)
		require.True(t, err.SameAccountRetryDeadline.IsZero())
		require.Zero(t, err.SameAccountRetryDelay)
	}
	// A plain recoverable 401 still leaves existing token-refresh handling in
	// charge; an overload below its circuit threshold retains bounded retries.
	require.True(t, newOpenAIUpstreamFailoverError(401, nil, nil, "", true).RetryableOnSameAccount)
	require.True(t, newOpenAIUpstreamFailoverError(529, nil, []byte(`{"error":{"code":"server_is_overloaded"}}`), "", true).RetryableOnSameAccount)
}

func TestOpenAIGatewayFailurePolicyGenericWrapperPreservesAccountStatus(t *testing.T) {
	for _, status := range []int{401, 402, 403} {
		body := []byte(`{"error":{"type":"invalid_request_error","message":"workspace forbidden by policy"}}`)
		require.Equal(t, openAIGatewayFailureUnknown, classifyOpenAIGatewayFailure(status, body, ""))
		require.True(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(newOpenAIUpstreamErrorTestAccount(), status, "", body))
	}
	for _, body := range []string{
		`{"type":"error","error":{"type":"invalid_request_error","status_code":401}}`,
		`{"type":"response.failed","response":{"error":{"type":"invalid_request_error","status":402}}}`,
	} {
		require.Equal(t, openAIGatewayFailureUnknown, classifyOpenAIGatewayFailure(0, []byte(body), ""))
	}
	// An explicit request rejection always wins, even when both the HTTP and
	// nested status try to describe it as an authentication failure.
	body := []byte(`{"error":{"code":"content_policy_violation","type":"invalid_request_error","status_code":401}}`)
	require.Equal(t, openAIGatewayFailureRequest, classifyOpenAIGatewayFailure(403, body, ""))
}

func TestOpenAIGatewayFailurePolicyPreservesNarrowLegacyAccountMessages(t *testing.T) {
	for _, message := range []string{"Your organization has been disabled.", "Identity verification is required to use this service."} {
		body := []byte(`{"error":{"type":"invalid_request_error","message":"` + message + `"}}`)
		require.Equal(t, openAIGatewayFailureUnknown, classifyOpenAIGatewayFailure(400, body, ""))
		require.Equal(t, openAIGatewayFailureRequest, classifyOpenAIGatewayFailure(0, body, ""), "do not infer stream credential health from free text")
	}
	for _, body := range []string{
		`{"error":{"code":"unknown_parameter","type":"invalid_request_error","message":"Unknown parameter: organization has been disabled"}}`,
		`{"error":{"type":"invalid_request_error","message":"Invalid input"},"input":{"message":"identity verification is required"}}`,
		`{"error":{"type":"invalid_request_error","message":"Invalid input says organization has been disabled"}}`,
	} {
		require.Equal(t, openAIGatewayFailureRequest, classifyOpenAIGatewayFailure(400, []byte(body), ""))
	}
}
