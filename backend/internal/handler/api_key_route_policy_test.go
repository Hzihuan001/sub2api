package handler

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRoutePolicyCreateJSONContract(t *testing.T) {
	for _, input := range []string{`{"name":"key"}`, `{"name":"key","key_route_failure_policy":null}`} {
		var req CreateAPIKeyRequest
		require.NoError(t, json.Unmarshal([]byte(input), &req))
		require.Nil(t, req.KeyRouteFailurePolicy)
	}
	var req CreateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"key","key_route_failure_policy":{"rules":[]}}`), &req))
	require.NotNil(t, req.KeyRouteFailurePolicy)
	require.NotNil(t, req.KeyRouteFailurePolicy.Rules)
	require.NoError(t, json.Unmarshal([]byte(`{"name":"key","key_route_failure_policy":{"rules":[{"type":"timeout","threshold":3,"sample_size":20,"cooldown_seconds":120}]}}`), &req))
	require.Equal(t, "timeout", req.KeyRouteFailurePolicy.Rules[0].Type)
	for _, input := range []string{
		`{"name":"key","key_route_failure_policy":{"rules":[{"type":"timeout","threshold":21,"sample_size":20,"cooldown_seconds":120}]}}`,
		`{"name":"key","key_route_failure_policy":{"rules":[{"type":"ttft","threshold":3,"cooldown_seconds":120}]}}`,
		`{"name":"key","key_route_failure_policy":{"rules":[{"type":"consecutive_failures","threshold":3,"cooldown_seconds":120,"window_seconds":0}]}}`,
	} {
		require.Error(t, json.Unmarshal([]byte(input), &req))
	}
}

func TestAPIKeyRoutePolicyUpdateJSONPresence(t *testing.T) {
	var omitted, inherited, disabled UpdateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &omitted))
	require.NoError(t, json.Unmarshal([]byte(`{"key_route_failure_policy":null}`), &inherited))
	require.NoError(t, json.Unmarshal([]byte(`{"key_route_failure_policy":{"rules":[]}}`), &disabled))
	require.Nil(t, omitted.KeyRouteFailurePolicy)
	require.Equal(t, "null", string(inherited.KeyRouteFailurePolicy))
	var policy service.KeyRouteFailurePolicy
	require.NoError(t, json.Unmarshal(disabled.KeyRouteFailurePolicy, &policy))
	require.NotNil(t, policy.Rules)
	require.Empty(t, policy.Rules)
}
