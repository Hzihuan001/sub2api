-- Preserve the exact settlement payload delivered by each event revision.
-- Existing events are backfilled from their current settlement row; new and
-- updated revisions are written by the trigger below before they are exposed
-- to reseller stations.

ALTER TABLE reseller_settlement_events
    ADD COLUMN IF NOT EXISTS settlement_snapshot JSONB;

CREATE OR REPLACE FUNCTION build_reseller_settlement_snapshot(
    settlement_row reseller_request_settlements,
    product_code_value VARCHAR,
    display_name_value VARCHAR
)
RETURNS JSONB AS $$
BEGIN
    RETURN jsonb_build_object(
        'id', settlement_row.id,
        'revision', settlement_row.revision,
        'request_source', settlement_row.request_source,
        'request_id', settlement_row.request_id,
        'upstream_request_id', settlement_row.upstream_request_id,
        'product_id', settlement_row.product_id,
        'product_code', product_code_value,
        'display_name', display_name_value,
        'moshu_group_id', settlement_row.moshu_group_id,
        'requested_model', settlement_row.requested_model,
        'upstream_model', settlement_row.upstream_model,
        'service_tier', settlement_row.service_tier,
        'input_tokens', settlement_row.input_tokens,
        'output_tokens', settlement_row.output_tokens,
        'cache_creation_tokens', settlement_row.cache_creation_tokens,
        'cache_read_tokens', settlement_row.cache_read_tokens,
        'image_count', settlement_row.image_count,
        'video_count', settlement_row.video_count,
        'standard_cost', settlement_row.standard_cost,
        'cost_rate_multiplier', settlement_row.cost_rate_multiplier,
        'actual_cost', settlement_row.actual_cost,
        'price_catalog_version', settlement_row.price_catalog_version,
        'status', settlement_row.status,
        'error_type', settlement_row.error_type,
        'started_at', settlement_row.started_at,
        'completed_at', settlement_row.completed_at,
        'created_at', settlement_row.created_at
    );
END;
$$ LANGUAGE plpgsql IMMUTABLE;

UPDATE reseller_settlement_events e
   SET settlement_snapshot = build_reseller_settlement_snapshot(rs, rp.product_code, rp.display_name)
  FROM reseller_request_settlements rs
  JOIN reseller_products rp ON rp.id = rs.product_id
 WHERE e.settlement_id = rs.id
   AND e.settlement_snapshot IS NULL;

ALTER TABLE reseller_settlement_events
    ALTER COLUMN settlement_snapshot SET NOT NULL;

CREATE OR REPLACE FUNCTION enqueue_reseller_settlement_event()
RETURNS TRIGGER AS $$
DECLARE
    product_code_value VARCHAR;
    display_name_value VARCHAR;
BEGIN
    SELECT rp.product_code, rp.display_name
      INTO product_code_value, display_name_value
      FROM reseller_products rp
     WHERE rp.id = NEW.product_id;

    INSERT INTO reseller_settlement_events (
        reseller_id, settlement_id, revision, settlement_snapshot
    ) VALUES (
        NEW.reseller_id, NEW.id, NEW.revision,
        build_reseller_settlement_snapshot(NEW, product_code_value, display_name_value)
    )
    ON CONFLICT (settlement_id, revision) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS reseller_settlement_event_after_write
    ON reseller_request_settlements;
CREATE TRIGGER reseller_settlement_event_after_write
AFTER INSERT OR UPDATE ON reseller_request_settlements
FOR EACH ROW EXECUTE FUNCTION enqueue_reseller_settlement_event();
