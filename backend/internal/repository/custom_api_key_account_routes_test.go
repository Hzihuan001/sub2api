package repository

import (
	"context"
	"fmt"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// SQLite exercises the attachment/write contract. PostgreSQL integration tests
// separately execute the real 9003 PL/pgSQL and its durable invalidation triggers.
func installAPIKeyAccountRouteTestSchema(t *testing.T, client *dbent.Client) {
	t.Helper()
	ctx := context.Background()
	_, err := client.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS custom_api_key_route_policies (
		 api_key_id INTEGER PRIMARY KEY REFERENCES api_keys(id) ON DELETE CASCADE, policy TEXT NOT NULL);
		CREATE TABLE custom_test_route_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT);
		CREATE TABLE custom_api_key_account_routes (
		 api_key_id INTEGER PRIMARY KEY REFERENCES api_keys(id) ON DELETE CASCADE,
		 scope TEXT NOT NULL DEFAULT 'group_v1' CHECK(scope IN ('group_v1','account_v2')),
		 revision INTEGER NOT NULL CHECK(revision > 0));
		CREATE TABLE custom_group_route_revisions (
		 group_id INTEGER PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
		 revision INTEGER NOT NULL CHECK(revision > 0));
		CREATE TRIGGER custom_test_key_metadata_insert AFTER INSERT ON api_keys BEGIN
		 INSERT INTO custom_test_route_revisions(id) VALUES(NULL);
		 INSERT INTO custom_api_key_account_routes(api_key_id,revision)
		 VALUES(NEW.id,(SELECT MAX(id) FROM custom_test_route_revisions)); END;
		CREATE TRIGGER custom_test_group_metadata_insert AFTER INSERT ON groups BEGIN
		 INSERT INTO custom_test_route_revisions(id) VALUES(NULL);
		 INSERT INTO custom_group_route_revisions(group_id,revision)
		 VALUES(NEW.id,(SELECT MAX(id) FROM custom_test_route_revisions)); END;
		CREATE TRIGGER custom_test_key_metadata_binding AFTER UPDATE OF group_id,status,user_id,deleted_at,key ON api_keys
		 WHEN OLD.group_id IS NOT NEW.group_id OR OLD.status IS NOT NEW.status OR OLD.user_id IS NOT NEW.user_id
		 OR OLD.deleted_at IS NOT NEW.deleted_at OR OLD.key IS NOT NEW.key BEGIN
		 INSERT INTO custom_test_route_revisions(id) VALUES(NULL);
		 UPDATE custom_api_key_account_routes SET revision=(SELECT MAX(id) FROM custom_test_route_revisions)
		 WHERE api_key_id=NEW.id; END;
		CREATE TRIGGER custom_test_key_metadata_scope AFTER UPDATE OF scope ON custom_api_key_account_routes
		 WHEN OLD.scope IS NOT NEW.scope BEGIN
		 INSERT INTO custom_test_route_revisions(id) VALUES(NULL);
		 UPDATE custom_api_key_account_routes SET revision=(SELECT MAX(id) FROM custom_test_route_revisions)
		 WHERE api_key_id=NEW.api_key_id; END;
	`)
	require.NoError(t, err)
	for _, table := range []string{"custom_api_key_routes", "custom_api_key_route_policies", "custom_api_key_model_policies"} {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			row := "NEW"
			if op == "DELETE" {
				row = "OLD"
			}
			_, err = client.ExecContext(ctx, fmt.Sprintf(`CREATE TRIGGER custom_test_revision_%s_%s AFTER %s ON %s BEGIN
			 INSERT INTO custom_test_route_revisions(id) VALUES(NULL);
			 UPDATE custom_api_key_account_routes SET revision=(SELECT MAX(id) FROM custom_test_route_revisions)
			 WHERE api_key_id=%s.api_key_id; END;`, table, op, op, table, row))
			require.NoError(t, err)
		}
	}
}

func mustCreateRouteTestGroup(t *testing.T, ctx context.Context, client *dbent.Client, name string) *dbent.Group {
	t.Helper()
	group, err := client.Group.Create().SetName(name).SetPlatform(service.PlatformOpenAI).Save(ctx)
	require.NoError(t, err)
	return group
}

func TestCustomAPIKeyAccountRouteMetadataRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "account-route-metadata@example.com")
	primary := mustCreateRouteTestGroup(t, ctx, client, "account-route-primary")
	backup := mustCreateRouteTestGroup(t, ctx, client, "account-route-backup")
	key := &service.APIKey{UserID: owner.ID, GroupID: &primary.ID, Key: "sk-account-route-metadata", Name: "metadata", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	require.Equal(t, service.KeyRouteScopeGroupV1, key.KeyRouteScope)
	require.Positive(t, key.KeyRouteRevision)
	initial := key.KeyRouteRevision
	key.KeyRouteScope = service.KeyRouteScopeAccountV2
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{KeyRouteScope: true}))
	require.Greater(t, key.KeyRouteRevision, initial)
	converted := key.KeyRouteRevision
	for _, load := range []func() (*service.APIKey, error){
		func() (*service.APIKey, error) { return repo.GetByID(ctx, key.ID) },
		func() (*service.APIKey, error) { return repo.GetByKey(ctx, key.Key) },
		func() (*service.APIKey, error) { return repo.GetByKeyForAuth(ctx, key.Key) },
	} {
		got, err := load()
		require.NoError(t, err)
		require.Equal(t, service.KeyRouteScopeAccountV2, got.KeyRouteScope)
		require.Equal(t, converted, got.KeyRouteRevision)
		require.Nil(t, got.KeyRouteFailurePolicy)
	}
	// Old writers do not know scope, but changes to their routes still advance it.
	_, err := client.ExecContext(ctx, `INSERT INTO custom_api_key_routes(api_key_id,position,group_id) VALUES($1,1,$2)`, key.ID, backup.ID)
	require.NoError(t, err)
	next, err := repo.GetKeyRouteRevision(ctx, key.ID)
	require.NoError(t, err)
	require.Greater(t, next, converted)
	key.BackupGroupIDs = []int64{backup.ID}
	key.KeyRouteFailurePolicy = &service.KeyRouteFailurePolicy{Rules: []service.KeyRouteFailureRule{}}
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{KeyRouteFailurePolicy: true}))
	require.Greater(t, key.KeyRouteRevision, next)
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.NotNil(t, got.KeyRouteFailurePolicy)
	require.Empty(t, got.KeyRouteFailurePolicy.Rules)
	key.KeyRouteFailurePolicy = nil
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{KeyRouteFailurePolicy: true}))
	got, err = repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Nil(t, got.KeyRouteFailurePolicy)
	require.Equal(t, service.KeyRouteScopeAccountV2, got.KeyRouteScope)
}

func TestCustomAPIKeyAccountRouteScopeRollbackAndMissingMetadata(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "account-route-atomic@example.com")
	key := &service.APIKey{UserID: owner.ID, Key: "sk-account-route-atomic", Name: "before", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	revision := key.KeyRouteRevision
	key.Name, key.KeyRouteScope = "must-not-save", "unknown_v3"
	require.Error(t, repo.Update(ctx, key, service.APIKeyUpdateFields{Name: true, KeyRouteScope: true}))
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, "before", got.Name)
	require.Equal(t, revision, got.KeyRouteRevision)
	_, err = client.ExecContext(ctx, "DELETE FROM custom_api_key_account_routes WHERE api_key_id=$1", key.ID)
	require.NoError(t, err)
	got, err = repo.GetByKeyForAuth(ctx, key.Key)
	require.Error(t, err)
	require.Nil(t, got, "lost metadata cannot silently reinterpret account_v2 as group_v1")
}

func TestCustomAPIKeyAccountRouteAttachmentRejectsMixedPrimarySnapshot(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "account-route-snapshot@example.com")
	key := &service.APIKey{UserID: owner.ID, Key: "sk-account-route-snapshot", Name: "snapshot", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	group := mustCreateRouteTestGroup(t, ctx, client, "account-route-snapshot")
	_, err := client.ExecContext(ctx, "UPDATE api_keys SET group_id=$1 WHERE id=$2", group.ID, key.ID)
	require.NoError(t, err)
	require.ErrorContains(t, repo.attachAPIKeyAccountRoutes(ctx, []service.APIKey{*key}), "changed while reading")
}

func TestCustomAPIKeyAccountRouteGroupSnapshotAndMissingRevision(t *testing.T) {
	ctx := context.Background()
	repo, client := newAPIKeyRepoSQLite(t)
	group := mustCreateRouteTestGroup(t, ctx, client, "account-route-policy-snapshot")
	raw, revision, err := repo.GetGroupRoutePolicySnapshot(ctx, group.ID)
	require.NoError(t, err)
	require.Empty(t, raw)
	require.Positive(t, revision)
	_, err = client.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES($1,$2,CURRENT_TIMESTAMP)`, groupRoutePolicySettingPrefix+fmt.Sprint(group.ID), `{"status_rules":[]}`)
	require.NoError(t, err)
	raw, same, err := repo.GetGroupRoutePolicySnapshot(ctx, group.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{"status_rules":[]}`, raw)
	require.Equal(t, revision, same, "SQLite fixture does not replace real PostgreSQL setting-trigger tests")
	_, err = client.ExecContext(ctx, `DELETE FROM custom_group_route_revisions WHERE group_id=$1`, group.ID)
	require.NoError(t, err)
	_, _, err = repo.GetGroupRoutePolicySnapshot(ctx, group.ID)
	require.ErrorContains(t, err, "invalid group route revision")
	require.NotErrorIs(t, err, service.ErrGroupNotFound, "infrastructure corruption is not authorization revocation")
}
