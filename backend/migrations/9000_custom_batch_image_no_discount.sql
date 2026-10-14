-- App-managed batches fan out to ordinary single-image requests. No batch
-- provider gets an implicit retail discount. Keep the columns for protocol
-- compatibility and for immutable pricing snapshots on existing jobs.
ALTER TABLE groups
    ALTER COLUMN batch_image_discount_multiplier SET DEFAULT 1.0,
    ALTER COLUMN batch_image_hold_multiplier SET DEFAULT 1.0;

UPDATE groups
SET batch_image_discount_multiplier = 1.0,
    batch_image_hold_multiplier = GREATEST(batch_image_hold_multiplier, 1.0)
WHERE batch_image_discount_multiplier IS DISTINCT FROM 1.0
   OR batch_image_hold_multiplier < 1.0;

COMMENT ON COLUMN groups.batch_image_discount_multiplier IS
    '兼容字段；新批量任务不提供额外折扣，固定为 1';

-- Defaults affect future inserts only. Never reprice pending/completed jobs,
-- their frozen balances, usage logs, or previously captured charges.
ALTER TABLE batch_image_jobs
    ALTER COLUMN batch_discount_multiplier SET DEFAULT 1.0,
    ALTER COLUMN hold_multiplier SET DEFAULT 1.0;
