#!/usr/bin/env bash
# Add evt_event break columns on Encore Cloud tenant schemas (requires --admin).
#
# Needed when API code expects break_start_time/break_end_time but CloudTenantReady
# skipped runtime DDL. Safe to re-run (IF NOT EXISTS).
#
# Usage:
#   ./scripts/patch-events-break-columns-cloud.sh staging
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env> (e.g. staging)}"
cd "$ROOT"

SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
TENANT_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"

PATCH_SQL="
ALTER TABLE evt_event ADD COLUMN IF NOT EXISTS break_start_time TIME;
ALTER TABLE evt_event ADD COLUMN IF NOT EXISTS break_end_time TIME;
"

echo "=== Patch evt_event break columns ($ENV_NAME) ==="

schemas="$(psql "$SYSTEM_URI" -tAc "
  SELECT tc.schema_name
  FROM tenant_company tc
  JOIN tenant t ON t.id = tc.tenant_id
  WHERE t.deleted_at IS NULL
    AND tc.schema_name IS NOT NULL AND tc.schema_name <> ''
  ORDER BY 1")"

if [[ -z "$schemas" ]]; then
  echo "No registered tenant schemas."
  exit 0
fi

while IFS= read -r schema; do
  [[ -z "$schema" ]] && continue
  has_evt="$(psql "$TENANT_URI" -tAc "
    SELECT to_regclass('\"$schema\".evt_event') IS NOT NULL" | tr -d '[:space:]')"
  if [[ "$has_evt" != "t" ]]; then
    echo "  -> $schema: no evt_event — skip"
    continue
  fi
  echo "  -> $schema"
  psql "$TENANT_URI" -v ON_ERROR_STOP=1 -c "SET search_path TO \"$schema\", public; $PATCH_SQL"
done <<< "$schemas"

echo "Done."
