//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration9000BatchImageNoDiscount(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("9000_custom_batch_image_no_discount.sql")
	require.NoError(t, err)

	// The harness applies every migration first. Restore the old defaults so
	// omitting either ALTER DEFAULT from this migration would fail the test.
	_, err = tx.ExecContext(ctx, `
ALTER TABLE groups
    ALTER COLUMN batch_image_discount_multiplier SET DEFAULT 0.5,
    ALTER COLUMN batch_image_hold_multiplier SET DEFAULT 0.6;
ALTER TABLE batch_image_jobs
    ALTER COLUMN batch_discount_multiplier SET DEFAULT 0.5,
    ALTER COLUMN hold_multiplier SET DEFAULT 0.6;`)
	require.NoError(t, err)

	type legacyGroup struct {
		id            int64
		name          string
		hold          float64
		ordinaryState string
	}
	var groups []legacyGroup
	for _, discount := range []float64{0, 0.5, 0.8, 1, 1.2} {
		for _, hold := range []float64{0.6, 2} {
			group := legacyGroup{
				name: fmt.Sprintf("batch-no-discount-%g-%g", discount, hold),
				hold: max(hold, 1),
			}
			require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (
    name, platform, rate_multiplier, image_rate_independent, image_rate_multiplier,
    batch_image_discount_multiplier, batch_image_hold_multiplier, updated_at
)
VALUES ($1, 'gemini', 0.75, TRUE, 1.25, $2, $3, '2025-01-01T00:00:00Z')
RETURNING id`, group.name, discount, hold).Scan(&group.id))
			require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (to_jsonb(g) - 'batch_image_discount_multiplier' - 'batch_image_hold_multiplier')::text
FROM groups AS g WHERE id = $1`, group.id).Scan(&group.ordinaryState))
			groups = append(groups, group)
		}
	}

	// Both in-flight and settled jobs retain their original pricing snapshot,
	// hold, and charged amounts. Compare the entire row to catch repricing of
	// any related field, including timestamps and the snapshot version.
	legacyJobs := make(map[int64]string)
	for _, status := range []string{"running", "completed"} {
		var actualCost any
		if status == "completed" {
			actualCost = 0.75
		}
		var jobID int64
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO batch_image_jobs (
    batch_id, user_id, provider, model, status, item_count, success_count,
    base_unit_price, group_rate_multiplier, account_rate_multiplier,
    batch_discount_multiplier, hold_multiplier, billable_unit_price, hold_unit_price,
    pricing_snapshot_version, estimated_cost, hold_amount, actual_cost, updated_at
)
VALUES ($1, 1001, 'gemini_api', 'gemini-2.5-flash-image', $2, 6, 6,
    0.25, 0.8, 1.25, 0.5, 0.6, 0.125, 0.15, 1, 0.75, 0.9, $3,
    '2025-01-01T00:00:00Z')
RETURNING id`, "imgbatch_no_discount_legacy_"+status, status, actualCost).Scan(&jobID))
		var state string
		require.NoError(t, tx.QueryRowContext(ctx,
			`SELECT to_jsonb(j)::text FROM batch_image_jobs AS j WHERE id = $1`, jobID).Scan(&state))
		legacyJobs[jobID] = state
	}

	for pass := 1; pass <= 2; pass++ {
		_, err = tx.ExecContext(ctx, string(migrationSQL))
		require.NoError(t, err, "migration pass %d", pass)

		for _, group := range groups {
			var discount, hold float64
			var ordinaryState string
			require.NoError(t, tx.QueryRowContext(ctx, `
SELECT batch_image_discount_multiplier, batch_image_hold_multiplier,
    (to_jsonb(g) - 'batch_image_discount_multiplier' - 'batch_image_hold_multiplier')::text
FROM groups AS g WHERE id = $1`, group.id).Scan(&discount, &hold, &ordinaryState))
			require.Equal(t, 1.0, discount, "%s, pass %d", group.name, pass)
			require.Equal(t, group.hold, hold, "%s, pass %d", group.name, pass)
			require.JSONEq(t, group.ordinaryState, ordinaryState,
				"ordinary group/image pricing must remain unchanged: %s, pass %d", group.name, pass)
		}
		for jobID, before := range legacyJobs {
			var after string
			require.NoError(t, tx.QueryRowContext(ctx,
				`SELECT to_jsonb(j)::text FROM batch_image_jobs AS j WHERE id = $1`, jobID).Scan(&after))
			require.JSONEq(t, before, after, "legacy job %d, pass %d", jobID, pass)
		}

		var groupDiscount, groupHold, jobDiscount, jobHold float64
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, platform)
VALUES ($1, 'gemini')
RETURNING batch_image_discount_multiplier, batch_image_hold_multiplier`,
			fmt.Sprintf("batch-no-discount-new-%d", pass)).Scan(&groupDiscount, &groupHold))
		require.Equal(t, 1.0, groupDiscount, "new group discount, pass %d", pass)
		require.Equal(t, 1.0, groupHold, "new group hold, pass %d", pass)
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO batch_image_jobs (batch_id, user_id, provider, model, item_count)
VALUES ($1, 1001, 'gemini_api', 'gemini-2.5-flash-image', 1)
RETURNING batch_discount_multiplier, hold_multiplier`,
			fmt.Sprintf("imgbatch_no_discount_new_%d", pass)).Scan(&jobDiscount, &jobHold))
		require.Equal(t, 1.0, jobDiscount, "new job discount, pass %d", pass)
		require.Equal(t, 1.0, jobHold, "new job hold, pass %d", pass)
	}
}
