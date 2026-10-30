-- 9006: make scheduled tests claimable and idempotent across workers.
-- Additive only: old application images ignore these nullable/defaulted columns.
ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS lease_token TEXT NULL,
    ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS active_run_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS failure_streak INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';

ALTER TABLE scheduled_test_results
    ADD COLUMN IF NOT EXISTS run_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_stp_due_lease
    ON scheduled_test_plans(enabled, next_run_at, lease_expires_at)
    WHERE enabled = true;

CREATE UNIQUE INDEX IF NOT EXISTS idx_str_plan_run_id
    ON scheduled_test_results(plan_id, run_id)
    WHERE run_id <> '';
