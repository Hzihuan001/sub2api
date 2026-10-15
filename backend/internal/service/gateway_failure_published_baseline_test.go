package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const gatewayPublishedBaselineCommit = "989594099c1bd10db911c737449d15d2bb6149ed"
const gatewayPublishedBaselineFixture = "testdata/gateway_failure_published_989594099.json"

// This test observes public-to-the-package transport wrappers, not the new
// decision helpers. Its expected values were captured by the identical test
// source overlaid onto the verified published .5 source, where those wrappers
// still contain their original implementations. See the testdata provenance.
func TestGatewayFailurePublishedBaseline(t *testing.T) {
	data, err := os.ReadFile(gatewayPublishedBaselineFixture)
	require.NoError(t, err)
	var fixture gatewayPublishedFixture
	require.NoError(t, json.Unmarshal(data, &fixture))
	require.Equal(t, gatewayPublishedBaselineCommit, fixture.Commit)
	require.GreaterOrEqual(t, len(fixture.Cases), 30)
	require.LessOrEqual(t, len(fixture.Cases), 100)
	names := map[string]bool{}
	for _, sample := range fixture.Cases {
		require.False(t, names[sample.Input.Name], "duplicate fixture name")
		names[sample.Input.Name] = true
		t.Run(sample.Input.Name, func(t *testing.T) {
			require.Equal(t, sample.Observed, observeGatewayPublishedWrappers(t, sample.Input))
		})
	}
}

type gatewayPublishedFixture struct {
	Commit     string                   `json:"baseline_commit"`
	Command    string                   `json:"generation_command"`
	SourceHash map[string]string        `json:"baseline_source_sha256"`
	Cases      []gatewayPublishedSample `json:"cases"`
}

type gatewayPublishedSample struct {
	Input    gatewayPublishedInput       `json:"input"`
	Observed gatewayPublishedObservation `json:"observed"`
}

type gatewayPublishedInput struct {
	Name       string `json:"name"`
	Status     int    `json:"http_status"`
	Body       string `json:"body"`
	Message    string `json:"message"`
	FailedBody string `json:"response_failed_body"`
}

type gatewayPublishedObservation struct {
	HTTPClass      string                             `json:"http_class"`
	ErrorClass     string                             `json:"error_event_class"`
	FailedClass    string                             `json:"response_failed_class"`
	ErrorSemantic  int                                `json:"error_semantic_status"`
	FailedSemantic int                                `json:"response_failed_semantic_status"`
	FailedStatus   int                                `json:"response_failed_status"`
	ErrorFailover  bool                               `json:"error_event_failover"`
	FailedFailover bool                               `json:"response_failed_failover"`
	Accounts       map[string]gatewayPublishedAccount `json:"account_contexts"`
	Replay         [2]gatewayPublishedReplay          `json:"replay_without_then_with_same_account_flag"`
}

type gatewayPublishedAccount struct {
	HTTP        bool `json:"http"`
	ManagedHTTP bool `json:"managed_http"`
	Passthrough bool `json:"passthrough"`
}

type gatewayPublishedReplay struct {
	Status                 int    `json:"upstream_status"`
	SameAccount            bool   `json:"same_account_retry"`
	RequestScopedTransient bool   `json:"request_scoped_transient"`
	NextAccountAction      uint8  `json:"next_account_action"`
	Stage                  string `json:"stage,omitempty"`
	Scope                  string `json:"scope,omitempty"`
	Reason                 string `json:"reason,omitempty"`
	ClientStatus           int    `json:"client_status,omitempty"`
	ClientMessage          string `json:"client_message,omitempty"`
}

// Only non-nil repository presence is relevant to the managed-model policy.
// Embedding an uninitialized interface makes an unexpected repository call
// fail immediately instead of permitting I/O or duplicating repository logic.
type gatewayPublishedManagedRepo struct{ AccountRepository }

