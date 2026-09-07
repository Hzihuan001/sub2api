package moshureseller

import (
	"context"
	"database/sql"
)

// Store the operational key and its scheduler notification in the same
// transaction as the encrypted product key. Never rewrite sales or account
// configuration while reconnecting. The outbox retries cache propagation.
func syncAccountCredentialTx(ctx context.Context, tx *sql.Tx, remoteProductID int64, key string) error {
	_, err := tx.ExecContext(ctx, `WITH changed AS (
		UPDATE accounts a SET credentials=jsonb_set(COALESCE(a.credentials,'{}'::jsonb),'{api_key}',to_jsonb($2::text)),updated_at=NOW()
		FROM moshu_products p WHERE p.remote_product_id=$1 AND p.local_account_id=a.id
		AND a.deleted_at IS NULL
		RETURNING a.id
	)
	INSERT INTO scheduler_outbox(event_type,account_id,payload)
	SELECT 'account_changed',c.id,jsonb_build_object('group_ids',
		COALESCE((SELECT jsonb_agg(ag.group_id) FROM account_groups ag WHERE ag.account_id=c.id),'[]'::jsonb))
	FROM changed c`, remoteProductID, key)
	return err
}
