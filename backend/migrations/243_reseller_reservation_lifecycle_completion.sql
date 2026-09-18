-- Complete the additive reservation lifecycle introduced in migration 242.
-- Kept separate so the checksum of the already released 242 migration never
-- changes during a staged upgrade.
CREATE OR REPLACE FUNCTION mark_reseller_reservation_completed()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE reseller_request_reservations
       SET status = 'completed'
     WHERE reseller_id = NEW.reseller_id
       AND request_id = NEW.request_id
       AND status = 'pending';
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS reseller_reservation_completed_after_settlement
    ON reseller_request_settlements;
CREATE TRIGGER reseller_reservation_completed_after_settlement
AFTER INSERT OR UPDATE OF status ON reseller_request_settlements
FOR EACH ROW
WHEN (NEW.status = 'completed')
EXECUTE FUNCTION mark_reseller_reservation_completed();
