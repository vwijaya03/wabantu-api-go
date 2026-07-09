#!/usr/bin/env bash
# Fix Postgres ownership + GRANTs after migrate-local-db-to-encore.sh (Encore Cloud).
#
# Encore Cloud dynamic grants on tenant DB run as the admin role from
# `encore db conn-uri tenant --admin` (encore_admin_*), not db_tenant_admin.
# System DB migrations use the database owner role (db_system_admin / encore-migrator).
# pg_restore --no-owner leaves schemas/tables owned by wrong roles → deploy fails:
#   permission denied for schema t_*
#
# Usage: ./scripts/fix-cloud-db-grants.sh staging
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env-name> (e.g. staging)}"
cd "$ROOT"

SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
TENANT_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"

SYSTEM_OWNER="$(psql "$SYSTEM_URI" -tAc "
  SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = current_database()" | tr -d '[:space:]')"
TENANT_OWNER="$(psql "$TENANT_URI" -tAc "SELECT current_user" | tr -d '[:space:]')"

if [[ -z "$SYSTEM_OWNER" || -z "$TENANT_OWNER" ]]; then
  echo "ERROR: could not resolve database owner roles" >&2
  exit 1
fi

echo "=== Fix cloud DB grants ($ENV_NAME) ==="
echo "System DB owner (migrations): $SYSTEM_OWNER"
echo "Tenant admin role (t_* dynamic grants): $TENANT_OWNER"
echo

fix_database() {
  local label="$1"
  local uri="$2"
  local schema_regex="$3"
  local target_role="$4"

  echo "--- $label (owner → $target_role) ---"
  psql "$uri" -v ON_ERROR_STOP=1 <<SQL
DO \$\$
DECLARE
  r record;
  s text;
  target_role text := '${target_role}';
BEGIN
  FOR r IN
    SELECT DISTINCT pg_get_userbyid(c.relowner) AS owner
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname ~ '${schema_regex}'
      AND pg_get_userbyid(c.relowner) IS NOT NULL
      AND pg_get_userbyid(c.relowner) <> target_role
  LOOP
    BEGIN
      EXECUTE format('REASSIGN OWNED BY %I TO %I', r.owner, target_role);
      RAISE NOTICE 'reassigned objects from % to %', r.owner, target_role;
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'skip reassign from %: %', r.owner, SQLERRM;
    END;
  END LOOP;

  FOR s IN SELECT nspname FROM pg_namespace WHERE nspname ~ '${schema_regex}' ORDER BY 1 LOOP
    BEGIN
      EXECUTE format('ALTER SCHEMA %I OWNER TO %I', s, target_role);
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'skip schema owner %: %', s, SQLERRM;
    END;
    BEGIN
      EXECUTE format('GRANT ALL ON SCHEMA %I TO %I', s, target_role);
      EXECUTE format('GRANT USAGE, CREATE ON SCHEMA %I TO encore_writer, encore_reader, encore_services', s);
      EXECUTE format(
        'GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO encore_writer, encore_services', s);
      EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA %I TO encore_reader', s);
      EXECUTE format(
        'GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %I TO encore_writer, encore_services', s);
      EXECUTE format('GRANT SELECT ON ALL SEQUENCES IN SCHEMA %I TO encore_reader', s);
      EXECUTE format(
        'ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO encore_writer, encore_services',
        s);
      EXECUTE format(
        'ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT SELECT ON TABLES TO encore_reader', s);
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'skip schema grants %: %', s, SQLERRM;
    END;
  END LOOP;

  IF to_regclass('public.schema_migrations') IS NOT NULL THEN
    EXECUTE format('ALTER TABLE public.schema_migrations OWNER TO %I', target_role);
    GRANT SELECT, INSERT, UPDATE, DELETE ON public.schema_migrations TO encore_writer, encore_services;
    GRANT SELECT ON public.schema_migrations TO encore_reader;
  END IF;
END \$\$;
SQL
}

fix_database "system DB (public)" "$SYSTEM_URI" '^public\$' "$SYSTEM_OWNER"

echo "--- system DB explicit table owner → $SYSTEM_OWNER ---"
psql "$SYSTEM_URI" -v ON_ERROR_STOP=1 <<SQL
DO \$\$
DECLARE
  r record;
  target_role text := '${SYSTEM_OWNER}';
