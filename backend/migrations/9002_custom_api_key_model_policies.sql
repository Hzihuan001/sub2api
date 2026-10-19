-- Exact public-model access restrictions are independent of upstream Ent.
CREATE TABLE IF NOT EXISTS custom_api_key_model_policies (
    api_key_id BIGINT PRIMARY KEY REFERENCES api_keys(id) ON DELETE CASCADE,
    policy JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE OR REPLACE FUNCTION cleanup_custom_api_key_model_policy_on_key_delete()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM custom_api_key_model_policies WHERE api_key_id = NEW.id;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_cleanup_custom_api_key_model_policy_on_key_delete ON api_keys;
CREATE TRIGGER trg_cleanup_custom_api_key_model_policy_on_key_delete
AFTER UPDATE OF deleted_at ON api_keys
FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
EXECUTE FUNCTION cleanup_custom_api_key_model_policy_on_key_delete();

-- Reuse the durable key invalidation outbox installed by custom route 9000.
DROP TRIGGER IF EXISTS trg_custom_api_key_model_policy_auth_invalidation ON custom_api_key_model_policies;
CREATE TRIGGER trg_custom_api_key_model_policy_auth_invalidation
AFTER INSERT OR UPDATE OR DELETE ON custom_api_key_model_policies
FOR EACH ROW EXECUTE FUNCTION enqueue_custom_api_key_routes_auth_invalidation();
