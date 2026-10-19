package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCustomAPIKeyModelPolicyRepositoryRoundTripAtomicClear(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "model-policy@example.com")
	key := &service.APIKey{UserID: owner.ID, Key: "sk-model-policy", Name: "original", Status: service.StatusActive, KeyModelPolicy: &service.KeyModelPolicy{Enabled: true, Models: []string{"model-a", "model-b"}}}
	require.NoError(t, repo.Create(ctx, key))
	for _, get := range []func() (*service.APIKey, error){
		func() (*service.APIKey, error) { return repo.GetByID(ctx, key.ID) },
		func() (*service.APIKey, error) { return repo.GetByKey(ctx, key.Key) },
		func() (*service.APIKey, error) { return repo.GetByKeyForAuth(ctx, key.Key) },
	} {
		got, err := get()
		require.NoError(t, err)
		require.Equal(t, key.KeyModelPolicy, got.KeyModelPolicy)
	}
	listed, _, err := repo.ListByUserID(ctx, owner.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{})
	require.NoError(t, err)
	require.Equal(t, key.KeyModelPolicy, listed[0].KeyModelPolicy)
	key.Name = "must roll back"
	key.KeyModelPolicy = &service.KeyModelPolicy{Enabled: true, Models: []string{"*"}}
	require.Error(t, repo.Update(ctx, key, service.APIKeyUpdateFields{Name: true, KeyModelPolicy: true}))
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, "original", got.Name)
	require.Equal(t, []string{"model-a", "model-b"}, got.KeyModelPolicy.Models)
	key.KeyModelPolicy = nil
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{KeyModelPolicy: true}))
	got, err = repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Nil(t, got.KeyModelPolicy)
}

func TestCustomAPIKeyModelPolicyRepositoryFailsClosed(t *testing.T) {
	for _, raw := range []string{`{"enabled":true,"models":[]}`, `{"enabled":true,"models":["*"]}`, `{"enabled":false}`, `null`, `broken`} {
		t.Run(raw, func(t *testing.T) {
			ctx := context.Background()
			repo, client := newAPIKeyRepoSQLite(t)
			owner := mustCreateAPIKeyRepoUser(t, ctx, client, "model-policy-invalid@example.com")
			key := &service.APIKey{UserID: owner.ID, Key: "sk-model-policy-invalid", Name: "invalid", Status: service.StatusActive}
			require.NoError(t, repo.Create(ctx, key))
			_, err := client.ExecContext(ctx, "INSERT INTO custom_api_key_model_policies (api_key_id, policy) VALUES ($1, $2)", key.ID, raw)
			require.NoError(t, err)
			got, err := repo.GetByKeyForAuth(ctx, key.Key)
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
	t.Run("missing table", func(t *testing.T) {
		ctx := context.Background()
		repo, client := newAPIKeyRepoSQLite(t)
		owner := mustCreateAPIKeyRepoUser(t, ctx, client, "model-policy-missing@example.com")
		key := &service.APIKey{UserID: owner.ID, Key: "sk-model-policy-missing", Name: "missing", Status: service.StatusActive}
		require.NoError(t, repo.Create(ctx, key))
		_, err := client.ExecContext(ctx, "DROP TABLE custom_api_key_model_policies")
		require.NoError(t, err)
		got, err := repo.GetByKeyForAuth(ctx, key.Key)
		require.Error(t, err)
		require.Nil(t, got)
	})
}
