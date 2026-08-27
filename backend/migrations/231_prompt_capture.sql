-- Passive prompt capture mode. This mode stores only the latest user input,
-- never calls a Guard endpoint, and is processed off the request path.

ALTER TABLE prompt_audit_jobs
    DROP CONSTRAINT IF EXISTS chk_prompt_audit_jobs_execution_mode;

ALTER TABLE prompt_audit_jobs
    ADD CONSTRAINT chk_prompt_audit_jobs_execution_mode
        CHECK (execution_mode IN ('async_audit', 'blocking', 'capture_only'));

ALTER TABLE prompt_audit_events
    ADD COLUMN IF NOT EXISTS capture_mode VARCHAR(32) NOT NULL DEFAULT 'guard_audit',
    ADD COLUMN IF NOT EXISTS prompt_bytes BIGINT NOT NULL DEFAULT 0;

ALTER TABLE prompt_audit_events
    DROP CONSTRAINT IF EXISTS chk_prompt_audit_events_decision;

ALTER TABLE prompt_audit_events
    ADD CONSTRAINT chk_prompt_audit_events_decision
        CHECK (decision IN ('unreviewed', 'pass', 'flag', 'critical'));

ALTER TABLE prompt_audit_events
    DROP CONSTRAINT IF EXISTS chk_prompt_audit_events_risk_level;

ALTER TABLE prompt_audit_events
    ADD CONSTRAINT chk_prompt_audit_events_risk_level
        CHECK (risk_level IN ('unknown', 'low', 'medium', 'high', 'critical'));

UPDATE prompt_audit_events
SET prompt_bytes = octet_length(full_prompt)
WHERE prompt_bytes = 0 AND full_prompt <> '';

ALTER TABLE prompt_audit_events
    DROP CONSTRAINT IF EXISTS chk_prompt_audit_events_capture_mode;

ALTER TABLE prompt_audit_events
    ADD CONSTRAINT chk_prompt_audit_events_capture_mode
        CHECK (capture_mode IN ('guard_audit', 'capture_only'));

ALTER TABLE prompt_audit_events
    DROP CONSTRAINT IF EXISTS chk_prompt_audit_events_prompt_bytes;

ALTER TABLE prompt_audit_events
    ADD CONSTRAINT chk_prompt_audit_events_prompt_bytes
        CHECK (prompt_bytes >= 0);

CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_capture_created
    ON prompt_audit_events(capture_mode, created_at DESC, id DESC);
