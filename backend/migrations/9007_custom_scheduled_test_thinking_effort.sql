-- 9007: persist optional reasoning strength for scheduled Pelican tests.
-- Existing plans keep provider defaults so their request payloads do not change.
ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS thinking_effort TEXT NOT NULL DEFAULT 'default';
