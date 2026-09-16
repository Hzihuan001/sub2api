-- Keep the request type through asynchronous job claim/retry/completion.
-- Historical jobs retain unknown (''); do not infer a type from an endpoint.
ALTER TABLE prompt_audit_jobs
    ADD COLUMN IF NOT EXISTS request_type VARCHAR(32) NOT NULL DEFAULT '';
