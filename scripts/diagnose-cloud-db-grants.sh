#!/usr/bin/env bash
# Print DB roles + schema/table owners (debug Encore Cloud deploy grant failures).
#
# Run this first when deploy logs show:
#   failed to execute dynamic grants: permission denied for table business_profile
#   (or permission denied for schema t_*)
#
# Output highlights:
#   - registered tenants (system.tenant_company) vs t_* schemas in tenant DB
#   - schema owners and business_profile owners (watch for encore_container_*)
#   - tables not owned by db_tenant_admin / encore_admin* (block dynamic grants)
#
# Next steps (see docs/DEPLOY_ENCORE_CLOUD.md § "Hot-fix 2am"):
#   ./scripts/prune-orphan-tenant-schemas-cloud.sh staging --apply --yes
#   ./scripts/fix-cloud-db-grants.sh staging
#   ./scripts/verify-cloud-deploy-ready.sh staging
#
# Usage: ./scripts/diagnose-cloud-db-grants.sh staging
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env>}"
cd "$ROOT"

SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
TENANT_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"

echo "=== DB grant diagnostics ($ENV_NAME) ==="
echo "Admin (tenant --admin): $(psql "$TENANT_URI" -tAc 'SELECT current_user' | tr -d '[:space:]')"
echo

echo "--- Roles ---"
psql "$TENANT_URI" -c "
  SELECT rolname FROM pg_roles
  WHERE rolname ~ '^encore'
  ORDER BY 1;"

echo "--- system.tenant + tenant_company (registered schemas) ---"
psql "$SYSTEM_URI" -c "
  SELECT t.name, tc.schema_name, t.deleted_at IS NULL AS active
  FROM tenant t
  LEFT JOIN tenant_company tc ON tc.tenant_id = t.id
  ORDER BY t.created_at;"

echo "--- tenant DB schema owners ---"
psql "$TENANT_URI" -c "
  SELECT nspname AS schema, pg_get_userbyid(nspowner) AS owner
  FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1;"

echo "--- drop_tenant_schema function ---"
psql "$TENANT_URI" -c "
  SELECT p.proname,
         pg_get_userbyid(p.proowner) AS owner,
         p.prosecdef AS security_definer,
         has_function_privilege('encore_services', 'public.drop_tenant_schema(text)', 'EXECUTE') AS services_exec
  FROM pg_proc p
  JOIN pg_namespace n ON n.oid = p.pronamespace
  WHERE n.nspname = 'public' AND p.proname = 'drop_tenant_schema';"

echo "--- db_tenant_admin role members ---"
psql "$TENANT_URI" -c "
  SELECT r.rolname AS role, m.rolname AS member
  FROM pg_auth_members am
  JOIN pg_roles r ON r.oid = am.roleid
  JOIN pg_roles m ON m.oid = am.member
  WHERE r.rolname = 'db_tenant_admin'
  ORDER BY 2;"

echo "--- system DB public tables (owner) ---"
psql "$SYSTEM_URI" -c "
  SELECT tablename, tableowner FROM pg_tables
  WHERE schemaname = 'public' ORDER BY tablename LIMIT 20;"

echo "--- business_profile owners (all t_*) ---"
psql "$TENANT_URI" -c "
  SELECT n.nspname AS schema, pg_get_userbyid(c.relowner) AS owner
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relname = 'business_profile' AND c.relkind = 'r'
  ORDER BY 1;"

echo "--- t_* tables NOT owned by db_tenant_admin / encore_admin* (block dynamic grants) ---"
psql "$TENANT_URI" -c "
  SELECT n.nspname AS schema,
         pg_get_userbyid(c.relowner) AS owner,
         count(*) AS tables
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname ~ '^t_'
    AND c.relkind = 'r'
    AND pg_get_userbyid(c.relowner) !~ '^(db_tenant_admin|encore_admin)'
  GROUP BY 1, 2
  ORDER BY 1, 2;"

echo "--- admin can GRANT on sample business_profile? ---"
ADMIN_USER="$(psql "$TENANT_URI" -tAc 'SELECT current_user' | tr -d '[:space:]')"
psql "$TENANT_URI" -c "
  SELECT n.nspname AS schema,
         pg_get_userbyid(c.relowner) AS owner,
         has_table_privilege('$ADMIN_USER', n.nspname || '.' || c.relname, 'SELECT') AS admin_select
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relname = 'business_profile' AND c.relkind = 'r'
  ORDER BY 1;"
