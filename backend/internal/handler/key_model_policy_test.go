package handler

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestKeyModelPolicyCreateAndUpdateJSONContract(t *testing.T) {
	for _, input := range []string{`{"name":"key"}`, `{"name":"key","key_model_policy":null}`, `{"name":"key","key_model_policy":{"enabled":false}}`} {
		var req CreateAPIKeyRequest
		require.NoError(t, json.Unmarshal([]byte(input), &req))
		require.NoError(t, req.KeyModelPolicy.Validate())
	}
	for _, input := range []string{`{"name":"key","key_model_policy":{"enabled":true,"models":[]}}`, `{"name":"key","key_model_policy":{"enabled":true,"models":["*"]}}`} {
		var req CreateAPIKeyRequest
		require.Error(t, json.Unmarshal([]byte(input), &req))
	}
	var omitted, cleared, set UpdateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &omitted))
	require.NoError(t, json.Unmarshal([]byte(`{"key_model_policy":null}`), &cleared))
	require.NoError(t, json.Unmarshal([]byte(`{"key_model_policy":{"enabled":true,"models":[" model-a "]}}`), &set))
	require.Nil(t, omitted.KeyModelPolicy)
	require.JSONEq(t, `null`, string(cleared.KeyModelPolicy))
	var parsed service.KeyModelPolicy
	require.NoError(t, json.Unmarshal(set.KeyModelPolicy, &parsed))
	require.Equal(t, []string{"model-a"}, parsed.Models)
}
