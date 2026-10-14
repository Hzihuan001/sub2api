package service

import "testing"

// Keep this benchmark across the policy extraction: it measures the same
// observable classifier through its host adapter, not a rewritten benchmark
// tailored to the new implementation. It never touches a provider or cache.
func BenchmarkCustomFailureClassification(b *testing.B) {
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
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			body := []byte(tc.body)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = classifyOpenAIGatewayFailure(tc.status, body, "")
			}
		})
	}
}
