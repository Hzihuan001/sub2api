-- Reseller protocol v2 is additive. Legacy pricing and settlement cursors stay
-- readable by older proxy stations during staged rollout and rollback.
ALTER TABLE reseller_request_reservations
    ADD COLUMN IF NOT EXISTS request_source VARCHAR(32) NOT NULL DEFAULT 'user';

ALTER TABLE reseller_request_settlements
    ADD COLUMN IF NOT EXISTS revision BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS request_source VARCHAR(32) NOT NULL DEFAULT 'user';

CREATE TABLE IF NOT EXISTS reseller_settlement_events (
    id BIGSERIAL PRIMARY KEY,
    reseller_id BIGINT NOT NULL REFERENCES reseller_tenants(id) ON DELETE RESTRICT,
    settlement_id BIGINT NOT NULL REFERENCES reseller_request_settlements(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at TIMESTAMPTZ,
    UNIQUE (settlement_id, revision)
);

CREATE INDEX IF NOT EXISTS reseller_settlement_events_pending_idx
    ON reseller_settlement_events (reseller_id, acknowledged_at, id);

CREATE OR REPLACE FUNCTION version_reseller_settlement()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW.revision := OLD.revision + 1;
    END IF;
    SELECT COALESCE(rr.request_source, 'user')
      INTO NEW.request_source
      FROM reseller_request_reservations rr
     WHERE rr.reseller_id = NEW.reseller_id AND rr.request_id = NEW.request_id;
    NEW.request_source := COALESCE(NULLIF(NEW.request_source, ''), 'user');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS reseller_settlement_version_before_write ON reseller_request_settlements;
CREATE TRIGGER reseller_settlement_version_before_write
BEFORE INSERT OR UPDATE ON reseller_request_settlements
FOR EACH ROW EXECUTE FUNCTION version_reseller_settlement();

CREATE OR REPLACE FUNCTION enqueue_reseller_settlement_event()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO reseller_settlement_events (reseller_id, settlement_id, revision)
    VALUES (NEW.reseller_id, NEW.id, NEW.revision)
    ON CONFLICT (settlement_id, revision) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS reseller_settlement_event_after_write ON reseller_request_settlements;
CREATE TRIGGER reseller_settlement_event_after_write
AFTER INSERT OR UPDATE ON reseller_request_settlements
FOR EACH ROW EXECUTE FUNCTION enqueue_reseller_settlement_event();
