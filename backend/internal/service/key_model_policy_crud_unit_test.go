//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeyModelPolicyCreateValidatesAndCopies(t *testing.T) {
	svc := newCustomRoutesService()
	repo := &createLimitAPIKeyRepoStub{apiKeyRepoStub: &apiKeyRepoStub{}}
	svc.apiKeyRepo = repo
	policy := &KeyModelPolicy{Enabled: true, Models: []string{" model-a "}}
	customKey := "sk-model-policy-test"
	got, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{Name: "model key", CustomKey: &customKey, KeyModelPolicy: policy})
	require.NoError(t, err)
	require.Equal(t, []string{"model-a"}, got.KeyModelPolicy.Models)
	policy.Models[0] = "changed"
	require.Equal(t, []string{"model-a"}, got.KeyModelPolicy.Models)
	_, err = svc.Create(context.Background(), 7, CreateAPIKeyRequest{Name: "invalid", KeyModelPolicy: &KeyModelPolicy{Enabled: true}})
	require.Error(t, err)
	require.Len(t, repo.created, 1)
}

func TestKeyModelPolicyUpdatePreserveSetClearAndOwnership(t *testing.T) {
	for _, tc := range []struct {
		name    string
		req     UpdateAPIKeyRequest
		want    *KeyModelPolicy
		changed bool
	}{
		{name: "omitted", req: UpdateAPIKeyRequest{}, want: &KeyModelPolicy{Enabled: true, Models: []string{"original"}}},
		{name: "replace", req: UpdateAPIKeyRequest{KeyModelPolicy: &KeyModelPolicy{Enabled: true, Models: []string{" replacement "}}}, want: &KeyModelPolicy{Enabled: true, Models: []string{"replacement"}}, changed: true},
		{name: "null", req: UpdateAPIKeyRequest{KeyModelPolicySet: true}, changed: true},
		{name: "disabled", req: UpdateAPIKeyRequest{KeyModelPolicy: &KeyModelPolicy{Enabled: false}}, changed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := &APIKey{ID: 1, UserID: 7, Key: "key", Status: StatusActive, QuotaUsed: 4, KeyModelPolicy: &KeyModelPolicy{Enabled: true, Models: []string{"original"}}}
			svc, repo := newUpdateFieldsAPIKeyService(key)
			got, err := svc.Update(context.Background(), 1, 7, tc.req)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.KeyModelPolicy)
			require.Equal(t, []APIKeyUpdateFields{{KeyModelPolicy: tc.changed}}, repo.updateFields)
			require.Equal(t, 4.0, got.QuotaUsed)
			_, err = svc.Update(context.Background(), 1, 99, tc.req)
			require.ErrorIs(t, err, ErrInsufficientPerms)
			require.Len(t, repo.updateFields, 1)
		})
	}
	key := &APIKey{ID: 1, UserID: 7, Key: "key", Status: StatusActive}
	svc, repo := newUpdateFieldsAPIKeyService(key)
	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{KeyModelPolicy: &KeyModelPolicy{Enabled: true, Models: []string{"*"}}})
	require.Error(t, err)
	require.Empty(t, repo.updateFields)
}

func TestKeyModelPolicyAuthSnapshotCopiesAndRejectsLegacyOrCorrupt(t *testing.T) {
	svc := &APIKeyService{}
	key := &APIKey{ID: 1, UserID: 7, User: &User{ID: 7}, KeyModelPolicy: &KeyModelPolicy{Enabled: true, Models: []string{"model-a"}}}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	key.KeyModelPolicy.Models[0] = "changed"
	require.Equal(t, "model-a", snapshot.KeyModelPolicy.Models[0])
	first := svc.snapshotToAPIKey("key", snapshot)
	first.KeyModelPolicy.Models[0] = "changed-again"
	require.Equal(t, "model-a", svc.snapshotToAPIKey("key", snapshot).KeyModelPolicy.Models[0])
	_, hit, err := svc.applyAuthCacheEntry("key", &APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	require.True(t, hit)
	for _, value := range []*APIKeyAuthSnapshot{
		{Version: 27},
		{Version: apiKeyAuthSnapshotVersion, KeyModelPolicy: &KeyModelPolicy{Enabled: true}},
		{Version: apiKeyAuthSnapshotVersion, KeyModelPolicy: &KeyModelPolicy{Enabled: true, Models: []string{"*"}}},
	} {
		_, hit, err = svc.applyAuthCacheEntry("key", &APIKeyAuthCacheEntry{Snapshot: value})
		require.NoError(t, err)
		require.False(t, hit)
	}
}
