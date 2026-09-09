-- Local cost estimates may differ from the public upstream group rate.
-- NULL follows the catalog. Authoritative upstream settlements are unchanged.
ALTER TABLE moshu_products
    ADD COLUMN IF NOT EXISTS cost_rate_override DECIMAL(10,4)
    CHECK (cost_rate_override >= 0 AND cost_rate_override < 1000000);
