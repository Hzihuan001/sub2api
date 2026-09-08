-- Deleted tenants retain their settlement ownership and former instance binding.
ALTER TABLE reseller_tenants ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
DROP INDEX IF EXISTS reseller_tenants_name_active_uidx;
CREATE UNIQUE INDEX IF NOT EXISTS reseller_tenants_name_active_uidx
    ON reseller_tenants (LOWER(name)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS reseller_tenants_user_active_uidx
    ON reseller_tenants (user_id) WHERE deleted_at IS NULL;