BEGIN
  FOR r IN
    SELECT tablename FROM pg_tables
    WHERE schemaname = 'public' AND tableowner IS DISTINCT FROM target_role
    ORDER BY tablename
  LOOP
    BEGIN
      EXECUTE format('ALTER TABLE public.%I OWNER TO %I', r.tablename, target_role);
      RAISE NOTICE 'alter owner public.% → %', r.tablename, target_role;
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'skip owner public.%: %', r.tablename, SQLERRM;
    END;
  END LOOP;
END \$\$;
SQL

registered_schemas="$(psql "$SYSTEM_URI" -tAc "
  SELECT tc.schema_name
  FROM tenant_company tc
  JOIN tenant t ON t.id = tc.tenant_id
  WHERE t.deleted_at IS NULL
    AND tc.schema_name IS NOT NULL AND tc.schema_name <> ''
  ORDER BY 1")"
if [[ -z "$registered_schemas" ]]; then
  echo "--- tenant DB: no registered schemas ---"
else
  echo "--- tenant DB (registered schemas → $TENANT_OWNER) ---"
  while IFS= read -r schema; do
    [[ -z "$schema" ]] && continue
    schema="$(echo "$schema" | tr -d '[:space:]')"
  echo "  -> $schema"
  psql "$TENANT_URI" -v ON_ERROR_STOP=1 -c "
    DO \$\$
    BEGIN
      BEGIN
        EXECUTE format('ALTER SCHEMA %I OWNER TO %I', '$schema', '$TENANT_OWNER');
      EXCEPTION WHEN OTHERS THEN
        RAISE NOTICE 'skip owner %: %', '$schema', SQLERRM;
      END;
      BEGIN
        EXECUTE format('GRANT USAGE, CREATE ON SCHEMA %I TO %I', '$schema', '$TENANT_OWNER');
        EXECUTE format('GRANT USAGE, CREATE ON SCHEMA %I TO encore_writer, encore_reader, encore_services', '$schema');
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO encore_writer, encore_services', '$schema');
        EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA %I TO encore_reader', '$schema');
        EXECUTE format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %I TO encore_writer, encore_services', '$schema');
        EXECUTE format('GRANT SELECT ON ALL SEQUENCES IN SCHEMA %I TO encore_reader', '$schema');
      EXCEPTION WHEN OTHERS THEN
        RAISE NOTICE 'skip grants %: %', '$schema', SQLERRM;
      END;
    END \$\$;"
  done <<< "$registered_schemas"
fi

echo
echo "Verify system public.schema_migrations (expect: $SYSTEM_OWNER):"
psql "$SYSTEM_URI" -c "
  SELECT tablename, tableowner
  FROM pg_tables WHERE schemaname = 'public' AND tablename = 'schema_migrations';"

echo
echo "Verify tenant schema owners (expect: $TENANT_OWNER):"
psql "$TENANT_URI" -c "
  SELECT nspname AS schema, pg_get_userbyid(nspowner) AS owner
  FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1;"

echo "--- tenant DB: runtime role membership for DROP SCHEMA via API ---"
psql "$TENANT_URI" -v ON_ERROR_STOP=1 <<SQL
DO \$\$
BEGIN
  BEGIN
    EXECUTE format('GRANT %I TO encore_services', '$TENANT_OWNER');
    RAISE NOTICE 'granted % to encore_services', '$TENANT_OWNER';
  EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'skip grant to encore_services: %', SQLERRM;
  END;
  BEGIN
    EXECUTE format('GRANT %I TO encore_writer', '$TENANT_OWNER');
    RAISE NOTICE 'granted % to encore_writer', '$TENANT_OWNER';
  EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'skip grant to encore_writer: %', SQLERRM;
  END;
END \$\$;
SQL

echo
echo "Verify admin role can access t_* schemas:"
psql "$TENANT_URI" -c "
  SELECT nspname,
         has_schema_privilege('$TENANT_OWNER', nspname, 'USAGE') AS usage,
         has_schema_privilege('$TENANT_OWNER', nspname, 'CREATE') AS create
  FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1;"

echo
echo "Done. Run: ./scripts/diagnose-cloud-db-grants.sh $ENV_NAME"
echo "Then: ./scripts/verify-cloud-deploy-ready.sh $ENV_NAME"
echo "Then retry Encore deploy."
