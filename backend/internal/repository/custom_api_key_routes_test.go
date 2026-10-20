package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCustomAPIKeyRoutesRepositoryRoundTripAndReverseLookup(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "routes@example.com")
	ids := make([]int64, 0, 4)
	for _, name := range []string{"primary", "backup one", "backup two", "unrelated"} {
		group, err := client.Group.Create().SetName(name).SetPlatform(service.PlatformOpenAI).Save(ctx)
		require.NoError(t, err)
		ids = append(ids, group.ID)
	}
	key := &service.APIKey{UserID: owner.ID, Key: "sk-custom-routes", Name: "routes", Status: service.StatusActive, GroupID: &ids[0], BackupGroupIDs: []int64{ids[2], ids[1]}}
	require.NoError(t, repo.Create(ctx, key))
	byID, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, key.BackupGroupIDs, byID.BackupGroupIDs)
	byKey, err := repo.GetByKey(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, key.BackupGroupIDs, byKey.BackupGroupIDs)
	forAuth, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, key.BackupGroupIDs, forAuth.BackupGroupIDs)
	listed, _, err := repo.ListByUserID(ctx, owner.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, key.BackupGroupIDs, listed[0].BackupGroupIDs)
	for _, id := range ids[:3] {
		keys, err := repo.ListKeysByGroupID(ctx, id)
		require.NoError(t, err)
		require.Equal(t, []string{key.Key}, keys)
	}
	unrelated, err := repo.ListKeysByGroupID(ctx, ids[3])
	require.NoError(t, err)
	require.Empty(t, unrelated)

	key.BackupGroupIDs = []int64{ids[1], ids[2]}
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{BackupGroupIDs: true}))
	updated, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, key.BackupGroupIDs, updated.BackupGroupIDs)
	key.BackupGroupIDs = nil
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{BackupGroupIDs: true}))
	updated, err = repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Empty(t, updated.BackupGroupIDs)
}

func TestCustomAPIKeyRoutesRepositoryAtomicFailure(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "routes-atomic@example.com")
	primary, err := client.Group.Create().SetName("primary").SetPlatform(service.PlatformOpenAI).Save(ctx)
	require.NoError(t, err)
	backup, err := client.Group.Create().SetName("backup").SetPlatform(service.PlatformOpenAI).Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{UserID: owner.ID, Key: "sk-custom-routes-atomic", Name: "original", Status: service.StatusActive, GroupID: &primary.ID, BackupGroupIDs: []int64{backup.ID}}
	require.NoError(t, repo.Create(ctx, key))
	key.Name, key.BackupGroupIDs = "must roll back", []int64{backup.ID, backup.ID}
	require.Error(t, repo.Update(ctx, key, service.APIKeyUpdateFields{Name: true, BackupGroupIDs: true}))
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, "original", got.Name)
	require.Equal(t, []int64{backup.ID}, got.BackupGroupIDs)

	// A primary-only writer cannot race a backup writer into a duplicate route.
	key.GroupID = &backup.ID
	require.Error(t, repo.Update(ctx, key, service.APIKeyUpdateFields{GroupID: true}))
	got, err = repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, primary.ID, *got.GroupID)

	invalid := &service.APIKey{UserID: owner.ID, Key: "sk-custom-invalid-create", Name: "invalid", Status: service.StatusActive, GroupID: &primary.ID, BackupGroupIDs: []int64{backup.ID, backup.ID}}
	require.Error(t, repo.Create(ctx, invalid))
	exists, err := repo.ExistsByKey(ctx, invalid.Key)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestCustomAPIKeyRoutesRepositoryReadFailureDoesNotDowngrade(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "routes-read-fail@example.com")
	key := &service.APIKey{UserID: owner.ID, Key: "sk-custom-routes-read-fail", Name: "read", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	_, err := client.ExecContext(ctx, "DROP TABLE custom_api_key_routes")
	require.NoError(t, err)
	got, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.Error(t, err)
	require.Nil(t, got)
}

func TestCustomAPIKeyRoutePoliciesRepositoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	_, err := client.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS custom_api_key_route_policies (
		api_key_id INTEGER PRIMARY KEY REFERENCES api_keys(id) ON DELETE CASCADE,
		policy TEXT NOT NULL
	)`)
	require.NoError(t, err)
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "route-policies@example.com")
	legacy := &service.KeyRouteFailurePolicy{Rules: []service.KeyRouteFailureRule{{StatusCodes: []int{502, 503}, Threshold: 3, WindowSeconds: 60, CooldownSeconds: 600}}}
	key := &service.APIKey{UserID: owner.ID, Key: "sk-route-policy", Name: "route policy", Status: service.StatusActive, KeyRouteFailurePolicy: legacy}
	require.NoError(t, repo.Create(ctx, key))
	for _, get := range []func() (*service.APIKey, error){
		func() (*service.APIKey, error) { return repo.GetByID(ctx, key.ID) },
		func() (*service.APIKey, error) { return repo.GetByKey(ctx, key.Key) },
		func() (*service.APIKey, error) { return repo.GetByKeyForAuth(ctx, key.Key) },
	} {
		got, err := get()
		require.NoError(t, err)
		require.Equal(t, legacy, got.KeyRouteFailurePolicy)
	}
	key.KeyRouteFailurePolicy = &service.KeyRouteFailurePolicy{Rules: []service.KeyRouteFailureRule{
		{Type: "consecutive_failures", Threshold: 3, CooldownSeconds: 120},
		{Type: "timeout", Threshold: 3, SampleSize: 20, CooldownSeconds: 120},
		{Type: "rate_limit", Threshold: 3, SampleSize: 20, CooldownSeconds: 120},
	}}
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{KeyRouteFailurePolicy: true}))
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, key.KeyRouteFailurePolicy, got.KeyRouteFailurePolicy)
	key.KeyRouteFailurePolicy = &service.KeyRouteFailurePolicy{Rules: []service.KeyRouteFailureRule{}}
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{KeyRouteFailurePolicy: true}))
	got, err = repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.NotNil(t, got.KeyRouteFailurePolicy)
	require.NotNil(t, got.KeyRouteFailurePolicy.Rules)
	require.Empty(t, got.KeyRouteFailurePolicy.Rules)
	key.KeyRouteFailurePolicy = nil
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{KeyRouteFailurePolicy: true}))
	got, err = repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Nil(t, got.KeyRouteFailurePolicy)
	// Validation and persistence share the same contract. An invalid policy
	// must roll back the primary-key row edit in the same transaction.
	key.Name = "must roll back"
	key.KeyRouteFailurePolicy = &service.KeyRouteFailurePolicy{Rules: []service.KeyRouteFailureRule{{Type: "timeout", Threshold: 21, SampleSize: 20, CooldownSeconds: 120}}}
	require.Error(t, repo.Update(ctx, key, service.APIKeyUpdateFields{Name: true, KeyRouteFailurePolicy: true}))
	got, err = repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, "route policy", got.Name)
	require.Nil(t, got.KeyRouteFailurePolicy)
}
