package reseller

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
)

// A new enrollment code authorizes the target; possession of the previous
// station refresh secret authorizes releasing the source binding. An instance
// UUID or reseller ID alone is not proof. Deleted tenants retain token hashes,
// so recovery does not require reactivating their user, keys or tenant.
func (s *Service) releasePreviousBindingTx(ctx context.Context, tx *sql.Tx, previousID int64, instanceID, refresh string) ([]string, error) {
	if refresh == "" {
		return nil, fmt.Errorf("%w: missing previous station credential", ErrInvalidInput)
	}
	var previousInstance sql.NullString
	var deleted bool
	if err := tx.QueryRowContext(ctx, `SELECT instance_id::text,deleted_at IS NOT NULL FROM reseller_tenants WHERE id=$1 FOR UPDATE`, previousID).Scan(&previousInstance, &deleted); err != nil {
		return nil, ErrForbidden
	}
	// NULL is allowed for a retry after the remote commit but before L1 saved it.
	if previousInstance.Valid && previousInstance.String != instanceID {
		return nil, ErrForbidden
	}
	hash := sha256.Sum256([]byte(refresh))
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM reseller_refresh_tokens
		WHERE reseller_id=$1 AND instance_id=$2::uuid AND token_hash=$3
		AND ($4::boolean OR (revoked_at IS NULL AND expires_at>NOW())))`, previousID, instanceID, hash[:], deleted || !previousInstance.Valid).Scan(&valid)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, ErrForbidden
	}
	keys, err := s.keysForInvalidationTx(ctx, tx, previousID, 0)
	if err != nil {
		return nil, err
	}
	for _, query := range []string{
		`UPDATE api_keys SET status='inactive',deleted_at=NOW(),updated_at=NOW() WHERE id IN (SELECT api_key_id FROM reseller_credentials WHERE reseller_id=$1) AND deleted_at IS NULL`,
		`UPDATE reseller_credentials SET status='revoked',updated_at=NOW() WHERE reseller_id=$1 AND status IN ('active','retiring')`,
		`UPDATE reseller_refresh_tokens SET revoked_at=NOW() WHERE reseller_id=$1 AND revoked_at IS NULL`,
		`UPDATE reseller_enrollment_codes SET used_at=NOW() WHERE reseller_id=$1 AND used_at IS NULL`,
		`UPDATE reseller_tenants SET instance_id=NULL,updated_at=NOW() WHERE id=$1`,
	} {
		if _, err := tx.ExecContext(ctx, query, previousID); err != nil {
			return nil, err
		}
	}
	return keys, nil
}
