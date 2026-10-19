package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRouteFailurePolicyDTOCopiesAndPreservesSemanticFields(t *testing.T) {
	key := &service.APIKey{KeyRouteFailurePolicy: &service.KeyRouteFailurePolicy{Rules: []service.KeyRouteFailureRule{
		{StatusCodes: []int{502}, Threshold: 3, WindowSeconds: 60, CooldownSeconds: 600},
		{Type: "rate_limit", Threshold: 3, SampleSize: 20, CooldownSeconds: 120},
	}}}
	out := APIKeyFromService(key)
	out.KeyRouteFailurePolicy.Rules[0].StatusCodes[0] = 503
	require.Equal(t, 502, key.KeyRouteFailurePolicy.Rules[0].StatusCodes[0])
	data, err := json.Marshal(out)
	require.NoError(t, err)
	var roundtrip APIKey
	require.NoError(t, json.Unmarshal(data, &roundtrip))
	require.Equal(t, "rate_limit", roundtrip.KeyRouteFailurePolicy.Rules[1].Type)
	require.Equal(t, 20, roundtrip.KeyRouteFailurePolicy.Rules[1].SampleSize)
}
