-- v2: function owner must be superuser (created via fix-cloud-db-grants --superuser).
-- Reassigns schema owner to definer, then DROP — works when t_* owned by encore_container/writer.
CREATE OR REPLACE FUNCTION public.drop_tenant_schema(p_schema_name text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  schema_owner name;
BEGIN
  IF p_schema_name IS NULL OR p_schema_name !~ '^t_[a-z0-9_]{0,62}$' THEN
    RAISE EXCEPTION 'invalid tenant schema name: %', p_schema_name;
  END IF;

  SELECT pg_get_userbyid(nspowner) INTO schema_owner
  FROM pg_namespace
  WHERE nspname = p_schema_name;

  IF schema_owner IS NULL THEN
    RETURN;
  END IF;

  IF schema_owner IS DISTINCT FROM current_user THEN
    EXECUTE format('ALTER SCHEMA %I OWNER TO %I', p_schema_name, current_user);
  END IF;

  EXECUTE format('DROP SCHEMA IF EXISTS %I CASCADE', p_schema_name);
END;
$$;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'encore_services') THEN
    GRANT EXECUTE ON FUNCTION public.drop_tenant_schema(text) TO encore_services;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'encore_writer') THEN
    GRANT EXECUTE ON FUNCTION public.drop_tenant_schema(text) TO encore_writer;
  END IF;
END $$;
