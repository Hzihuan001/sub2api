package reseller

import (
	"context"
	"encoding/json"
)

// Refresh at catalog reads (the client polls every five minutes), never on the
// inference hot path. Compare against the source again on write so a concurrent
// group edit cannot be overwritten with an older snapshot.
func (s *Service) refreshProductSnapshots(ctx context.Context, resellerID int64) error {
	rows, err := s.db.QueryContext(ctx, `SELECT rp.id,g.id,g.platform,COALESCE(ugr.rate_multiplier,g.rate_multiplier),
		COALESCE(g.model_allowlist,'{}'::jsonb)
		FROM reseller_products rp JOIN groups g ON g.id=rp.moshu_group_id
		JOIN reseller_tenants rt ON rt.id=rp.reseller_id
		LEFT JOIN user_group_rate_multipliers ugr ON ugr.user_id=rt.user_id AND ugr.group_id=g.id
		WHERE rp.reseller_id=$1 AND g.deleted_at IS NULL AND g.subscription_type='standard'`, resellerID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	type snapshot struct {
		id, groupID int64
		platform    string
		rate        float64
		raw         []byte
	}
	var snapshots []snapshot
	for rows.Next() {
		var item snapshot
		if err := rows.Scan(&item.id, &item.groupID, &item.platform, &item.rate, &item.raw); err != nil {
			return err
		}
		snapshots = append(snapshots, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range snapshots {
		models, err := json.Marshal(extractModelsSnapshot(item.raw))
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `UPDATE reseller_products rp SET
		 platform=$3::text,cost_rate_multiplier=$4::numeric,model_snapshot=$5::jsonb,
		 capabilities=jsonb_set(COALESCE(rp.capabilities,'{}'::jsonb),'{platform}',to_jsonb($3::text)),
		 price_catalog_version=rp.price_catalog_version+1,effective_at=NOW(),updated_at=NOW()
			 FROM groups g CROSS JOIN reseller_tenants rt
		 LEFT JOIN user_group_rate_multipliers ugr ON ugr.user_id=rt.user_id AND ugr.group_id=$2::bigint
		 WHERE rp.id=$1::bigint AND rp.moshu_group_id=$2::bigint AND g.id=$2::bigint AND rt.id=rp.reseller_id
		 AND g.deleted_at IS NULL AND g.subscription_type='standard'
		 AND g.platform=$3::text AND COALESCE(ugr.rate_multiplier,g.rate_multiplier)=$4::numeric AND COALESCE(g.model_allowlist,'{}'::jsonb)=$6::jsonb
		 AND (rp.platform IS DISTINCT FROM $3::text OR rp.cost_rate_multiplier IS DISTINCT FROM $4::numeric OR rp.model_snapshot IS DISTINCT FROM $5::jsonb)`, item.id, item.groupID, item.platform, item.rate, models, item.raw)
		if err != nil {
			return err
		}
	}
	return nil
}
