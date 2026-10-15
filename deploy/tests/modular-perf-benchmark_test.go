package service

import (
	"net/http"
	"testing"
)

// Overlay this identical file into baseline and candidate packages. Measure
// the real unchanged host API, not a new helper that only one tree contains.
var modularPerfBool bool
var modularPerfError *UpstreamFailoverError

func BenchmarkModularHostFailureWrappers(b *testing.B) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"overload", 503, `{"error":{"code":"server_is_overloaded","message":"overloaded"}}`},
		{"quota", 429, `{"error":{"code":"insufficient_quota"}}`},
		{"model", 404, `{"error":{"code":"model_not_found"}}`},
		{"request", 400, `{"error":{"code":"invalid_parameter","message":"invalid parameter"}}`},
		{"safety", 403, `{"error":{"code":"content_policy_violation"}}`},
	}
	service := &OpenAIGatewayService{}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"pool_mode": true}}
	headers := http.Header{"Retry-After": {"7"}}
	for _, tc := range cases {
		body := []byte(tc.body)
		failed := []byte(`{"type":"response.failed","response":` + tc.body + `}`)
		operations := []struct {
			name string
			run  func()
		}{
			{"http", func() { modularPerfBool = service.shouldFailoverOpenAIUpstreamResponse(account, tc.status, "", body) }},
			{"passthrough", func() { modularPerfBool = shouldFailoverOpenAIPassthroughResponse(account, tc.status, body) }},
			{"stream_failed", func() { modularPerfBool = openAIStreamFailedEventShouldFailover(failed, "") }},
			{"stream_error", func() { modularPerfBool = openAIStreamErrorEventShouldFailover(body, "") }},
			{"replay", func() { modularPerfError = newOpenAIUpstreamFailoverError(tc.status, headers, body, "", true) }},
		}
		for _, op := range operations {
			b.Run(tc.name+"/"+op.name, func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					op.run()
				}
			})
		}
	}
}
