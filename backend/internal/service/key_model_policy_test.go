package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeyModelPolicyExactPublicIDs(t *testing.T) {
	policy, err := NormalizeKeyModelPolicy(&KeyModelPolicy{Enabled: true, Models: []string{" gpt-6.1-sol ", "deepseek-chat", "gpt-6.1-sol"}})
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-6.1-sol", "deepseek-chat"}, policy.Models)
	key := &APIKey{KeyModelPolicy: policy}
	require.True(t, key.KeyModelPolicyEnabled())
	for _, model := range []string{"gpt-6.1-sol", " gpt-6.1-sol ", "deepseek-chat"} {
		require.True(t, key.AllowsPublicModel(model), model)
	}
	for _, model := range []string{"", "GPT-6.1-sol", "gpt-6.1-sol-high", "models/gpt-6.1-sol", "gpt-6.1-sol*", "unknown"} {
		require.False(t, key.AllowsPublicModel(model), model)
	}
	for _, disabled := range []*KeyModelPolicy{nil, {Enabled: false, Models: []string{"one"}}} {
		require.True(t, disabled.Allows("arbitrary"))
		got, err := NormalizeKeyModelPolicy(disabled)
		require.NoError(t, err)
		require.Nil(t, got)
	}
	require.False(t, (&KeyModelPolicy{Enabled: true}).Allows("anything"), "invalid enabled empty policy fails closed")
}

func TestKeyModelPolicyValidationBoundaries(t *testing.T) {
	for _, models := range [][]string{nil, {}, {""}, {" "}, {"gpt-*"}, {"line\nbreak"}, {strings.Repeat("a", 201)}, make([]string, 201)} {
		require.Error(t, (&KeyModelPolicy{Enabled: true, Models: models}).Validate())
	}
	valid := &KeyModelPolicy{Enabled: true, Models: []string{strings.Repeat("模", 200)}}
	require.NoError(t, valid.Validate(), "character limit is not a UTF-8 byte limit")
	models := make([]string, 200)
	for i := range models {
		models[i] = "m" + strings.Repeat("x", i)
	}
	require.NoError(t, (&KeyModelPolicy{Enabled: true, Models: models}).Validate())
	for _, input := range []string{`{"enabled":true,"models":[]}`, `{"enabled":true,"models":["*"]}`, `{"enabled":true,"models":[1]}`, `[]`, `true`} {
		var policy KeyModelPolicy
		require.Error(t, json.Unmarshal([]byte(input), &policy), input)
	}
}

func TestKeyModelPolicyCloneDoesNotAlias(t *testing.T) {
	policy := &KeyModelPolicy{Enabled: true, Models: []string{"model-a"}}
	copy := policy.Clone()
	copy.Models[0] = "different"
	require.Equal(t, "model-a", policy.Models[0])
	var missing *KeyModelPolicy
	require.Nil(t, missing.Clone())
}
