package moshureseller

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// Only managed upstream resources change. There are deliberately no writes to
// local users, wallets, API keys, usage logs, settings or historical settlements.
// Reuse a channel only when the main group and platform identify it uniquely;
// product names/codes can be reused for unrelated groups and are not identity.
func prepareStationReauthorizationTx(ctx context.Context, tx *sql.Tx, catalog RemoteCatalog) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,remote_product_id,moshu_group_id,platform FROM moshu_products ORDER BY id FOR UPDATE`)
	if err != nil {
		return err
	}
	type channel struct {
		id, remoteID, groupID int64
		platform              string
	}
	var channels []channel
	for rows.Next() {
		var c channel
		if err := rows.Scan(&c.id, &c.remoteID, &c.groupID, &c.platform); err != nil {
			_ = rows.Close()
			return err
		}
		channels = append(channels, c)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	// Free globally unique product codes before persisting the new catalog.
	// Preserve old rows and their group/account IDs for history and revocation.
	if _, err := tx.ExecContext(ctx, `UPDATE moshu_products SET authorized=FALSE,product_code=$1::text||id::text,credential_ciphertext=NULL,credential_received_at=NULL,updated_at=NOW()`, "__retired:"+uuid.NewString()+":"); err != nil {
		return err
	}
	if err := clearRevokedCredentialsTx(ctx, tx); err != nil {
		return err
	}
	counts := map[string]int{}
	identity := func(group int64, platform string) string { return fmt.Sprintf("%d:%s", group, platform) }
	for _, p := range catalog.Products {
		counts[identity(p.MoshuGroupID, p.Platform)]++
	}
	remoteIDs := make(map[int64]bool, len(catalog.Products))
	for _, p := range catalog.Products {
		remoteIDs[p.ID] = true
	}
	for _, p := range catalog.Products {
		var matches []channel
		alreadyPresent := false
		for _, c := range channels {
			if c.remoteID == p.ID {
				alreadyPresent = true
			}
			if !remoteIDs[c.remoteID] && c.groupID == p.MoshuGroupID && c.platform == p.Platform {
				matches = append(matches, c)
			}
		}
		if alreadyPresent || len(matches) != 1 || counts[identity(p.MoshuGroupID, p.Platform)] != 1 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE moshu_products SET remote_product_id=$2,credential_ciphertext=NULL,credential_received_at=NULL,updated_at=NOW() WHERE id=$1`, matches[0].id, p.ID); err != nil {
			return err
		}
	}
	// Remote settlement IDs are globally unique on the unchanged main site.
	// Replay the new tenant's ledger, retaining the old ledger and local logs.
	if _, err := tx.ExecContext(ctx, `UPDATE moshu_settlement_cursors SET last_remote_id=0,updated_at=NOW() WHERE id=1`); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE moshu_reseller_connections SET last_settlement_sync_at=NULL WHERE id=1`)
	return err
}
