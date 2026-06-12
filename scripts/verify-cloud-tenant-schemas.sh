#!/usr/bin/env bash
# Verify tenant schemas on Encore Cloud: tables exist + app role can SELECT.
#
# Usage: ./scripts/verify-cloud-tenant-schemas.sh staging
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env-name>}"
cd "$ROOT"

SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
TENANT_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"
WRITE_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --write)"

echo "=== Verify cloud tenant schemas ($ENV_NAME) ==="

schemas="$(psql "$SYSTEM_URI" -tAc "
  SELECT tc.schema_name
  FROM tenant_company tc
  JOIN tenant t ON t.id = tc.tenant_id
  WHERE t.deleted_at IS NULL
    AND tc.schema_name IS NOT NULL AND tc.schema_name <> ''
  ORDER BY 1")"
if [[ -z "$schemas" ]]; then
  echo "ERROR: no registered tenant schemas in tenant_company" >&2
  exit 1
fi

orphans=""
all_schemas="$(psql "$TENANT_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1")"
while IFS= read -r s; do
  [[ -z "$s" ]] && continue
  found=0
  while IFS= read -r r; do
    [[ -z "$r" ]] && continue
    [[ "$s" == "$(echo "$r" | tr -d '[:space:]')" ]] && found=1 && break
  done <<< "$schemas"
  [[ "$found" -eq 0 ]] && orphans="${orphans:+$orphans, }$s"
done <<< "$all_schemas"
if [[ -n "$orphans" ]]; then
  echo "NOTE: orphan schemas (skipped): $orphans"
  echo
fi

fail=0
while IFS= read -r schema; do
  [[ -z "$schema" ]] && continue
  echo "--- $schema ---"
  psql "$TENANT_URI" -v ON_ERROR_STOP=1 -c "SET search_path TO \"$schema\", public;" -c "
    SELECT
      to_regclass('business_catalog_item') IS NOT NULL AS catalog,
      to_regclass('business_price_type') IS NOT NULL AS pricing,
      to_regclass('fin_wallet') IS NOT NULL AS finance,
      to_regclass('evt_event') IS NOT NULL AS events,
      to_regclass('branch') IS NOT NULL AS branch;
  " || { fail=1; continue; }

  if ! psql "$WRITE_URI" -v ON_ERROR_STOP=1 -c "SET search_path TO \"$schema\", public; SELECT count(*) FROM business_catalog_item LIMIT 1;" >/dev/null 2>&1; then
    echo "  ERROR: encore_writer cannot SELECT business_catalog_item — run ./scripts/fix-cloud-db-grants.sh $ENV_NAME"
    fail=1
  else
    echo "  ok writer SELECT"
  fi
done <<< "$schemas"

tenant_count="$(psql "$SYSTEM_URI" -tAc "SELECT count(*) FROM tenant WHERE deleted_at IS NULL")"
echo
echo "system.tenant rows: $tenant_count"
if [[ "$fail" -ne 0 ]]; then
  exit 1
fi
echo "All checks passed."
