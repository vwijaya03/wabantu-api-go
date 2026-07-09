-- SECURITY DEFINER helper so runtime roles (encore_services) can DROP SCHEMA t_*
-- without SET ROLE db_tenant_admin membership.
CREATE OR REPLACE FUNCTION public.drop_tenant_schema(p_schema_name text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
  IF p_schema_name IS NULL OR p_schema_name !~ '^t_[a-z0-9_]{0,62}$' THEN
    RAISE EXCEPTION 'invalid tenant schema name: %', p_schema_name;
  END IF;
  EXECUTE format('DROP SCHEMA IF EXISTS %I CASCADE', p_schema_name);
END;
$$;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'db_tenant_admin') THEN
    EXECUTE 'ALTER FUNCTION public.drop_tenant_schema(text) OWNER TO db_tenant_admin';
  END IF;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'drop_tenant_schema owner transfer skipped: %', SQLERRM;
END $$;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'encore_services') THEN
    GRANT EXECUTE ON FUNCTION public.drop_tenant_schema(text) TO encore_services;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'encore_writer') THEN
    GRANT EXECUTE ON FUNCTION public.drop_tenant_schema(text) TO encore_writer;
  END IF;
END $$;
