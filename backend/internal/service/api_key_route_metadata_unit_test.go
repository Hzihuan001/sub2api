//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyRouteMetadataUpdateRequiresExplicitConversion(t *testing.T) {
	key := &APIKey{ID: 1, UserID: 7, Key: "sk-scope-update", Name: "before", Status: StatusActive,
		KeyRouteScope: KeyRouteScopeGroupV1, KeyRouteRevision: 17,
		KeyRouteFailurePolicy: &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{}},
	}
	svc, repo := newUpdateFieldsAPIKeyService(key)
	name := "renamed"
	got, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{Name: &name})
	require.NoError(t, err)
	require.Equal(t, KeyRouteScopeGroupV1, got.KeyRouteScope)
	require.Equal(t, []APIKeyUpdateFields{{Name: true}}, repo.updateFields)
	scope := KeyRouteScopeAccountV2
	got, err = svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{KeyRouteScope: &scope})
	require.NoError(t, err)
	require.Equal(t, KeyRouteScopeAccountV2, got.KeyRouteScope)
	require.Equal(t, APIKeyUpdateFields{KeyRouteScope: true}, repo.updateFields[1])
	require.NotNil(t, got.KeyRouteFailurePolicy)
	require.Empty(t, got.KeyRouteFailurePolicy.Rules, "scope edits do not rewrite existing null/empty rules")
	_, err = svc.Update(context.Background(), 1, 99, UpdateAPIKeyRequest{KeyRouteScope: &scope})
	require.ErrorIs(t, err, ErrInsufficientPerms)
	require.Len(t, repo.updateFields, 2, "scope writes retain ownership enforcement")
}

func TestAPIKeyRouteMetadataAdminConversionReusesRegularWriteMask(t *testing.T) {
	key := &APIKey{ID: 1, UserID: 7, Key: "sk-admin-scope", Status: StatusActive, KeyRouteScope: KeyRouteScopeGroupV1}
	svc, repo := newUpdateFieldsAPIKeyService(key)
	admin := &adminServiceImpl{apiKeyRepo: repo, apiKeyService: svc}
	scope := KeyRouteScopeAccountV2
	result, err := admin.AdminUpdateAPIKeyRouting(context.Background(), key.ID, nil, nil, &scope)
	require.NoError(t, err)
	require.Equal(t, KeyRouteScopeAccountV2, result.APIKey.KeyRouteScope)
	require.Equal(t, []APIKeyUpdateFields{{KeyRouteScope: true}}, repo.updateFields)
	require.False(t, result.AutoGrantedGroupAccess)
	scope = "unknown"
	_, err = admin.AdminUpdateAPIKeyRouting(context.Background(), key.ID, nil, nil, &scope)
	require.Error(t, err)
	require.Len(t, repo.updateFields, 1)
}
