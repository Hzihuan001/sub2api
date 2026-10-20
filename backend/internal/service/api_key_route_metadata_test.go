package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyRouteMetadataAuthSnapshotRoundTripAndVersionFence(t *testing.T) {
	svc := &APIKeyService{}
	key := &APIKey{ID: 1, UserID: 2, Key: "sk-metadata", Status: StatusActive,
		KeyRouteScope: KeyRouteScopeAccountV2, KeyRouteRevision: 73,
		KeyRouteFailurePolicy: &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{}},
		User:                  &User{ID: 2, Status: StatusActive},
	}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	require.NotNil(t, snapshot)
	require.Equal(t, 29, snapshot.Version)
	require.Equal(t, KeyRouteScopeAccountV2, snapshot.KeyRouteScope)
	require.EqualValues(t, 73, snapshot.KeyRouteRevision)
	got, ok, err := svc.applyAuthCacheEntry(key.Key, &APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, key.KeyRouteScope, got.KeyRouteScope)
	require.Equal(t, key.KeyRouteRevision, got.KeyRouteRevision)
	require.NotNil(t, got.KeyRouteFailurePolicy)
	require.Empty(t, got.KeyRouteFailurePolicy.Rules)
	snapshot.Version = 28
	got, ok, err = svc.applyAuthCacheEntry(key.Key, &APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, got)
}

func TestAPIKeyRouteMetadataWriteValidationPreservesOmission(t *testing.T) {
	for _, scope := range []string{KeyRouteScopeGroupV1, KeyRouteScopeAccountV2} {
		require.NoError(t, ValidateKeyRouteWriteMetadata(&scope, nil))
	}
	require.NoError(t, ValidateKeyRouteWriteMetadata(nil, nil))
	require.Equal(t, KeyRouteScopeGroupV1, keyRouteWriteScope(nil))
	for _, input := range []string{
		`{"key_route_scope":"invalid"}`, `{"key_route_scope":""}`,
		`{"key_route_revision":1}`, `{"key_route_revision":null}`,
	} {
		var create CreateAPIKeyRequest
		require.NoError(t, json.Unmarshal([]byte(input), &create))
		require.Error(t, validateCreateAPIKeyRequest(create))
		var update UpdateAPIKeyRequest
		require.NoError(t, json.Unmarshal([]byte(input), &update))
		require.Error(t, validateUpdateAPIKeyRequest(update))
	}
}

func TestAPIKeyRouteMetadataRejectsCorruptAccountScopeCache(t *testing.T) {
	for _, tc := range []struct {
		scope    string
		revision int64
	}{
		{KeyRouteScopeAccountV2, 0}, {KeyRouteScopeAccountV2, -1}, {"unknown_v3", 9},
	} {
		svc := &APIKeyService{}
		got, ok, err := svc.applyAuthCacheEntry("sk-corrupt", &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{
			Version: apiKeyAuthSnapshotVersion, KeyRouteScope: tc.scope, KeyRouteRevision: tc.revision,
		}})
		require.NoError(t, err)
		require.False(t, ok)
		require.Nil(t, got)
	}
	require.NoError(t, ValidateKeyRouteMetadata("", 0), "legacy zero values retain group_v1 behavior")
	require.NoError(t, ValidateKeyRouteMetadata(KeyRouteScopeGroupV1, 9))
}
