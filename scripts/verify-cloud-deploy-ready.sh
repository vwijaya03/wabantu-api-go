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

ADMIN_ROLE="$(psql "$TENANT_URI" -tAc "SELECT current_user" | tr -d '[:space:]')"
MIGRATOR_ROLE="$(psql "$TENANT_URI" -tAc "SELECT rolname FROM pg_roles WHERE rolname = 'encore-migrator' LIMIT 1" | tr -d '[:space:]')"
SYSTEM_OWNER="${MIGRATOR_ROLE:-$ADMIN_ROLE}"
TENANT_OWNER="$ADMIN_ROLE"

echo "=== Encore deploy readiness ($ENV_NAME) ==="
echo "Tenant t_* owner expected: $TENANT_OWNER"
echo "System schema_migrations owner expected: $SYSTEM_OWNER"
echo

fail=0

sm_owner="$(psql "$SYSTEM_URI" -tAc "
  SELECT tableowner FROM pg_tables
  WHERE schemaname = 'public' AND tablename = 'schema_migrations' LIMIT 1" | tr -d '[:space:]')"
if [[ -z "$sm_owner" ]]; then
  echo "WARN: public.schema_migrations not found (fresh DB — OK on first deploy)"
elif [[ "$sm_owner" != "$SYSTEM_OWNER" ]]; then
  echo "FAIL: schema_migrations owner=$sm_owner (want $SYSTEM_OWNER)"
  fail=1
else
  echo "OK: schema_migrations owner=$sm_owner"
fi

bad_schemas="$(psql "$TENANT_URI" -tAc "
  SELECT string_agg(nspname, ', ')
  FROM pg_namespace
  WHERE nspname ~ '^t_'
    AND pg_get_userbyid(nspowner) <> '$TENANT_OWNER'")"
if [[ -n "$bad_schemas" ]]; then
  echo "FAIL: tenant schemas wrong owner (want $TENANT_OWNER): $bad_schemas"
  fail=1
else
  count="$(psql "$TENANT_URI" -tAc "SELECT count(*) FROM pg_namespace WHERE nspname ~ '^t_'")"
  echo "OK: $count tenant schema(s) owned by $TENANT_OWNER"
fi

# Orphans: t_* in tenant DB but not registered in system.tenant
mapfile -t registered < <(psql "$SYSTEM_URI" -tAc "SELECT schema_name FROM tenant WHERE deleted_at IS NULL")
mapfile -t all_schemas < <(psql "$TENANT_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_'")
orphan_list=()
for s in "${all_schemas[@]}"; do
  [[ -z "$s" ]] && continue
  found=0
  for r in "${registered[@]}"; do
    [[ "$s" == "$(echo "$r" | tr -d '[:space:]')" ]] && found=1 && break
  done
  [[ "$found" -eq 0 ]] && orphan_list+=("$s")
done
if [[ ${#orphan_list[@]} -gt 0 ]]; then
  echo "WARN: orphan schemas (not in system.tenant): ${orphan_list[*]}"
  echo "      Consider: ./scripts/prune-orphan-tenant-schemas-cloud.sh $ENV_NAME --apply"
fi

echo
if [[ "$fail" -ne 0 ]]; then
  echo "Run: ./scripts/fix-cloud-db-grants.sh $ENV_NAME" >&2
  exit 1
fi
echo "DB ready for Encore deploy."
