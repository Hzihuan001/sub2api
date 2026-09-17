-- Additive protocol-v2 state. Existing schema-1 JSONB and cursor data remain
-- readable by rollback images.
ALTER TABLE moshu_pricing_snapshots
    ADD COLUMN IF NOT EXISTS schema_version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS digest_algorithm VARCHAR(32),
    ADD COLUMN IF NOT EXISTS raw_payload BYTEA,
    ADD COLUMN IF NOT EXISTS billing_semantics_version INTEGER NOT NULL DEFAULT 1;

ALTER TABLE moshu_pricing_history
    ADD COLUMN IF NOT EXISTS schema_version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS digest_algorithm VARCHAR(32),
    ADD COLUMN IF NOT EXISTS raw_payload BYTEA,
    ADD COLUMN IF NOT EXISTS billing_semantics_version INTEGER NOT NULL DEFAULT 1;

ALTER TABLE moshu_reseller_connections
    ADD COLUMN IF NOT EXISTS auth_last_success_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS auth_last_error_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS auth_last_error TEXT,
    ADD COLUMN IF NOT EXISTS catalog_last_success_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS catalog_last_error_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS catalog_last_error TEXT,
    ADD COLUMN IF NOT EXISTS pricing_last_success_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS pricing_last_error_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS pricing_last_error TEXT,
    ADD COLUMN IF NOT EXISTS settlement_last_success_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS settlement_last_error_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS settlement_last_error TEXT;

UPDATE moshu_reseller_connections SET
    auth_last_success_at=COALESCE(auth_last_success_at,last_catalog_sync_at),
    catalog_last_success_at=COALESCE(catalog_last_success_at,last_catalog_sync_at),
    settlement_last_success_at=COALESCE(settlement_last_success_at,last_settlement_sync_at)
WHERE id=1;

ALTER TABLE moshu_request_profit_records
    ADD COLUMN IF NOT EXISTS remote_revision BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS estimated_cost DECIMAL(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cost_difference DECIMAL(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS request_source VARCHAR(32) NOT NULL DEFAULT 'user',
    ADD COLUMN IF NOT EXISTS settlement_confirmed BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS moshu_profit_confirmation_idx
    ON moshu_request_profit_records (settlement_confirmed, updated_at DESC);
