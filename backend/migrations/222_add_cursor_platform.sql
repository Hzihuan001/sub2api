-- Migration: 222_add_cursor_platform
-- Add the new 'cursor' upstream platform to every platform/provider CHECK
-- constraint, mirroring how 'grok' was introduced (157/172/176). Additive
-- only: each new constraint is a superset of the previous one, so existing
-- rows keep validating.

-- user_platform_quotas.platform (see 157_user_platform_quotas_add_grok.sql)
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'kiro',
                        'grok', 'kimi', 'zhipu', 'deepseek', 'cursor'));

-- composite_model_routes.target_platform (see 172_composite_model_routes.sql)
ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'kiro',
                               'grok', 'kimi', 'zhipu', 'deepseek', 'cursor'));

-- channel monitor providers (see 176_channel_monitor_grok_provider.sql).
-- Cursor checks reuse the OpenAI-compatible chat completions protocol.
DO $$
DECLARE
    monitor_constraint_def TEXT;
    template_constraint_def TEXT;
BEGIN
    SELECT pg_get_constraintdef(c.oid)
      INTO monitor_constraint_def
      FROM pg_constraint c
      JOIN pg_class t ON t.oid = c.conrelid
     WHERE t.relname = 'channel_monitors'
       AND c.conname = 'channel_monitors_provider_check';

    IF monitor_constraint_def IS NULL OR position('cursor' IN monitor_constraint_def) = 0 THEN
        ALTER TABLE channel_monitors
            DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_provider_check
            CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                                'antigravity', 'kiro', 'kimi', 'zhipu', 'deepseek', 'cursor'));
    END IF;

    SELECT pg_get_constraintdef(c.oid)
      INTO template_constraint_def
      FROM pg_constraint c
      JOIN pg_class t ON t.oid = c.conrelid
     WHERE t.relname = 'channel_monitor_request_templates'
       AND c.conname = 'channel_monitor_request_templates_provider_check';

    IF template_constraint_def IS NULL OR position('cursor' IN template_constraint_def) = 0 THEN
        ALTER TABLE channel_monitor_request_templates
            DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
        ALTER TABLE channel_monitor_request_templates
            ADD CONSTRAINT channel_monitor_request_templates_provider_check
            CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                                'antigravity', 'kiro', 'kimi', 'zhipu', 'deepseek', 'cursor'));
    END IF;
END $$;
