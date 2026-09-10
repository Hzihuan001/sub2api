-- Internal price-only cache. No customer balances or historical usage are rewritten.
CREATE TABLE IF NOT EXISTS moshu_pricing_snapshots (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    base_url TEXT NOT NULL,
    reseller_id BIGINT NOT NULL,
    instance_id UUID NOT NULL,
    revision VARCHAR(64) NOT NULL,
    payload JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS moshu_pricing_history (
    revision VARCHAR(64) PRIMARY KEY,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS moshu_request_pricing (
    request_id TEXT NOT NULL,
    api_key_id BIGINT NOT NULL,
    revision VARCHAR(64) NOT NULL,
    base_cost NUMERIC(20,8) NOT NULL,
    customer_charge NUMERIC(20,8) NOT NULL,
    upstream_cost_rate NUMERIC(20,8) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(request_id, api_key_id)
);
