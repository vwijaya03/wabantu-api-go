#!/usr/bin/env bash
# Fix Postgres ownership + GRANTs after migrate-local-db-to-encore.sh (Encore Cloud).
#
# Encore Cloud:
#   - system DB migrations → role "encore-migrator"
#   - tenant DB dynamic grants on t_* → role admin (--admin conn-uri), NOT migrator
#
# pg_restore --no-owner leaves wrong owners → deploy fails:
#   permission denied for schema t_*
#
# Usage: ./scripts/fix-cloud-db-grants.sh staging
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env-name> (e.g. staging)}"
cd "$ROOT"

SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
TENANT_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"

ADMIN_ROLE="$(psql "$TENANT_URI" -tAc "SELECT current_user" | tr -d '[:space:]')"
MIGRATOR_ROLE="$(psql "$TENANT_URI" -tAc "SELECT rolname FROM pg_roles WHERE rolname = 'encore-migrator' LIMIT 1" | tr -d '[:space:]')"
SYSTEM_OWNER="${MIGRATOR_ROLE:-$ADMIN_ROLE}"
TENANT_OWNER="$ADMIN_ROLE"

if [[ -z "$ADMIN_ROLE" ]]; then
  echo "ERROR: could not resolve admin DB role" >&2
  exit 1
fi

echo "=== Fix cloud DB grants ($ENV_NAME) ==="
echo "Admin role (tenant t_* owner): $TENANT_OWNER"
echo "Migrator role (system migrations): ${MIGRATOR_ROLE:-<not found, using admin>}"
echo

fix_schemas() {
  local label="$1"
  local uri="$2"
  local schema_regex="$3"
  local owner_role="$4"

  echo "--- $label (owner → $owner_role) ---"
  psql "$uri" -v ON_ERROR_STOP=1 <<SQL
DO \$\$
DECLARE
  r record;
  s text;
  owner_role text := '${owner_role}';
BEGIN
  -- Reassign tables/sequences to the Encore role that runs grants/migrations.
  FOR r IN
    SELECT DISTINCT pg_get_userbyid(c.relowner) AS owner
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname ~ '${schema_regex}'
      AND pg_get_userbyid(c.relowner) IS NOT NULL
      AND pg_get_userbyid(c.relowner) <> owner_role
  LOOP
    BEGIN
      EXECUTE format('REASSIGN OWNED BY %I TO %I', r.owner, owner_role);
      RAISE NOTICE 'reassigned objects from % to %', r.owner, owner_role;
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'skip reassign from %: %', r.owner, SQLERRM;
    END;
  END LOOP;

  FOR s IN SELECT nspname FROM pg_namespace WHERE nspname ~ '${schema_regex}' ORDER BY 1 LOOP
    EXECUTE format('ALTER SCHEMA %I OWNER TO %I', s, owner_role);
    EXECUTE format('GRANT ALL ON SCHEMA %I TO %I', s, owner_role);
    EXECUTE format('GRANT USAGE, CREATE ON SCHEMA %I TO encore_writer, encore_reader, encore_services', s);
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'encore-migrator') THEN
      EXECUTE format('GRANT ALL ON SCHEMA %I TO encore-migrator', s);
    END IF;
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
  END LOOP;

  IF to_regclass('public.schema_migrations') IS NOT NULL AND '${schema_regex}' = '^public\$' THEN
    EXECUTE format('ALTER TABLE public.schema_migrations OWNER TO %I', owner_role);
    GRANT SELECT, INSERT, UPDATE, DELETE ON public.schema_migrations TO encore_writer, encore_services;
    GRANT SELECT ON public.schema_migrations TO encore_reader;
  END IF;
END \$\$;
SQL
}

fix_schemas "system DB (public)" "$SYSTEM_URI" '^public\$' "$SYSTEM_OWNER"
fix_schemas "tenant DB (t_*)" "$TENANT_URI" '^t_' "$TENANT_OWNER"

echo
echo "Tenant schemas (run diagnose-cloud-db-grants.sh for orphan check):"
psql "$TENANT_URI" -c "
  SELECT nspname AS schema, pg_get_userbyid(nspowner) AS owner
  FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1;"

echo
echo "Verify system schema_migrations (expect: $SYSTEM_OWNER):"
psql "$SYSTEM_URI" -c "
  SELECT tablename, tableowner FROM pg_tables
  WHERE schemaname = 'public' AND tablename = 'schema_migrations';"

echo
echo "Verify tenant schema owners (expect: $TENANT_OWNER):"
psql "$TENANT_URI" -c "
  SELECT nspname AS schema, pg_get_userbyid(nspowner) AS owner
  FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1;"

echo
echo "Done. Run: ./scripts/verify-cloud-deploy-ready.sh $ENV_NAME"
echo "Then retry Encore deploy."
