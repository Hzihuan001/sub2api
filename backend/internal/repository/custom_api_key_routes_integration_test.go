//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCustomAPIKeyRoutesDurableInvalidation(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	primary := mustCreateGroup(t, integrationEntClient, &service.Group{Name: fmt.Sprintf("route-primary-%d", suffix), Platform: service.PlatformOpenAI, RateMultiplier: 1})
	backup := mustCreateGroup(t, integrationEntClient, &service.Group{Name: fmt.Sprintf("route-backup-%d", suffix), Platform: service.PlatformOpenAI, RateMultiplier: 1})
	user := mustCreateUser(t, integrationEntClient, &service.User{Email: fmt.Sprintf("route-user-%d@example.com", suffix), Concurrency: 5})
	key := &service.APIKey{UserID: user.ID, GroupID: &primary.ID, BackupGroupIDs: []int64{backup.ID}, Key: fmt.Sprintf("sk-routes-%d", suffix), Name: "routes", Status: service.StatusActive}
	repo := NewAPIKeyRepository(integrationEntClient, integrationDB)
	require.NoError(t, repo.Create(ctx, key))
	sum := sha256.Sum256([]byte(key.Key))
	cacheKey := hex.EncodeToString(sum[:])
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id = $1", key.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id IN ($1, $2)", primary.ID, backup.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = $1", cacheKey)
		require.NoError(t, err)
	})
	clear := func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = $1", cacheKey)
		require.NoError(t, err)
	}
	count := func() int {
		var n int
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_cache_invalidation_outbox WHERE cache_key = $1", cacheKey).Scan(&n))
		return n
	}
	require.Positive(t, count(), "route creation must enqueue durable invalidation")
	clear()
	result, err := integrationDB.ExecContext(ctx, "UPDATE groups SET status = 'inactive' WHERE id = $1", backup.ID)
	require.NoError(t, err)
	affected, err := result.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, affected, "BEFORE trigger must not suppress the update")
	require.Positive(t, count(), "backup disable must invalidate the referencing key")
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{backup.ID}, got.BackupGroupIDs, "disabled backup stays explicit")
	clear()
	key.BackupGroupIDs = nil
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{BackupGroupIDs: true}))
	require.Positive(t, count(), "route clearing must enqueue durable invalidation")
	key.BackupGroupIDs = []int64{backup.ID}
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{BackupGroupIDs: true}))
	require.NoError(t, repo.DeleteWithAudit(ctx, key.ID))
	var routeCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM custom_api_key_routes WHERE api_key_id = $1", key.ID).Scan(&routeCount))
	require.Zero(t, routeCount, "soft deletion reclaims routes in the deletion transaction")
}