func observeGatewayPublishedWrappers(t *testing.T, input gatewayPublishedInput) gatewayPublishedObservation {
	t.Helper()
	body, failed := []byte(input.Body), []byte(input.FailedBody)
	result := gatewayPublishedObservation{
		HTTPClass:      gatewayPublishedClassName(classifyOpenAIGatewayFailure(input.Status, body, input.Message)),
		ErrorClass:     gatewayPublishedClassName(classifyOpenAIGatewayFailure(0, body, input.Message)),
		FailedClass:    gatewayPublishedClassName(classifyOpenAIGatewayFailure(0, failed, input.Message)),
		ErrorSemantic:  openAIStreamFailedEventSemanticStatus(body, input.Message),
		FailedSemantic: openAIStreamFailedEventSemanticStatus(failed, input.Message),
		FailedStatus:   openAIStreamFailureStatus(failed, input.Message),
		ErrorFailover:  openAIStreamErrorEventShouldFailover(body, input.Message),
		FailedFailover: openAIStreamFailedEventShouldFailover(failed, input.Message),
		Accounts:       map[string]gatewayPublishedAccount{},
	}
	bare := &OpenAIGatewayService{}
	managed := &OpenAIGatewayService{accountRepo: &gatewayPublishedManagedRepo{}}
	accounts := map[string]*Account{
		"none":             nil,
		"openai_api_key":   {Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		"openai_oauth":     {Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		"openai_pool":      {Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"pool_mode": true}},
		"openai_pool_400":  {Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"pool_mode": true, "pool_mode_retry_status_codes": []any{400, 418}}},
		"deepseek_api_key": {Platform: PlatformDeepseek, Type: AccountTypeAPIKey},
		"kimi_api_key":     {Platform: PlatformKimi, Type: AccountTypeAPIKey},
		"anthropic":        {Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
	}
	for name, account := range accounts {
		result.Accounts[name] = gatewayPublishedAccount{
			HTTP:        bare.shouldFailoverOpenAIUpstreamResponse(account, input.Status, input.Message, body),
			ManagedHTTP: managed.shouldFailoverOpenAIUpstreamResponse(account, input.Status, input.Message, body),
			Passthrough: shouldFailoverOpenAIPassthroughResponse(account, input.Status, body),
		}
	}
	for index, sameAccount := range []bool{false, true} {
		headers := http.Header{"Retry-After": {"7"}, "X-Request-Id": {"baseline-fixture"}}
		err := newOpenAIUpstreamFailoverError(input.Status, headers, body, input.Message, sameAccount)
		result.Replay[index] = gatewayPublishedReplay{
			Status: err.StatusCode, SameAccount: err.RetryableOnSameAccount,
			RequestScopedTransient: err.RequestScopedTransient, NextAccountAction: uint8(err.NextAccountAction),
			Stage: string(err.Stage), Scope: string(err.Scope), Reason: string(err.Reason),
			ClientStatus: err.ClientStatusCode, ClientMessage: err.ClientMessage,
		}
		require.Equal(t, input.Body, string(err.ResponseBody), "wrapper must preserve provider bytes")
		require.Equal(t, headers, err.ResponseHeaders, "wrapper must preserve response headers")
		err.ResponseHeaders.Set("Retry-After", "changed")
		require.Equal(t, "7", headers.Get("Retry-After"), "wrapper must clone the headers")
	}
	require.Equal(t, input.Body, string(body), "observation must not mutate the input")
	return result
}

func gatewayPublishedClassName(class openAIGatewayFailureClass) string {
	switch class {
	case openAIGatewayFailureUnknown:
		return "unknown"
	case openAIGatewayFailureRequest:
		return "request"
	case openAIGatewayFailureCredential:
		return "credential"
	case openAIGatewayFailureQuota:
		return "quota"
	case openAIGatewayFailureModelUnavailable:
		return "model_unavailable"
	default:
		return fmt.Sprintf("unrecognized:%d", class)
	}
}

func gatewayPublishedBaselineInputs() []gatewayPublishedInput {
	var cases []gatewayPublishedInput
	add := func(name string, status int, body, message string) {
		failed := body
		if json.Valid([]byte(body)) {
			failed = `{"type":"response.failed","response":` + body + `}`
		}
		cases = append(cases, gatewayPublishedInput{name, status, body, message, failed})
	}
	for _, status := range []int{200, 400, 401, 402, 403, 404, 405, 408, 413, 418, 429, 500, 502, 503, 504, 520, 524, 529} {
		add(fmt.Sprintf("unknown_http_%d", status), status, `{}`, "")
	}
	add("safety_over_server_status", 503, `{"error":{"code":"content_policy_violation","type":"invalid_request_error"}}`, "")
	add("parameter_over_server_status", 502, `{"error":{"code":"invalid_parameter","type":"invalid_request_error"}}`, "")
	add("context_over_body_limit", 413, `{"error":{"code":"context_length_exceeded"}}`, "")
	add("permission_request", 403, `{"error":{"type":"permission_error","status_code":403}}`, "")
	add("permission_ip_blocked", 403, `{"error":{"type":"permission_error","details":{"reason":"ip_blocked"}}}`, "")
	add("generic_invalid_request", 400, `{"error":{"type":"invalid_request_error"}}`, "")
	add("generic_invalid_request_auth_status", 401, `{"error":{"type":"invalid_request_error"}}`, "")
	add("echoed_credentials_are_not_signals", 503, `{"error":{"code":"invalid_parameter","message":"bad schema"},"request":{"code":"invalid_api_key"}}`, "")
	add("credential_under_400", 400, `{"error":{"code":"invalid_api_key","type":"invalid_request_error"}}`, "")
	add("credential_under_503", 503, `{"error":{"type":"authentication_error"}}`, "")
	add("account_suspended", 403, `{"error":{"code":"account_suspended"}}`, "")
	add("workspace_deactivated", 400, `{"error":{"code":"deactivated_workspace"}}`, "")
	add("hard_quota", 429, `{"error":{"code":"insufficient_quota","type":"invalid_request_error"}}`, "")
	add("billing_hard_limit", 402, `{"error":{"code":"billing_hard_limit_reached"}}`, "")
	for _, status := range []int{400, 404, 500} {
		add(fmt.Sprintf("model_unavailable_%d", status), status, `{"error":{"code":"model_not_found"}}`, "")
	}
	add("compatible_unknown_provider", 400, `{"error":{"message":"unknown provider for model fixture"}}`, "")
	for _, status := range []int{400, 503, 504} {
		add(fmt.Sprintf("capacity_shed_%d", status), status, `{"error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`, "")
	}
	add("slow_down", 429, `{"error":{"code":"slow_down"}}`, "")
	add("plain_text_capacity", 503, "Our servers are currently overloaded. Please try again later.", "")
	add("transient_processing", 400, `{"error":{"message":"An error occurred while processing your request"}}`, "An error occurred while processing your request")
	add("rate_limit", 429, `{"error":{"code":"rate_limit_exceeded","type":"rate_limit_error"}}`, "")
	add("embedded_rate_status", 400, `{"error":{"type":"invalid_request_error","status_code":429}}`, "")
	add("embedded_server_status", 400, `{"error":{"status":503}}`, "")
	add("embedded_auth_status", 400, `{"error":{"type":"invalid_request_error","status_code":401}}`, "")
	add("body_limit", 413, `{"error":{"message":"Request payload is too large"}}`, "Request payload is too large")
	add("empty_payload", 400, "", "")
	add("malformed_payload", 502, `{"error":`, "")
	add("unknown_provider_message", 400, `{"error":{"message":"provider refused"}}`, "provider refused")
	return cases
}

func TestGatewayFailurePublishedBaselineCapture(t *testing.T) {
	if os.Getenv("SUB2API_CAPTURE_PUBLISHED_FAILURES") != gatewayPublishedBaselineCommit {
		t.Skip("capture is opt-in and only runs against verified published source")
	}
	_, err := os.Stat("gateway_failure_decision.go")
	require.True(t, os.IsNotExist(err), "refuse to generate expectations from the refactored decision implementation")
	hashes := map[string]string{
		"gateway_failure_policy.go":         "6ba8c049685bd5d4a4995d46439d39d1fbc391f8b3b5e151b34b80c006a5a309",
		"openai_gateway_passthrough.go":     "1c7458480660fbcf60652684cb111b244348587ffdd1a64fd636fb43d8024269",
		"openai_gateway_upstream_errors.go": "02a91e8385a8cffa8dc7bce889fc947168087dc7bf450e1d60ae13134c8c87ec",
	}
	for file, expected := range hashes {
		data, readErr := os.ReadFile(file)
		require.NoError(t, readErr)
		require.Equal(t, expected, fmt.Sprintf("%x", sha256.Sum256(data)), "published source fingerprint: %s", file)
	}
	fixture := gatewayPublishedFixture{
		Commit: gatewayPublishedBaselineCommit, SourceHash: hashes,
		Command: "SUB2API_CAPTURE_PUBLISHED_FAILURES=989594099c1bd10db911c737449d15d2bb6149ed go test -overlay=<capture-overlay.json> -tags=unit ./internal/service -run '^TestGatewayFailurePublishedBaselineCapture$' -count=1 -v",
	}
	for _, input := range gatewayPublishedBaselineInputs() {
		fixture.Cases = append(fixture.Cases, gatewayPublishedSample{Input: input, Observed: observeGatewayPublishedWrappers(t, input)})
	}
	data, err := json.Marshal(fixture)
	require.NoError(t, err)
	fmt.Printf("PUBLISHED_FAILURE_BASELINE_JSON=%s\n", data)
}
