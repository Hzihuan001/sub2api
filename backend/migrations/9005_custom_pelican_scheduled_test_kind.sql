-- 9005: keep ordinary scheduled connection checks separate from Pelican showcase tests.
ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS test_kind TEXT NOT NULL DEFAULT 'connection';

ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS test_prompt TEXT NOT NULL DEFAULT '';

ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS group_id BIGINT NULL REFERENCES groups(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_stp_kind_enabled_next_run
    ON scheduled_test_plans(test_kind, enabled, next_run_at)
    WHERE enabled = true;

CREATE INDEX IF NOT EXISTS idx_stp_group_id ON scheduled_test_plans(group_id);
