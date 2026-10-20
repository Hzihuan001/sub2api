-- A database-backed namespace is stable across application restarts and is
-- not derived from a display name, a container hostname, or credentials.
-- Old binaries ignore this additive table. A deliberately cloned site must
-- allocate a new site_id before sharing routing-health Redis with its source.
CREATE TABLE IF NOT EXISTS custom_key_route_site (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    site_id UUID NOT NULL DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO custom_key_route_site (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
