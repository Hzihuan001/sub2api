-- Account-local routing is opt-in. Do not rewrite the legacy rule payload:
-- old binaries must still be able to read null / [] / non-empty policies.
CREATE SEQUENCE IF NOT EXISTS custom_key_route_revision_seq AS BIGINT;

CREATE TABLE IF NOT EXISTS custom_api_key_account_routes (
    api_key_id BIGINT PRIMARY KEY REFERENCES api_keys(id) ON DELETE CASCADE,
    scope TEXT NOT NULL DEFAULT 'group_v1' CHECK (scope IN ('group_v1', 'account_v2')),
    revision BIGINT NOT NULL DEFAULT nextval('custom_key_route_revision_seq') CHECK (revision > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS custom_group_route_revisions (
    group_id BIGINT PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL DEFAULT nextval('custom_key_route_revision_seq') CHECK (revision > 0)
);

-- Tombstones survive soft deletion and rule removal. Re-enabling identical
-- rules or restoring a key must never reuse an earlier generation.
INSERT INTO custom_api_key_account_routes (api_key_id)
SELECT id FROM api_keys ON CONFLICT (api_key_id) DO NOTHING;
INSERT INTO custom_group_route_revisions (group_id)
SELECT id FROM groups ON CONFLICT (group_id) DO NOTHING;

CREATE OR REPLACE FUNCTION bump_custom_key_route_revision(target_key_id BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO custom_api_key_account_routes (api_key_id)
    SELECT id FROM api_keys WHERE id = target_key_id
    ON CONFLICT (api_key_id) DO UPDATE
      SET revision = nextval('custom_key_route_revision_seq'), updated_at = NOW();
END;
$$;

-- Allocate under the metadata row's write lock, not using an earlier EXCLUDED
-- value: concurrent writers must not overwrite a newer revision with an older one.
CREATE OR REPLACE FUNCTION version_custom_key_route_scope()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.scope IS DISTINCT FROM NEW.scope THEN
        NEW.revision := nextval('custom_key_route_revision_seq');
        NEW.updated_at := NOW();
    ELSIF NEW.revision < OLD.revision THEN
        RAISE EXCEPTION 'API key route revision cannot move backwards';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_version_custom_key_route_scope
BEFORE UPDATE ON custom_api_key_account_routes
FOR EACH ROW EXECUTE FUNCTION version_custom_key_route_scope();
CREATE TRIGGER trg_custom_key_route_metadata_auth_invalidation
AFTER INSERT OR UPDATE OR DELETE ON custom_api_key_account_routes
FOR EACH ROW EXECUTE FUNCTION enqueue_custom_api_key_routes_auth_invalidation();

CREATE OR REPLACE FUNCTION version_custom_key_route_binding()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' OR OLD.group_id IS DISTINCT FROM NEW.group_id
       OR OLD.user_id IS DISTINCT FROM NEW.user_id OR OLD.status IS DISTINCT FROM NEW.status
       OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at OR OLD.key IS DISTINCT FROM NEW.key
       OR OLD.expires_at IS DISTINCT FROM NEW.expires_at
       OR OLD.ip_whitelist IS DISTINCT FROM NEW.ip_whitelist
       OR OLD.ip_blacklist IS DISTINCT FROM NEW.ip_blacklist THEN
        PERFORM bump_custom_key_route_revision(NEW.id);
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_version_custom_key_route_binding
AFTER INSERT OR UPDATE ON api_keys
FOR EACH ROW EXECUTE FUNCTION version_custom_key_route_binding();

CREATE OR REPLACE FUNCTION version_custom_key_route_extension()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        PERFORM bump_custom_key_route_revision(OLD.api_key_id);
    END IF;
    IF TG_OP <> 'DELETE' THEN
        IF TG_OP = 'INSERT' OR OLD.api_key_id IS DISTINCT FROM NEW.api_key_id THEN
            PERFORM bump_custom_key_route_revision(NEW.api_key_id);
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER trg_version_custom_key_routes
AFTER INSERT OR UPDATE OR DELETE ON custom_api_key_routes
FOR EACH ROW EXECUTE FUNCTION version_custom_key_route_extension();
CREATE TRIGGER trg_version_custom_key_route_policy
AFTER INSERT OR UPDATE OR DELETE ON custom_api_key_route_policies
FOR EACH ROW EXECUTE FUNCTION version_custom_key_route_extension();
CREATE TRIGGER trg_version_custom_key_model_policy
AFTER INSERT OR UPDATE OR DELETE ON custom_api_key_model_policies
FOR EACH ROW EXECUTE FUNCTION version_custom_key_route_extension();

-- Version inherited policy changes directly in the settings transaction. This
-- also covers older binaries and direct SQL, including delete/recreate cycles.
CREATE OR REPLACE FUNCTION bump_custom_group_route_policy(setting_key TEXT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    target_group_id BIGINT;
BEGIN
    IF setting_key !~ '^custom\.gateway-resilience\.group-failure-policy\.[1-9][0-9]{0,18}$' THEN
        RETURN;
    END IF;
    BEGIN
        target_group_id := substring(setting_key FROM '[0-9]+$')::BIGINT;
    EXCEPTION WHEN numeric_value_out_of_range THEN
        RETURN;
    END;
    INSERT INTO custom_group_route_revisions (group_id)
    SELECT id FROM groups WHERE id = target_group_id
    ON CONFLICT (group_id) DO UPDATE
      SET revision = nextval('custom_key_route_revision_seq');
END;
$$;
CREATE OR REPLACE FUNCTION version_custom_group_route_policy()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.key IS NOT DISTINCT FROM NEW.key
       AND OLD.value IS NOT DISTINCT FROM NEW.value THEN
        RETURN NULL;
    END IF;
    IF TG_OP <> 'INSERT' THEN
        PERFORM bump_custom_group_route_policy(OLD.key);
    END IF;
    IF TG_OP <> 'DELETE' THEN
        IF TG_OP = 'INSERT' OR OLD.key IS DISTINCT FROM NEW.key THEN
            PERFORM bump_custom_group_route_policy(NEW.key);
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER trg_version_custom_group_route_policy
AFTER INSERT OR UPDATE OR DELETE ON settings
FOR EACH ROW EXECUTE FUNCTION version_custom_group_route_policy();

-- Access revocation/restoration is a generation boundary too, even when the
-- literal routing rule has not changed. Financial counters are deliberately
-- excluded so normal billing does not invalidate every routing request.
CREATE OR REPLACE FUNCTION version_custom_key_route_group_access()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    target_key_id BIGINT;
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO custom_group_route_revisions (group_id) VALUES (NEW.id)
        ON CONFLICT (group_id) DO NOTHING;
        RETURN NEW;
    END IF;
    IF (to_jsonb(OLD) - 'updated_at') IS NOT DISTINCT FROM (to_jsonb(NEW) - 'updated_at') THEN
        RETURN NEW;
    END IF;
    FOR target_key_id IN
        SELECT k.id FROM api_keys k WHERE k.group_id = NEW.id OR EXISTS (
            SELECT 1 FROM custom_api_key_routes r WHERE r.api_key_id = k.id AND r.group_id = NEW.id)
        ORDER BY k.id
    LOOP
        PERFORM bump_custom_key_route_revision(target_key_id);
    END LOOP;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_version_custom_key_route_group_access
AFTER INSERT OR UPDATE ON groups
FOR EACH ROW EXECUTE FUNCTION version_custom_key_route_group_access();

CREATE OR REPLACE FUNCTION version_custom_key_route_user_access()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    target_user_id BIGINT;
    target_key_id BIGINT;
BEGIN
    IF TG_TABLE_NAME = 'users' THEN
        IF OLD.status IS NOT DISTINCT FROM NEW.status AND OLD.role IS NOT DISTINCT FROM NEW.role
           AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at
           AND OLD.restrict_public_groups IS NOT DISTINCT FROM NEW.restrict_public_groups THEN
            RETURN NULL;
        END IF;
        target_user_id := NEW.id;
    ELSIF TG_OP = 'INSERT' THEN
        target_user_id := NEW.user_id;
    ELSE
        target_user_id := OLD.user_id;
    END IF;
    FOR target_key_id IN SELECT id FROM api_keys WHERE user_id = target_user_id ORDER BY id LOOP
        PERFORM bump_custom_key_route_revision(target_key_id);
    END LOOP;
    IF TG_TABLE_NAME = 'user_allowed_groups' AND TG_OP = 'UPDATE' THEN
        IF OLD.user_id IS DISTINCT FROM NEW.user_id THEN
            FOR target_key_id IN SELECT id FROM api_keys WHERE user_id = NEW.user_id ORDER BY id LOOP
                PERFORM bump_custom_key_route_revision(target_key_id);
            END LOOP;
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER trg_version_custom_key_route_user_access
AFTER UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION version_custom_key_route_user_access();
CREATE TRIGGER trg_version_custom_key_route_user_groups
AFTER INSERT OR UPDATE OR DELETE ON user_allowed_groups
FOR EACH ROW EXECUTE FUNCTION version_custom_key_route_user_access();
