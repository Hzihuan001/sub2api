package reseller

import (
	"context"
	"database/sql"
	"errors"
)

// Keep ledger foreign keys intact while removing keys from the user's key list.
func revokeProductKeysTx(ctx context.Context, tx *sql.Tx, resellerID, productID int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE api_keys SET status='inactive',deleted_at=NOW(),updated_at=NOW()
		WHERE id IN (SELECT api_key_id FROM reseller_credentials WHERE reseller_id=$1 AND product_id=$2)
		AND deleted_at IS NULL`, resellerID, productID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE reseller_credentials SET status='revoked',updated_at=NOW()
		WHERE reseller_id=$1 AND product_id=$2 AND status IN ('active','retiring')`, resellerID, productID); err != nil {
		return err
	}
	// An old, unused code must not restore a product after deletion and re-addition.
	_, err := tx.ExecContext(ctx, `UPDATE reseller_enrollment_codes SET used_at=NOW()
		WHERE reseller_id=$1 AND used_at IS NULL AND product_ids @> jsonb_build_array($2::bigint)`, resellerID, productID)
	return err
}

func (s *Service) DeleteProduct(ctx context.Context, resellerID, productID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = getTenantTx(ctx, tx, resellerID, true)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	keys, err := s.keysForInvalidationTx(ctx, tx, resellerID, productID)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE reseller_products SET enabled=FALSE,
		price_catalog_version=price_catalog_version+1,updated_at=NOW()
		WHERE reseller_id=$1 AND id=$2`, resellerID, productID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	if err := revokeProductKeysTx(ctx, tx, resellerID, productID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.apiKeys != nil {
		for _, key := range keys {
			s.apiKeys.InvalidateAuthCacheByKey(ctx, key)
		}
	}
	return nil
}

// Read before soft deletion: the generic per-user key listing excludes deleted keys.
func (s *Service) keysForInvalidationTx(ctx context.Context, tx *sql.Tx, resellerID, productID int64) ([]string, error) {
	if s.apiKeys == nil {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT ak.key FROM api_keys ak JOIN reseller_credentials rc ON rc.api_key_id=ak.id
		WHERE rc.reseller_id=$1 AND ($2::bigint=0 OR rc.product_id=$2::bigint) AND ak.deleted_at IS NULL`, resellerID, productID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func grantedProducts(products []Product) []Product {
	result := make([]Product, 0, len(products))
	for _, p := range products {
		if p.Enabled && p.CredentialConfigured {
			result = append(result, p)
		}
	}
	return result
}
