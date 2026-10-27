-- Run in the main site's database after authorization.sql. New users remain
-- denied. Grant/revoke individuals in the existing admin user editor.
BEGIN;
DO $attribute$
BEGIN
  IF EXISTS (
    SELECT 1 FROM public.user_attribute_definitions
    WHERE key = 'moshu_direct_access' AND deleted_at IS NULL
      AND (type <> 'multi_select'
           OR options <> '[{"value":"enabled","label":"允许直连接入"}]'::jsonb)
  ) THEN
    RAISE EXCEPTION 'reserved direct-access attribute conflicts with existing definition';
  END IF;
END
$attribute$;
INSERT INTO public.user_attribute_definitions
  (key, name, description, type, options, required, display_order, enabled)
SELECT 'moshu_direct_access', '直连 API 权限',
  '仅管理员可配置。允许使用 https://nocf.moshu.cloud/v1，原 API Key、分组、额度及计费保持不变。取消勾选后新请求立即失效。',
  'multi_select', '[{"value":"enabled","label":"允许直连接入"}]'::jsonb,
  false, COALESCE((SELECT max(display_order) + 1 FROM public.user_attribute_definitions), 0), true
WHERE NOT EXISTS (
  SELECT 1 FROM public.user_attribute_definitions
  WHERE key = 'moshu_direct_access' AND deleted_at IS NULL
);
COMMIT;
