-- Synthetic data for the isolated direct-access acceptance project only.
-- These are deliberately not production credentials or a production migration.
CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    deleted_at TIMESTAMPTZ
);
CREATE TABLE api_keys (
    id BIGINT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    key VARCHAR(128) NOT NULL UNIQUE,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ
);
CREATE TABLE user_attribute_definitions (
    id BIGINT PRIMARY KEY,
    key VARCHAR(100) NOT NULL,
    type VARCHAR(20) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    deleted_at TIMESTAMPTZ
);
CREATE TABLE user_attribute_values (
    id BIGINT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    attribute_id BIGINT NOT NULL REFERENCES user_attribute_definitions(id),
    value TEXT,
    UNIQUE(user_id, attribute_id)
);
INSERT INTO users(id) VALUES (1), (2);
INSERT INTO api_keys(id, user_id, key) VALUES
    (1, 1, 'sk-direct-allowed-acceptance-only'),
    (2, 2, 'sk-direct-ordinary-acceptance-only');
INSERT INTO user_attribute_definitions(id, key, type)
VALUES (1, 'moshu_direct_access', 'multi_select');
INSERT INTO user_attribute_values(id, user_id, attribute_id, value)
VALUES (1, 1, 1, '["enabled"]');
-- Production authorization.sql follows this fixture and owns the restricted
-- SECURITY DEFINER function/role grants. The local-only generated final init
-- script sets a synthetic password for that same EXECUTE-only role.
