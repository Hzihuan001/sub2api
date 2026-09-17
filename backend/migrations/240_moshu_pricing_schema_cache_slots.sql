-- Preserve independent schema-1 and schema-2 price snapshots. Slot 1 remains
-- untouched for rollback binaries; protocol-v2 clients write slot 2.
ALTER TABLE moshu_pricing_snapshots
    DROP CONSTRAINT IF EXISTS moshu_pricing_snapshots_id_check;

ALTER TABLE moshu_pricing_snapshots
    ADD CONSTRAINT moshu_pricing_snapshots_id_check CHECK (id IN (1, 2));
