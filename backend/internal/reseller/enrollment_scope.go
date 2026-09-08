package reseller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

// Future tenant-isolated restoration belongs here. This release only records
// the attempt and rejects it before consuming the code or issuing keys.
func rebindNotSupported(currentID, targetID int64) error {
	slog.Info("reseller.rebind_rejected", "current_reseller_id", currentID, "target_reseller_id", targetID, "reason", "not_supported")
	return fmt.Errorf("%w: 本次仅支持同一代理商重新授权，不支持切换代理商", ErrInvalidInput)
}

func checkInstanceBindingTx(ctx context.Context, tx *sql.Tx, resellerID int64, instanceID string) error {
	var boundID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM reseller_tenants WHERE instance_id=$1::uuid`, instanceID).Scan(&boundID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if boundID != resellerID {
		return rebindNotSupported(boundID, resellerID)
	}
	return nil
}

func listGrantedProductsTx(ctx context.Context, tx *sql.Tx, resellerID int64) ([]Product, error) {
	rows, err := tx.QueryContext(ctx, `SELECT rp.id,rp.reseller_id,rp.moshu_group_id,rp.product_code,rp.display_name,
		rp.platform,rp.enabled,rp.cost_rate_multiplier,rp.price_catalog_version,
		rp.model_snapshot,rp.capabilities,rp.effective_at,TRUE,rp.created_at,rp.updated_at
		FROM reseller_products rp JOIN groups g ON g.id=rp.moshu_group_id
		WHERE rp.reseller_id=$1 AND rp.enabled=TRUE AND g.deleted_at IS NULL
		AND g.status='active' AND g.subscription_type='standard'
		AND EXISTS(SELECT 1 FROM reseller_credentials rc WHERE rc.product_id=rp.id AND rc.status='active')
		ORDER BY rp.product_code`, resellerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	products := make([]Product, 0)
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, *p)
	}
	return products, rows.Err()
}
