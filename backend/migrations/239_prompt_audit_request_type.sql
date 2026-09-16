-- Persist the usage-record request type on prompt-audit events so the admin
-- filter does not depend on a non-existent JSON snapshot column.
ALTER TABLE prompt_audit_events
    ADD COLUMN IF NOT EXISTS request_type VARCHAR(32) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_request_type
    ON prompt_audit_events(request_type, created_at DESC, id DESC);
