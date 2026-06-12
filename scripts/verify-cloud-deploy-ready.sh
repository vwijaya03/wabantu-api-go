#!/usr/bin/env bash
# Pre-deploy check: DB ownership compatible with Encore Cloud dynamic grants.
#
# Usage: ./scripts/verify-cloud-deploy-ready.sh staging
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env> (e.g. staging)}"
cd "$ROOT"

SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
TENANT_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"

TARGET_ROLE="$(psql "$TENANT_URI" -tAc "SELECT COALESCE(
  (SELECT rolname FROM pg_roles WHERE rolname = 'encore-migrator' LIMIT 1),
  current_user
)" | tr -d '[:space:]')"

echo "=== Encore deploy readiness ($ENV_NAME) ==="
echo "Expected owner: $TARGET_ROLE"
echo

fail=0

sm_owner="$(psql "$SYSTEM_URI" -tAc "
  SELECT tableowner FROM pg_tables
  WHERE schemaname = 'public' AND tablename = 'schema_migrations' LIMIT 1" | tr -d '[:space:]')"
if [[ -z "$sm_owner" ]]; then
  echo "WARN: public.schema_migrations not found (fresh DB — OK on first deploy)"
elif [[ "$sm_owner" != "$TARGET_ROLE" ]]; then
  echo "FAIL: schema_migrations owner=$sm_owner (want $TARGET_ROLE)"
  fail=1
else
  echo "OK: schema_migrations owner=$sm_owner"
fi

bad_schemas="$(psql "$TENANT_URI" -tAc "
  SELECT string_agg(nspname, ', ')
  FROM pg_namespace
  WHERE nspname ~ '^t_' AND pg_get_userbyid(nspowner) <> '$TARGET_ROLE'")"
if [[ -n "$bad_schemas" ]]; then
  echo "FAIL: tenant schemas not owned by $TARGET_ROLE: $bad_schemas"
  fail=1
else
  count="$(psql "$TENANT_URI" -tAc "SELECT count(*) FROM pg_namespace WHERE nspname ~ '^t_'")"
  echo "OK: $count tenant schema(s) owned by $TARGET_ROLE"
fi

echo
if [[ "$fail" -ne 0 ]]; then
  echo "Run: ./scripts/fix-cloud-db-grants.sh $ENV_NAME" >&2
  exit 1
fi
echo "DB ready for Encore deploy."
