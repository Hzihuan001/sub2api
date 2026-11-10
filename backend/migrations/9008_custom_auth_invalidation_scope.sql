-- Route revisions change for both security and bookkeeping updates. Only a
-- real route/policy edit should enqueue another auth-cache invalidation; the
-- upstream key/group triggers already cover their own security mutations.
CREATE OR REPLACE FUNCTION enqueue_custom_api_key_routes_auth_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'custom_api_key_account_routes' THEN
        IF TG_OP = 'UPDATE'
           AND OLD.api_key_id IS NOT DISTINCT FROM NEW.api_key_id
           AND OLD.scope IS NOT DISTINCT FROM NEW.scope THEN
            RETURN NULL;
        END IF;
    END IF;
    IF TG_OP <> 'INSERT' THEN
        PERFORM enqueue_auth_cache_invalidation(k.key)
        FROM api_keys k WHERE k.id = OLD.api_key_id AND k.deleted_at IS NULL;
    END IF;
    IF TG_OP <> 'DELETE'
       AND (TG_OP = 'INSERT' OR OLD.api_key_id IS DISTINCT FROM NEW.api_key_id) THEN
        PERFORM enqueue_auth_cache_invalidation(k.key)
        FROM api_keys k WHERE k.id = NEW.api_key_id AND k.deleted_at IS NULL;
    END IF;
    RETURN NULL;
END;
$$;

-- Keep every group routing/security/pricing field covered, including fields
-- added after migration 193, while excluding presentation-only edits.
CREATE OR REPLACE FUNCTION custom_auth_group_configuration_changed(before_row JSONB, after_row JSONB)
RETURNS BOOLEAN LANGUAGE SQL IMMUTABLE AS $$
    SELECT (before_row - ARRAY['name', 'description', 'sort_order', 'created_at', 'updated_at'])
        IS DISTINCT FROM
           (after_row - ARRAY['name', 'description', 'sort_order', 'created_at', 'updated_at']);
$$;

CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND NOT custom_auth_group_configuration_changed(to_jsonb(OLD), to_jsonb(NEW)) THEN
        RETURN NEW;
    END IF;
    PERFORM enqueue_auth_cache_invalidation(k.key)
    FROM api_keys k
    WHERE k.group_id = OLD.id AND k.deleted_at IS NULL;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

-- A backup binding needs the same invalidation boundary as a primary binding.
CREATE OR REPLACE FUNCTION enqueue_custom_api_key_backup_group_auth_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND NOT custom_auth_group_configuration_changed(to_jsonb(OLD), to_jsonb(NEW)) THEN
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

-- RestrictPublicGroups and allowed-group membership are part of the user auth
-- snapshot. Preserve their invalidation after revision-only events are removed.
CREATE OR REPLACE FUNCTION enqueue_user_auth_cache_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.role IS NOT DISTINCT FROM NEW.role
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at
       AND OLD.restrict_public_groups IS NOT DISTINCT FROM NEW.restrict_public_groups THEN
        RETURN NEW;
    END IF;
    PERFORM enqueue_auth_cache_invalidation(k.key)
    FROM api_keys k WHERE k.user_id = OLD.id AND k.deleted_at IS NULL;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_allowed_group_auth_cache_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.user_id IS NOT DISTINCT FROM NEW.user_id
       AND OLD.group_id IS NOT DISTINCT FROM NEW.group_id THEN
        RETURN NEW;
    END IF;
    IF TG_OP <> 'INSERT' THEN
        PERFORM enqueue_auth_cache_invalidation(k.key)
        FROM api_keys k WHERE k.user_id = OLD.user_id AND k.deleted_at IS NULL;
    END IF;
    IF TG_OP <> 'DELETE'
       AND (TG_OP = 'INSERT' OR OLD.user_id IS DISTINCT FROM NEW.user_id) THEN
        PERFORM enqueue_auth_cache_invalidation(k.key)
        FROM api_keys k WHERE k.user_id = NEW.user_id AND k.deleted_at IS NULL;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
