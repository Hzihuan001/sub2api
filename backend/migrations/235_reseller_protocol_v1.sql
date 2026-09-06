-- Moshu reseller protocol v1.
-- Additive only: disabling MOSHU_RESELLER_SERVER_ENABLED leaves every legacy
-- gateway, user, group, account and usage path unchanged.

CREATE TABLE IF NOT EXISTS reseller_tenants (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name VARCHAR(120) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'suspended', 'disabled')),
    protocol_version VARCHAR(16) NOT NULL DEFAULT 'v1',
    instance_id UUID,
    allowed_cidrs JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS reseller_tenants_name_active_uidx
    ON reseller_tenants (LOWER(name));
CREATE UNIQUE INDEX IF NOT EXISTS reseller_tenants_instance_uidx
    ON reseller_tenants (instance_id) WHERE instance_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS reseller_tenants_user_idx ON reseller_tenants (user_id);

CREATE TABLE IF NOT EXISTS reseller_products (
    id BIGSERIAL PRIMARY KEY,
    reseller_id BIGINT NOT NULL REFERENCES reseller_tenants(id) ON DELETE CASCADE,
    moshu_group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    product_code VARCHAR(80) NOT NULL,
    display_name VARCHAR(120) NOT NULL,
    platform VARCHAR(50) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    cost_rate_multiplier DECIMAL(10,4) NOT NULL CHECK (cost_rate_multiplier >= 0),
    price_catalog_version BIGINT NOT NULL DEFAULT 1 CHECK (price_catalog_version > 0),
    model_snapshot JSONB NOT NULL DEFAULT '[]'::jsonb,
    capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
    effective_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (reseller_id, product_code),
    UNIQUE (reseller_id, moshu_group_id)
);

CREATE INDEX IF NOT EXISTS reseller_products_catalog_idx
    ON reseller_products (reseller_id, enabled, price_catalog_version, id);

