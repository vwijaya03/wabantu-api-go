-- SECURITY DEFINER helper: reassign t_* schema + object owners to db_tenant_admin and
-- apply Encore Cloud runtime GRANTs. Callable from POST /api/v1/admin/migrate-tenant-schemas
-- so deploy dynamic grants stop failing with permission denied for table business_profile.
--
-- Definer takes schema ownership first (same pattern as drop_tenant_schema v2) so objects
-- owned by encore_container_* can be reassigned without a superuser shell session.
CREATE OR REPLACE FUNCTION public.repair_tenant_schema_grants(p_schema_name text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  r record;
  target text := 'db_tenant_admin';
  sch text;
  schema_owner name;
BEGIN
  IF p_schema_name IS NULL OR p_schema_name !~ '^t_[a-z0-9_]{0,62}$' THEN
    RAISE EXCEPTION 'invalid tenant schema name: %', p_schema_name;
  END IF;
  sch := p_schema_name;

  IF NOT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = sch) THEN
    RETURN;
  END IF;

  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = target) THEN
    RAISE EXCEPTION 'role % not found', target;
  END IF;

  SELECT pg_get_userbyid(nspowner) INTO schema_owner
  FROM pg_namespace WHERE nspname = sch;

  IF schema_owner IS DISTINCT FROM current_user THEN
    EXECUTE format('ALTER SCHEMA %I OWNER TO %I', sch, current_user);
  END IF;

  EXECUTE format('ALTER SCHEMA %I OWNER TO %I', sch, target);

  FOR r IN
    SELECT c.relname,
           CASE
             WHEN c.relkind = 'S' THEN 'SEQUENCE'
             WHEN c.relkind IN ('v', 'm') THEN 'VIEW'
             ELSE 'TABLE'
           END AS kind
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = sch
      AND c.relkind IN ('r', 'p', 'S', 'v', 'm')
      AND pg_get_userbyid(c.relowner) IS DISTINCT FROM target
  LOOP
    BEGIN
      EXECUTE format('ALTER %s %I.%I OWNER TO %I', r.kind, sch, r.relname, target);
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'repair_tenant_schema_grants skip %.%: %', sch, r.relname, SQLERRM;
    END;
  END LOOP;

  EXECUTE format('GRANT USAGE, CREATE ON SCHEMA %I TO %I', sch, target);
  EXECUTE format('GRANT USAGE, CREATE ON SCHEMA %I TO encore_writer, encore_reader, encore_services', sch);
  EXECUTE format(
    'GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO encore_writer, encore_services', sch);
  EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA %I TO encore_reader', sch);
  EXECUTE format(
    'GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %I TO encore_writer, encore_services', sch);
  EXECUTE format('GRANT SELECT ON ALL SEQUENCES IN SCHEMA %I TO encore_reader', sch);
  EXECUTE format(
    'ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO encore_writer, encore_services',
    sch);
  EXECUTE format(
    'ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT SELECT ON TABLES TO encore_reader', sch);
END;
$$;

-- Jangan pindahkan owner ke db_tenant_admin: definer harus tetap role migrator Encore
-- (privileged) agar ALTER OWNER dari encore_container_* berhasil (sama seperti drop_tenant_schema v2).

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'encore_services') THEN
    GRANT EXECUTE ON FUNCTION public.repair_tenant_schema_grants(text) TO encore_services;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'encore_writer') THEN
    GRANT EXECUTE ON FUNCTION public.repair_tenant_schema_grants(text) TO encore_writer;
  END IF;
END $$;
