-- Multiple independent reseller stations may share one prepaid billing user.
-- Keep tenant names, instance bindings, and per-tenant credentials unique.
DROP INDEX IF EXISTS reseller_tenants_user_active_uidx;
CREATE INDEX IF NOT EXISTS reseller_tenants_user_idx ON reseller_tenants (user_id);
