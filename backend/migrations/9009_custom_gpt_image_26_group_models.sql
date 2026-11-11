-- 9009_custom_gpt_image_26_group_models: expose the managed GPT Image 2.6
-- models in existing OpenAI image groups without replacing administrator
-- allowlists.  The guarded appends are idempotent so a restart or retry cannot
-- duplicate either model.

UPDATE groups
   SET model_allowlist = jsonb_set(
       COALESCE(model_allowlist, '{}'::jsonb),
       '{models}',
       COALESCE(model_allowlist -> 'models', '[]'::jsonb) || '["gpt-image-2.6-flare"]'::jsonb
   )
 WHERE platform = 'openai'
   AND allow_image_generation = TRUE
   AND NOT (COALESCE(model_allowlist -> 'models', '[]'::jsonb) ? 'gpt-image-2.6-flare');

UPDATE groups
   SET model_allowlist = jsonb_set(
       COALESCE(model_allowlist, '{}'::jsonb),
       '{models}',
       COALESCE(model_allowlist -> 'models', '[]'::jsonb) || '["gpt-image-2.6-sunburst"]'::jsonb
   )
 WHERE platform = 'openai'
   AND allow_image_generation = TRUE
   AND NOT (COALESCE(model_allowlist -> 'models', '[]'::jsonb) ? 'gpt-image-2.6-sunburst');
