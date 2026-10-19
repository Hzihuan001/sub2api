package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyModelPolicyDTOCopies(t *testing.T) {
	key := &service.APIKey{KeyModelPolicy: &service.KeyModelPolicy{Enabled: true, Models: []string{"model-a"}}}
	got := APIKeyFromService(key)
	got.KeyModelPolicy.Models[0] = "changed"
	require.Equal(t, "model-a", key.KeyModelPolicy.Models[0])
	require.Nil(t, APIKeyFromService(&service.APIKey{}).KeyModelPolicy)
}

func TestAPIKeyModelPolicyGroupPublicCandidates(t *testing.T) {
	group := &service.Group{ModelAllowlist: service.GroupModelAllowlist{
		Enabled: true, Models: []string{"exact-model", "gpt-*", "other-model", "exact-model"},
	}}
	got := GroupFromService(group)
	require.Equal(t, []string{"exact-model", "other-model"}, got.ModelCandidates)
	payload, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "model_allowlist")
	require.NotContains(t, string(payload), "gpt-*")
	group.ModelAllowlist.Enabled = false
	require.Empty(t, GroupFromService(group).ModelCandidates)
}
