#!/usr/bin/env bash
# Print DB roles + schema owners (debug Encore deploy grant failures).
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

echo "--- system DB public tables (owner) ---"
psql "$SYSTEM_URI" -c "
  SELECT tablename, tableowner FROM pg_tables
  WHERE schemaname = 'public' ORDER BY tablename LIMIT 20;"
