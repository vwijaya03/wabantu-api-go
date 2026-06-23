#!/usr/bin/env bash
# Apply only inventory column/index patches required before migrate-tenant-schemas on cloud.
# Much faster than apply-inventory-schema-cloud.sh (full InventorySchemaSQL).
#
# Usage:
#   ./scripts/apply-inventory-column-patch-cloud.sh staging
#   ./scripts/apply-inventory-column-patch-cloud.sh staging t_omah_apparel
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env> [schema_name]}"
SCHEMA_FILTER="${2:-}"
RETRIES="${RETRIES:-3}"
cd "$ROOT"

read -r -d '' PATCH_SQL <<'EOSQL' || true
ALTER TABLE inv_setting ADD COLUMN IF NOT EXISTS purchase_posts_expense BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE inv_setting ADD COLUMN IF NOT EXISTS stock_txn_backfill_done BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS idx_inv_movement_orphan_backfill
    ON inv_stock_movement(created_at)
    WHERE ref_id IS NULL
      AND movement_type IN ('adjustment_plus','adjustment_minus','opening_balance','transfer_out','revaluation_cost');
CREATE INDEX IF NOT EXISTS idx_inv_stock_txn_line_item_wh
    ON inv_stock_transaction_line(catalog_item_id, warehouse_id);
ALTER TABLE inv_warehouse ADD COLUMN IF NOT EXISTS customer_label VARCHAR(80);
EOSQL

apply_schema() {
  local schema="$1"
  local attempt
  for ((attempt = 1; attempt <= RETRIES; attempt++)); do
    if psql "$ADMIN_URI" -v ON_ERROR_STOP=1 \
      -c "SET search_path TO \"$schema\", public;" \
      -c "$PATCH_SQL"; then
      return 0
    fi
    echo "  retry $attempt/$RETRIES for $schema..." >&2
    sleep "$((attempt * 2))"
  done
  return 1
}

ADMIN_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"

echo "=== Apply inventory column patch ($ENV_NAME) ==="

if [[ -n "$SCHEMA_FILTER" ]]; then
  schemas="$SCHEMA_FILTER"
else
  schemas="$(psql "$ADMIN_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1")"
fi

if [[ -z "$schemas" ]]; then
  echo "ERROR: no schemas to patch" >&2
  exit 1
fi

fail=0
while IFS= read -r schema; do
  [[ -z "$schema" ]] && continue
  echo "  -> $schema"
  if ! apply_schema "$schema"; then
    echo "  ERROR: column patch failed for $schema" >&2
    fail=1
  fi
done <<< "$schemas"

if [[ "$fail" -ne 0 ]]; then
  echo "Some schemas failed — re-run with a single schema, e.g.:" >&2
  echo "  $0 $ENV_NAME t_omah_apparel" >&2
  exit 1
fi

echo "Done. Column patch applied for $ENV_NAME."
