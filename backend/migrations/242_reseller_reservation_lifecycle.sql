-- Reseller request reservations are short-lived admission records.  They are
-- deliberately additive so older binaries can still read the original rows.
ALTER TABLE reseller_request_reservations
    ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '2 hours');

ALTER TABLE reseller_request_reservations
    DROP CONSTRAINT IF EXISTS reseller_request_reservations_status_check;
ALTER TABLE reseller_request_reservations
    ADD CONSTRAINT reseller_request_reservations_status_check
    CHECK (status IN ('pending', 'completed', 'failed', 'expired'));

UPDATE reseller_request_reservations
   SET expires_at = created_at + INTERVAL '2 hours'
 WHERE expires_at IS NULL OR expires_at > created_at + INTERVAL '2 hours';

CREATE INDEX IF NOT EXISTS reseller_request_reservations_expiry_idx
    ON reseller_request_reservations (status, expires_at)
    WHERE status = 'pending';
