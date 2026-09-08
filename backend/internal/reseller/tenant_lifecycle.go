package reseller

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"
)

func isTenantConflict(err error) bool {
	var pg *pq.Error
	return errors.As(err, &pg) && pg.Code == "23505" &&
		(pg.Constraint == "reseller_tenants_user_active_uidx" || pg.Constraint == "reseller_tenants_name_active_uidx")
}

// Soft deletion preserves the billing user, balance and historical settlements.
func (s *Service) DeleteTenant(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = getTenantTx(ctx, tx, id, true)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	keys, err := s.keysForInvalidationTx(ctx, tx, id, 0)
	if err != nil {
		return err
	}
	queries := []string{
		`UPDATE api_keys SET status='inactive',deleted_at=NOW(),updated_at=NOW()
		 WHERE id IN (SELECT api_key_id FROM reseller_credentials WHERE reseller_id=$1) AND deleted_at IS NULL`,
		`UPDATE reseller_credentials SET status='revoked',updated_at=NOW() WHERE reseller_id=$1 AND status IN ('active','retiring')`,
		`UPDATE reseller_refresh_tokens SET revoked_at=NOW() WHERE reseller_id=$1 AND revoked_at IS NULL`,
		`UPDATE reseller_enrollment_codes SET used_at=NOW() WHERE reseller_id=$1 AND used_at IS NULL`,
		`UPDATE reseller_products SET enabled=FALSE,updated_at=NOW() WHERE reseller_id=$1`,
		`UPDATE reseller_tenants SET status='disabled',deleted_at=NOW(),updated_at=NOW() WHERE id=$1`,
	}
	for _, query := range queries {
		if _, err := tx.ExecContext(ctx, query, id); err != nil {
			return err
		}
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
