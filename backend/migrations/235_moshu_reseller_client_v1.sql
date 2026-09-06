-- L1 client state for Moshu reseller protocol v1.
-- Additive only: MOSHU_RESELLER_CLIENT_ENABLED=false preserves the existing
-- manually configured per-group Moshu keys.

CREATE TABLE IF NOT EXISTS moshu_reseller_connections (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    base_url VARCHAR(500) NOT NULL,
    instance_id UUID NOT NULL,
    reseller_id BIGINT,
    reseller_name VARCHAR(120),
    protocol_version VARCHAR(16),
    status VARCHAR(20) NOT NULL DEFAULT 'disconnected'
        CHECK (status IN ('disconnected', 'active', 'error', 'suspended')),
    access_token_ciphertext TEXT,
    refresh_token_ciphertext TEXT,
    access_token_expires_at TIMESTAMPTZ,
    catalog_etag VARCHAR(200),
    catalog_version BIGINT NOT NULL DEFAULT 0,
    last_catalog_sync_at TIMESTAMPTZ,
    last_settlement_sync_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS moshu_products (
    id BIGSERIAL PRIMARY KEY,
    remote_product_id BIGINT NOT NULL UNIQUE,
    product_code VARCHAR(80) NOT NULL UNIQUE,
    display_name VARCHAR(120) NOT NULL,
    platform VARCHAR(50) NOT NULL,
    moshu_group_id BIGINT NOT NULL,
    authorized BOOLEAN NOT NULL DEFAULT TRUE,
    selected BOOLEAN NOT NULL DEFAULT FALSE,
    cost_rate_multiplier DECIMAL(10,4) NOT NULL CHECK (cost_rate_multiplier >= 0),
    sales_rate_multiplier DECIMAL(10,4),
    price_catalog_version BIGINT NOT NULL,
    model_snapshot JSONB NOT NULL DEFAULT '[]'::jsonb,
    capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
    credential_ciphertext TEXT,
    credential_received_at TIMESTAMPTZ,
    local_group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    local_account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS moshu_products_sale_idx
    ON moshu_products (authorized, selected, platform, product_code);

CREATE TABLE IF NOT EXISTS moshu_catalog_sync_runs (
    id BIGSERIAL PRIMARY KEY,
    status VARCHAR(20) NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    from_version BIGINT NOT NULL DEFAULT 0,
    to_version BIGINT NOT NULL DEFAULT 0,
    products_seen INTEGER NOT NULL DEFAULT 0,
    products_revoked INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS moshu_settlement_cursors (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_remote_id BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO moshu_settlement_cursors (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS moshu_request_profit_records (
    id BIGSERIAL PRIMARY KEY,
    remote_settlement_id BIGINT NOT NULL UNIQUE,
    request_id UUID NOT NULL UNIQUE,
    remote_product_id BIGINT NOT NULL,
    product_code VARCHAR(80) NOT NULL,
    local_group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    local_usage_log_id BIGINT REFERENCES usage_logs(id) ON DELETE SET NULL,
    requested_model VARCHAR(200),
    upstream_model VARCHAR(200),
    service_tier VARCHAR(50),
    standard_cost DECIMAL(20,10) NOT NULL DEFAULT 0,
    moshu_cost_rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 0,
    moshu_actual_cost DECIMAL(20,10) NOT NULL DEFAULT 0,
    l1_sales_rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 0,
    l1_customer_charge DECIMAL(20,10) NOT NULL DEFAULT 0,
    gross_profit DECIMAL(20,10) NOT NULL DEFAULT 0,
    price_catalog_version BIGINT NOT NULL,
    settlement_status VARCHAR(20) NOT NULL,
    remote_completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS moshu_profit_product_created_idx
    ON moshu_request_profit_records (product_code, created_at DESC);
