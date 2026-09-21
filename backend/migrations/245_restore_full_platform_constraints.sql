-- Repair platform CHECK constraints after the historical 237/238 custom merge.
--
-- Some reseller databases already applied an earlier 237 checksum and a
-- later 238 variant that did not retain every custom platform.  This migration
-- is additive and idempotent: it never changes account or billing data and
-- converges all four constraints to the complete platform set used by the
-- current runtime.

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                        'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'kiro', 'cursor'));

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                               'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'kiro', 'cursor'));

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity',
                        'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'kiro', 'cursor'));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;

ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity',
                        'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'kiro', 'cursor'));
