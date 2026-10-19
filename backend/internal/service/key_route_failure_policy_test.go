//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestKeyRouteFailurePolicyRequestValidation(t *testing.T) {
	valid := &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{{Type: "timeout", Threshold: 3, SampleSize: 20, CooldownSeconds: 120}}}
	require.NoError(t, validateCreateAPIKeyRequest(CreateAPIKeyRequest{KeyRouteFailurePolicy: valid}))
	require.NoError(t, validateUpdateAPIKeyRequest(UpdateAPIKeyRequest{KeyRouteFailurePolicy: valid}))
	invalid := valid.Clone()
	invalid.Rules[0].Threshold = 21
	for _, err := range []error{
		validateCreateAPIKeyRequest(CreateAPIKeyRequest{KeyRouteFailurePolicy: invalid}),
		validateUpdateAPIKeyRequest(UpdateAPIKeyRequest{KeyRouteFailurePolicy: invalid}),
	} {
		require.Equal(t, "KEY_ROUTE_FAILURE_POLICY_INVALID", infraerrors.Reason(err))
	}
	// Account-suspension policy stays independent and cannot accept key-only semantics.
	var group GroupFailurePolicy
	require.Error(t, json.Unmarshal([]byte(`{"rules":[{"type":"timeout","threshold":3,"sample_size":20,"cooldown_seconds":120}]}`), &group))
}

func TestKeyRouteFailurePolicyAuthSnapshotCopiesBothDirections(t *testing.T) {
	svc := &APIKeyService{}
	key := &APIKey{ID: 1, UserID: 7, User: &User{ID: 7}, KeyRouteFailurePolicy: &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{
		{StatusCodes: []int{502, 503}, Threshold: 3, WindowSeconds: 60, CooldownSeconds: 600},
		{Type: "timeout", Threshold: 3, SampleSize: 20, CooldownSeconds: 120},
	}}}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	key.KeyRouteFailurePolicy.Rules[0].StatusCodes[0] = 504
	key.KeyRouteFailurePolicy.Rules[1].SampleSize = 99
	require.Equal(t, 502, snapshot.KeyRouteFailurePolicy.Rules[0].StatusCodes[0])
	require.Equal(t, 20, snapshot.KeyRouteFailurePolicy.Rules[1].SampleSize)
	data, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var cached APIKeyAuthSnapshot
	require.NoError(t, json.Unmarshal(data, &cached))
	first := svc.snapshotToAPIKey("key", &cached)
	first.KeyRouteFailurePolicy.Rules[0].StatusCodes[0] = 429
	first.KeyRouteFailurePolicy.Rules[1].Threshold = 10
	second := svc.snapshotToAPIKey("key", &cached)
	require.Equal(t, 502, second.KeyRouteFailurePolicy.Rules[0].StatusCodes[0])
	require.Equal(t, 3, second.KeyRouteFailurePolicy.Rules[1].Threshold)
	for _, policy := range []*KeyRouteFailurePolicy{nil, {Rules: []KeyRouteFailureRule{}}} {
		key.KeyRouteFailurePolicy = policy
		roundtrip := svc.snapshotToAPIKey("key", svc.snapshotFromAPIKey(context.Background(), key))
		if policy == nil {
			require.Nil(t, roundtrip.KeyRouteFailurePolicy)
		} else {
			require.NotNil(t, roundtrip.KeyRouteFailurePolicy)
			require.NotNil(t, roundtrip.KeyRouteFailurePolicy.Rules)
			require.Empty(t, roundtrip.KeyRouteFailurePolicy.Rules)
		}
	}
}

func TestKeyRouteFailurePolicyUpdatePreserveDisableAndInherit(t *testing.T) {
	initial := &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{{Type: "consecutive_failures", Threshold: 3, CooldownSeconds: 120}}}
	key := &APIKey{ID: 1, UserID: 7, Key: "key", Status: StatusActive, KeyRouteFailurePolicy: initial}
	svc, repo := newUpdateFieldsAPIKeyService(key)
	name := "rename"
	got, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{Name: &name})
	require.NoError(t, err)
	require.Equal(t, initial, got.KeyRouteFailurePolicy)
	require.False(t, repo.updateFields[0].KeyRouteFailurePolicy)
	disabled := &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{}}
	got, err = svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{KeyRouteFailurePolicy: disabled})
	require.NoError(t, err)
	require.NotNil(t, got.KeyRouteFailurePolicy.Rules)
	require.Empty(t, got.KeyRouteFailurePolicy.Rules)
	require.True(t, repo.updateFields[1].KeyRouteFailurePolicy)
	got, err = svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{KeyRouteFailurePolicySet: true})
	require.NoError(t, err)
	require.Nil(t, got.KeyRouteFailurePolicy)
	require.True(t, repo.updateFields[2].KeyRouteFailurePolicy)
	got, err = svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{KeyRouteFailurePolicy: initial})
	require.NoError(t, err)
	got.KeyRouteFailurePolicy.Rules[0].Threshold = 99
	require.Equal(t, 3, initial.Rules[0].Threshold, "update must not retain the caller's mutable policy")
}
