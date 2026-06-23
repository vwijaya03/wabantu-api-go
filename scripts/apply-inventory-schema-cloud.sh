#!/usr/bin/env bash
# Apply inventory/HPP module schema (inv_*) on Encore Cloud for every tenant schema.
#
# The inventory module only creates NEW tables (inv_setting, inv_warehouse, inv_sku,
# and in later PRs inv_stock_movement, inv_stock_balance, inv_cost_layer, …), so the
# app role can normally create them at runtime. This script is the explicit, auditable
# admin path (and is required if the app role lacks CREATE on a schema).
#
# Prerequisite: encore auth login, admin access to tenant DB.
#
# Usage:
#   ./scripts/apply-inventory-schema-cloud.sh staging
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env> (e.g. staging)}"
cd "$ROOT"

ADMIN_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"
PATCH_SQL="$(go run ./scripts/cmd/cloud-inventory-patch-sql/)"

echo "=== Apply inventory schema patch ($ENV_NAME) ==="

schemas="$(psql "$ADMIN_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1")"
if [[ -z "$schemas" ]]; then
  echo "ERROR: no t_* schemas found" >&2
  exit 1
fi

fail=0
apply_schema() {
  local schema="$1"
  local attempt
  local retries="${RETRIES:-3}"
  for ((attempt = 1; attempt <= retries; attempt++)); do
    if psql "$ADMIN_URI" -v ON_ERROR_STOP=1 -c "SET search_path TO \"$schema\", public;" -c "$PATCH_SQL"; then
      return 0
    fi
    echo "  retry $attempt/$retries for $schema..." >&2
    sleep "$((attempt * 2))"
  done
  return 1
}

while IFS= read -r schema; do
  [[ -z "$schema" ]] && continue
  echo "  -> $schema"
  if ! apply_schema "$schema"; then
    echo "  ERROR: inventory patch failed for $schema" >&2
    echo "  Hint: ./scripts/apply-inventory-column-patch-cloud.sh $ENV_NAME $schema" >&2
    fail=1
  fi
done <<< "$schemas"

if [[ "$fail" -ne 0 ]]; then
  echo "Some schemas failed — fix errors above and re-run." >&2
  exit 1
fi

echo "Done. Inventory tables present on all tenant schemas for $ENV_NAME."