CREATE TABLE IF NOT EXISTS reseller_enrollment_codes (
    id BIGSERIAL PRIMARY KEY,
    reseller_id BIGINT NOT NULL REFERENCES reseller_tenants(id) ON DELETE CASCADE,
    code_hash BYTEA NOT NULL UNIQUE,
    product_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    used_instance_id UUID,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS reseller_enrollment_codes_reseller_idx
    ON reseller_enrollment_codes (reseller_id, expires_at DESC);

CREATE TABLE IF NOT EXISTS reseller_credentials (
    id BIGSERIAL PRIMARY KEY,
    reseller_id BIGINT NOT NULL REFERENCES reseller_tenants(id) ON DELETE CASCADE,
    product_id BIGINT NOT NULL REFERENCES reseller_products(id) ON DELETE CASCADE,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    status VARCHAR(20) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'retiring', 'revoked', 'expired')),
    expires_at TIMESTAMPTZ,
    overlap_until TIMESTAMPTZ,
    rotated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (api_key_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS reseller_credentials_one_active_product_uidx
    ON reseller_credentials (product_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS reseller_credentials_lookup_idx
    ON reseller_credentials (api_key_id, status, expires_at);

CREATE TABLE IF NOT EXISTS reseller_refresh_tokens (
    id UUID PRIMARY KEY,
    reseller_id BIGINT NOT NULL REFERENCES reseller_tenants(id) ON DELETE CASCADE,
    instance_id UUID NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS reseller_refresh_tokens_tenant_idx
    ON reseller_refresh_tokens (reseller_id, instance_id, expires_at DESC);

-- Reserve request IDs before any upstream work. This table is deliberately
-- separate from settlements: settlement IDs are allocated only when a request
-- reaches a terminal state, so an incremental consumer can never skip an older
-- pending row that completes after its cursor has advanced.
CREATE TABLE IF NOT EXISTS reseller_request_reservations (
    reseller_id BIGINT NOT NULL REFERENCES reseller_tenants(id) ON DELETE RESTRICT,
    request_id UUID NOT NULL,
    product_id BIGINT NOT NULL REFERENCES reseller_products(id) ON DELETE RESTRICT,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    moshu_group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    price_catalog_version BIGINT NOT NULL CHECK (price_catalog_version > 0),
    cost_rate_multiplier DECIMAL(10,4) NOT NULL CHECK (cost_rate_multiplier >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (reseller_id, request_id)
);

CREATE INDEX IF NOT EXISTS reseller_request_reservations_key_idx
    ON reseller_request_reservations (api_key_id, request_id);

CREATE TABLE IF NOT EXISTS reseller_request_settlements (
    id BIGSERIAL PRIMARY KEY,
    request_id UUID NOT NULL,
    upstream_request_id VARCHAR(200),
    usage_log_id BIGINT REFERENCES usage_logs(id) ON DELETE SET NULL,
    reseller_id BIGINT NOT NULL REFERENCES reseller_tenants(id) ON DELETE RESTRICT,
    product_id BIGINT NOT NULL REFERENCES reseller_products(id) ON DELETE RESTRICT,
    moshu_group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    requested_model VARCHAR(200),
    upstream_model VARCHAR(200),
    service_tier VARCHAR(50),
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    image_count INTEGER NOT NULL DEFAULT 0,
    video_count INTEGER NOT NULL DEFAULT 0,
    standard_cost DECIMAL(20,10) NOT NULL DEFAULT 0,
    cost_rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 0,
    actual_cost DECIMAL(20,10) NOT NULL DEFAULT 0,
    price_catalog_version BIGINT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'completed', 'failed')),
    error_type VARCHAR(100),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (reseller_id, request_id)
);

CREATE INDEX IF NOT EXISTS reseller_settlements_cursor_idx
    ON reseller_request_settlements (reseller_id, id);
CREATE INDEX IF NOT EXISTS reseller_settlements_created_idx
    ON reseller_request_settlements (reseller_id, created_at DESC, id DESC);

-- A successful usage row is the authoritative source of reseller cost.  The
-- trigger cannot alter normal billing; it only mirrors completed usage for
-- API keys explicitly issued by the reseller protocol.
CREATE OR REPLACE FUNCTION capture_reseller_usage_settlement()
RETURNS TRIGGER AS $$
DECLARE
    reservation_row RECORD;
    parsed_request_id UUID;
    raw_request_id TEXT;
BEGIN
    raw_request_id := NEW.request_id;
    IF raw_request_id LIKE 'client:%' THEN
        raw_request_id := SUBSTRING(raw_request_id FROM 8);
    ELSIF raw_request_id LIKE 'local:%' THEN
        raw_request_id := SUBSTRING(raw_request_id FROM 7);
    END IF;
    BEGIN
        parsed_request_id := raw_request_id::UUID;
    EXCEPTION WHEN invalid_text_representation THEN
        RETURN NEW;
    END;

    SELECT rr.reseller_id, rr.product_id, rr.moshu_group_id,
           rr.price_catalog_version, rr.cost_rate_multiplier
      INTO reservation_row
      FROM reseller_request_reservations rr
     WHERE rr.request_id = parsed_request_id
       AND rr.api_key_id = NEW.api_key_id
     LIMIT 1;

    IF NOT FOUND THEN
        RETURN NEW;
    END IF;

    INSERT INTO reseller_request_settlements (
        request_id, upstream_request_id, usage_log_id, reseller_id, product_id,
        moshu_group_id, requested_model, upstream_model, service_tier,
        input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
        image_count, video_count, standard_cost, cost_rate_multiplier,
        actual_cost, price_catalog_version, status, completed_at, updated_at
    ) VALUES (
        parsed_request_id, NEW.upstream_request_id, NEW.id,
        reservation_row.reseller_id, reservation_row.product_id,
        reservation_row.moshu_group_id, COALESCE(NEW.requested_model, NEW.model),
        COALESCE(NEW.upstream_model, NEW.model), NEW.service_tier,
        NEW.input_tokens, NEW.output_tokens, NEW.cache_creation_tokens,
        NEW.cache_read_tokens, NEW.image_count, NEW.video_count,
        NEW.total_cost,
        reservation_row.cost_rate_multiplier,
        NEW.actual_cost, reservation_row.price_catalog_version,
        'completed', NEW.created_at, NOW()
    )
    ON CONFLICT (reseller_id, request_id) DO UPDATE SET
        upstream_request_id = EXCLUDED.upstream_request_id,
        usage_log_id = EXCLUDED.usage_log_id,
        requested_model = EXCLUDED.requested_model,
        upstream_model = EXCLUDED.upstream_model,
        service_tier = EXCLUDED.service_tier,
        input_tokens = EXCLUDED.input_tokens,
        output_tokens = EXCLUDED.output_tokens,
        cache_creation_tokens = EXCLUDED.cache_creation_tokens,
        cache_read_tokens = EXCLUDED.cache_read_tokens,
        image_count = EXCLUDED.image_count,
        video_count = EXCLUDED.video_count,
        standard_cost = EXCLUDED.standard_cost,
        cost_rate_multiplier = EXCLUDED.cost_rate_multiplier,
        actual_cost = EXCLUDED.actual_cost,
        price_catalog_version = EXCLUDED.price_catalog_version,
        status = 'completed', error_type = NULL,
        completed_at = EXCLUDED.completed_at, updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS usage_logs_capture_reseller_settlement ON usage_logs;
CREATE TRIGGER usage_logs_capture_reseller_settlement
AFTER INSERT ON usage_logs
FOR EACH ROW EXECUTE FUNCTION capture_reseller_usage_settlement();
