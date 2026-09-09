//go:build integration

package moshureseller

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	postgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestCredentialSyncAtomicWithOutbox(t *testing.T) {
	ctx := context.Background()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("credential_sync_test"), postgres.WithUsername("test"), postgres.WithPassword("test"), postgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(ctx)) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(ctx, `
	CREATE TABLE accounts(id bigint PRIMARY KEY,credentials jsonb,updated_at timestamptz,deleted_at timestamptz,extra jsonb);
	CREATE TABLE moshu_products(remote_product_id bigint PRIMARY KEY,local_account_id bigint,credential_ciphertext text,sales_rate_multiplier numeric,product_code text);
	CREATE TABLE account_groups(account_id bigint,group_id bigint);
	CREATE TABLE scheduler_outbox(event_type text,account_id bigint,payload jsonb);
	INSERT INTO accounts VALUES(2,'{"api_key":"old","base_url":"https://example.test","other":"keep"}',NOW(),NULL,'{"custom":"keep","moshu_remote_product_id":1}');
	INSERT INTO moshu_products VALUES(3,2,'old-cipher',1.23,'gpt'),(4,NULL,'unused-cipher',2.34,'claude');
	INSERT INTO account_groups VALUES(2,5),(2,6);`)
	require.NoError(t, err)
	readKey := func() string {
		var key string
		require.NoError(t, db.QueryRow(`SELECT credentials->>'api_key' FROM accounts WHERE id=2`).Scan(&key))
		return key
	}
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, syncAccountCredentialTx(ctx, tx, 3, "rolled-back"))
	require.NoError(t, tx.Rollback())
	require.Equal(t, "old", readKey())
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM scheduler_outbox`).Scan(&count))
	require.Zero(t, count)

	tx, err = db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.Exec(`UPDATE moshu_products SET credential_ciphertext='new-cipher' WHERE remote_product_id=3`)
	require.NoError(t, err)
	require.NoError(t, syncAccountCredentialTx(ctx, tx, 3, "new-key"))
	require.NoError(t, syncAccountCredentialTx(ctx, tx, 4, "not-enabled-key"))
	require.NoError(t, tx.Commit())
	require.Equal(t, "new-key", readKey())
	var remoteID int64
	var code, custom string
	require.NoError(t, db.QueryRow(`SELECT (extra->>'moshu_remote_product_id')::bigint,extra->>'moshu_product_code',extra->>'custom' FROM accounts WHERE id=2`).Scan(&remoteID, &code, &custom))
	require.EqualValues(t, 3, remoteID)
	require.Equal(t, "gpt", code)
	require.Equal(t, "keep", custom)
	var baseURL, other, ciphertext string
	var rate float64
	require.NoError(t, db.QueryRow(`SELECT a.credentials->>'base_url',a.credentials->>'other',p.credential_ciphertext,p.sales_rate_multiplier FROM accounts a JOIN moshu_products p ON p.local_account_id=a.id WHERE a.id=2`).Scan(&baseURL, &other, &ciphertext, &rate))
	require.Equal(t, "https://example.test", baseURL)
	require.Equal(t, "keep", other)
	require.Equal(t, "new-cipher", ciphertext)
	require.Equal(t, 1.23, rate)
	var event string
	var accountID int64
	var raw []byte
	require.NoError(t, db.QueryRow(`SELECT event_type,account_id,payload FROM scheduler_outbox`).Scan(&event, &accountID, &raw))
	require.Equal(t, "account_changed", event)
	require.EqualValues(t, 2, accountID)
	var payload struct {
		GroupIDs []int64 `json:"group_ids"`
	}
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.ElementsMatch(t, []int64{5, 6}, payload.GroupIDs)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM scheduler_outbox`).Scan(&count))
	require.Equal(t, 1, count)
}
