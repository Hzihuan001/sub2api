package moshureseller

import (
	"context"
	"database/sql"
)

// Clear stale plaintext/ciphertext keys with the catalog transaction. The
// scheduler outbox propagates account changes even if later reconciliation fails.
func clearRevokedCredentialsTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `WITH changed AS (
		UPDATE accounts a SET credentials=COALESCE(a.credentials,'{}'::jsonb)-'api_key',schedulable=FALSE,updated_at=NOW()
		FROM moshu_products p WHERE p.authorized=FALSE AND p.local_account_id=a.id AND a.deleted_at IS NULL
		AND (a.schedulable=TRUE OR a.credentials ? 'api_key') RETURNING a.id
	) INSERT INTO scheduler_outbox(event_type,account_id,payload)
	SELECT 'account_changed',c.id,jsonb_build_object('group_ids',
		COALESCE((SELECT jsonb_agg(ag.group_id) FROM account_groups ag WHERE ag.account_id=c.id),'[]'::jsonb)) FROM changed c`)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE moshu_products SET credential_ciphertext=NULL,credential_received_at=NULL,updated_at=NOW()
		WHERE authorized=FALSE AND credential_ciphertext IS NOT NULL`)
	return err
}
