-- Optional per-API-key failover policy. A NULL row means the key inherits
-- the selected group's policy; an empty rules array explicitly disables it.
CREATE TABLE IF NOT EXISTS custom_api_key_route_policies (
    api_key_id BIGINT PRIMARY KEY REFERENCES api_keys(id) ON DELETE CASCADE,
    policy JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_custom_api_key_route_policies_updated_at
    ON custom_api_key_route_policies(updated_at);

-- API keys are soft-deleted by the application. Remove the extension row at
-- the same boundary so a later restore cannot resurrect stale routing rules.
CREATE OR REPLACE FUNCTION cleanup_custom_api_key_route_policy_on_key_delete()
RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM custom_api_key_route_policies WHERE api_key_id = NEW.id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_cleanup_custom_api_key_route_policy_on_key_delete ON api_keys;
CREATE TRIGGER trg_cleanup_custom_api_key_route_policy_on_key_delete
AFTER UPDATE OF deleted_at ON api_keys
FOR EACH ROW
WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
EXECUTE FUNCTION cleanup_custom_api_key_route_policy_on_key_delete();

-- Reuse the durable API-key cache invalidation function installed by 9000.
-- Policy edits must invalidate auth snapshots just like route edits.
DROP TRIGGER IF EXISTS trg_custom_api_key_route_policy_auth_invalidation ON custom_api_key_route_policies;
CREATE TRIGGER trg_custom_api_key_route_policy_auth_invalidation
AFTER INSERT OR UPDATE OR DELETE ON custom_api_key_route_policies
FOR EACH ROW EXECUTE FUNCTION enqueue_custom_api_key_routes_auth_invalidation();
