-- Custom extension: retain api_keys.group_id as the primary binding.
CREATE TABLE IF NOT EXISTS custom_api_key_routes (
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    position SMALLINT NOT NULL CHECK (position BETWEEN 1 AND 4),
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    PRIMARY KEY (api_key_id, position),
    UNIQUE (api_key_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_custom_api_key_routes_group_id
    ON custom_api_key_routes (group_id, api_key_id);

-- API keys are normally soft-deleted. Reclaim extension rows in the same
-- transaction for repository deletion, user deletion and direct SQL alike.
CREATE OR REPLACE FUNCTION cleanup_custom_api_key_routes_on_key_delete()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM custom_api_key_routes WHERE api_key_id = NEW.id;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS trg_cleanup_custom_api_key_routes ON api_keys;
CREATE TRIGGER trg_cleanup_custom_api_key_routes
AFTER UPDATE OF deleted_at ON api_keys
FOR EACH ROW WHEN (NEW.deleted_at IS NOT NULL)
EXECUTE FUNCTION cleanup_custom_api_key_routes_on_key_delete();

-- Preserve the durable invalidation contract even when a transaction commits
-- before an instance can publish its immediate cache invalidation.
CREATE OR REPLACE FUNCTION enqueue_custom_api_key_routes_auth_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        PERFORM enqueue_auth_cache_invalidation(k.key)
        FROM api_keys k WHERE k.id = OLD.api_key_id AND k.deleted_at IS NULL;
    END IF;
    IF TG_OP <> 'DELETE' THEN
        PERFORM enqueue_auth_cache_invalidation(k.key)
        FROM api_keys k WHERE k.id = NEW.api_key_id AND k.deleted_at IS NULL;
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS trg_custom_api_key_routes_auth_invalidation ON custom_api_key_routes;
CREATE TRIGGER trg_custom_api_key_routes_auth_invalidation
AFTER INSERT OR UPDATE OR DELETE ON custom_api_key_routes
FOR EACH ROW EXECUTE FUNCTION enqueue_custom_api_key_routes_auth_invalidation();

-- The upstream group trigger only knows the primary binding. Add a separate
-- extension trigger so disabling, deleting or changing a backup invalidates
-- every key that references it without replacing upstream trigger functions.
CREATE OR REPLACE FUNCTION enqueue_custom_api_key_backup_group_auth_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND
       (to_jsonb(OLD) - 'updated_at') IS NOT DISTINCT FROM (to_jsonb(NEW) - 'updated_at') THEN
        RETURN NEW;
    END IF;
    PERFORM enqueue_auth_cache_invalidation(k.key)
    FROM api_keys k
    WHERE k.deleted_at IS NULL AND EXISTS (
        SELECT 1 FROM custom_api_key_routes r
        WHERE r.api_key_id = k.id AND r.group_id = OLD.id
    );
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_custom_api_key_backup_group_auth_invalidation ON groups;
CREATE TRIGGER trg_custom_api_key_backup_group_auth_invalidation
BEFORE UPDATE OR DELETE ON groups
FOR EACH ROW EXECUTE FUNCTION enqueue_custom_api_key_backup_group_auth_invalidation();
