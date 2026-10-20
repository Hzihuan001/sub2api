//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCustomAPIKeyAccountRouteDurableRevisionsAndLegacyRollback(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	primary := mustCreateGroup(t, integrationEntClient, &service.Group{Name: fmt.Sprintf("account-primary-%d", suffix), Platform: service.PlatformOpenAI, RateMultiplier: 1})
	backup := mustCreateGroup(t, integrationEntClient, &service.Group{Name: fmt.Sprintf("account-backup-%d", suffix), Platform: service.PlatformOpenAI, RateMultiplier: 1})
	owner := mustCreateUser(t, integrationEntClient, &service.User{Email: fmt.Sprintf("account-route-%d@example.com", suffix), Concurrency: 5})
	repo := newAPIKeyRepositoryWithSQL(integrationEntClient, integrationDB)
	key := &service.APIKey{UserID: owner.ID, GroupID: &primary.ID, Key: fmt.Sprintf("sk-account-%d", suffix), Name: "legacy-compatible", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id=$1", key.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id=$1", owner.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM settings WHERE key=$1", groupRoutePolicySettingPrefix+fmt.Sprint(primary.ID))
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id IN ($1,$2)", primary.ID, backup.ID)
		require.NoError(t, err)
	})
	require.Equal(t, service.KeyRouteScopeGroupV1, key.KeyRouteScope)
	previous := key.KeyRouteRevision
	key.KeyRouteScope = service.KeyRouteScopeAccountV2
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{KeyRouteScope: true}))
	require.Greater(t, key.KeyRouteRevision, previous)
	previous = key.KeyRouteRevision

	// Raw writes emulate the old image: original tables and JSON remain usable.
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO custom_api_key_routes(api_key_id,position,group_id) VALUES($1,1,$2)`, key.ID, backup.ID)
	require.NoError(t, err)
	revision, err := repo.GetKeyRouteRevision(ctx, key.ID)
	require.NoError(t, err)
	require.Greater(t, revision, previous)
	previous = revision
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO custom_api_key_route_policies(api_key_id,policy) VALUES($1,'{"rules":[]}'::jsonb)`, key.ID)
	require.NoError(t, err)
	var legacyPayload string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT policy::text FROM custom_api_key_route_policies WHERE api_key_id=$1`, key.ID).Scan(&legacyPayload))
	require.JSONEq(t, `{"rules":[]}`, legacyPayload)
	revision, err = repo.GetKeyRouteRevision(ctx, key.ID)
	require.NoError(t, err)
	require.Greater(t, revision, previous)
	_, err = integrationDB.ExecContext(ctx, `UPDATE custom_api_key_account_routes SET revision=1 WHERE api_key_id=$1`, key.ID)
	require.Error(t, err, "a stale/direct writer cannot move the fence backwards")

	// A rule deletion and identical re-creation cannot reuse the inherited fence.
	_, before, err := repo.GetGroupRoutePolicySnapshot(ctx, primary.ID)
	require.NoError(t, err)
	settingKey := groupRoutePolicySettingPrefix + fmt.Sprint(primary.ID)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES($1,'{"status_rules":[]}')`, settingKey)
	require.NoError(t, err)
	raw, first, err := repo.GetGroupRoutePolicySnapshot(ctx, primary.ID)
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	require.Greater(t, first, before)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM settings WHERE key=$1`, settingKey)
	require.NoError(t, err)
	raw, removed, err := repo.GetGroupRoutePolicySnapshot(ctx, primary.ID)
	require.NoError(t, err)
	require.Empty(t, raw)
	require.Greater(t, removed, first)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES($1,'{"status_rules":[]}')`, settingKey)
	require.NoError(t, err)
	_, restored, err := repo.GetGroupRoutePolicySnapshot(ctx, primary.ID)
	require.NoError(t, err)
	require.Greater(t, restored, removed)
	// Both metadata kinds use the same ordered generation source.
	key.KeyRouteScope = service.KeyRouteScopeGroupV1
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{KeyRouteScope: true}))
	require.Greater(t, key.KeyRouteRevision, restored)

	previous = key.KeyRouteRevision
	_, err = integrationDB.ExecContext(ctx, `UPDATE groups SET status='inactive' WHERE id=$1`, backup.ID)
	require.NoError(t, err)
	revision, err = repo.GetKeyRouteRevision(ctx, key.ID)
	require.NoError(t, err)
	require.Greater(t, revision, previous, "revoking a backup invalidates old attempt identities")
	previous = revision
	_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET deleted_at=NOW() WHERE id=$1`, key.ID)
	require.NoError(t, err)
	_, err = repo.GetKeyRouteRevision(ctx, key.ID)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT revision FROM custom_api_key_account_routes WHERE api_key_id=$1`, key.ID).Scan(&revision))
	require.Greater(t, revision, previous, "soft deletion keeps the newer tombstone")
}
