-- The published 238_restore_custom_platform_constraints.sql can remove OpenCode
-- from fresh installations. Repair only constraints missing one of these platforms.
DO $$
DECLARE
    constraint_def TEXT;
BEGIN
    SELECT pg_get_constraintdef(oid) INTO constraint_def
      FROM pg_constraint
     WHERE conrelid = 'user_platform_quotas'::regclass
       AND conname = 'user_platform_quotas_platform_check';
    IF constraint_def IS NULL
       OR position('opencode_go' IN constraint_def) = 0
       OR position('kiro' IN constraint_def) = 0
       OR position('cursor' IN constraint_def) = 0 THEN
        ALTER TABLE user_platform_quotas
            DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
        ALTER TABLE user_platform_quotas
            ADD CONSTRAINT user_platform_quotas_platform_check
            CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                                'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'kiro', 'cursor'));
    END IF;

    SELECT pg_get_constraintdef(oid) INTO constraint_def
      FROM pg_constraint
     WHERE conrelid = 'composite_model_routes'::regclass
       AND conname = 'composite_model_routes_target_platform_check';
    IF constraint_def IS NULL
       OR position('opencode_go' IN constraint_def) = 0
       OR position('kiro' IN constraint_def) = 0
       OR position('cursor' IN constraint_def) = 0 THEN
        ALTER TABLE composite_model_routes
            DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
        ALTER TABLE composite_model_routes
            ADD CONSTRAINT composite_model_routes_target_platform_check
            CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                                       'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'kiro', 'cursor'));
    END IF;

    SELECT pg_get_constraintdef(oid) INTO constraint_def
      FROM pg_constraint
     WHERE conrelid = 'channel_monitors'::regclass
       AND conname = 'channel_monitors_provider_check';
    IF constraint_def IS NULL
       OR position('opencode_go' IN constraint_def) = 0
       OR position('kiro' IN constraint_def) = 0
       OR position('cursor' IN constraint_def) = 0 THEN
        ALTER TABLE channel_monitors
            DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_provider_check
            CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                                'antigravity', 'kimi', 'zhipu', 'deepseek', 'minimax',
                                'opencode_go', 'kiro', 'cursor'));
    END IF;

    SELECT pg_get_constraintdef(oid) INTO constraint_def
      FROM pg_constraint
     WHERE conrelid = 'channel_monitor_request_templates'::regclass
       AND conname = 'channel_monitor_request_templates_provider_check';
    IF constraint_def IS NULL
       OR position('opencode_go' IN constraint_def) = 0
       OR position('kiro' IN constraint_def) = 0
       OR position('cursor' IN constraint_def) = 0 THEN
        ALTER TABLE channel_monitor_request_templates
            DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
        ALTER TABLE channel_monitor_request_templates
            ADD CONSTRAINT channel_monitor_request_templates_provider_check
            CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                                'antigravity', 'kimi', 'zhipu', 'deepseek', 'minimax',
                                'opencode_go', 'kiro', 'cursor'));
    END IF;
END $$;
