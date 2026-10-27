-- Apply as a database administrator in the main site's database.
-- Additive infrastructure only: no business table/column/migration changes.
BEGIN;
DO $roles$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'moshu_direct_access_owner') THEN
    CREATE ROLE moshu_direct_access_owner NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'moshu_direct_access_auth') THEN
    CREATE ROLE moshu_direct_access_auth LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS CONNECTION LIMIT 8;
  END IF;
END
$roles$;

GRANT USAGE ON SCHEMA public TO moshu_direct_access_owner, moshu_direct_access_auth;
GRANT SELECT (key, user_id, status, deleted_at, expires_at) ON public.api_keys TO moshu_direct_access_owner;
GRANT SELECT (id, status, deleted_at) ON public.users TO moshu_direct_access_owner;
GRANT SELECT (id, key, type, enabled, deleted_at) ON public.user_attribute_definitions TO moshu_direct_access_owner;
GRANT SELECT (user_id, attribute_id, value) ON public.user_attribute_values TO moshu_direct_access_owner;

CREATE OR REPLACE FUNCTION public.moshu_direct_access_allowed(candidate_key text)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $function$
DECLARE
  grant_value text;
BEGIN
  SELECT av.value INTO grant_value
  FROM public.api_keys AS k
  JOIN public.users AS u ON u.id = k.user_id
  JOIN public.user_attribute_values AS av ON av.user_id = u.id
  JOIN public.user_attribute_definitions AS ad ON ad.id = av.attribute_id
  WHERE k.key = candidate_key
    AND k.status = 'active' AND k.deleted_at IS NULL
    AND (k.expires_at IS NULL OR k.expires_at > pg_catalog.now())
    AND u.status = 'active' AND u.deleted_at IS NULL
    AND ad.key = 'moshu_direct_access' AND ad.type = 'multi_select'
    AND ad.enabled AND ad.deleted_at IS NULL;
  IF grant_value IS NULL THEN
    RETURN false;
  END IF;
  BEGIN
    -- Semantic JSON equality allows formatting whitespace, but not extra
    -- options, duplicate options, boolean/string coercions or malformed JSON.
    RETURN grant_value::pg_catalog.jsonb = '["enabled"]'::pg_catalog.jsonb;
  EXCEPTION WHEN invalid_text_representation THEN
    RETURN false;
  END;
END
$function$;
ALTER FUNCTION public.moshu_direct_access_allowed(text) OWNER TO moshu_direct_access_owner;
REVOKE ALL ON FUNCTION public.moshu_direct_access_allowed(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.moshu_direct_access_allowed(text) TO moshu_direct_access_auth;
COMMENT ON FUNCTION public.moshu_direct_access_allowed(text) IS
  'nocf ingress permission only; native gateway still enforces all key, IP, balance, quota, model and group checks';
COMMIT;
