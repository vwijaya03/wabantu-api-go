#!/usr/bin/env bash
# Report tenant schema migration completeness on Encore Cloud.
#
# Usage:
#   ./scripts/verify-tenant-migrations-cloud.sh staging
#
# Required for commerce/inbox tenants: PII columns, contact patches, pricing, grants.
# Finance/events modules are reported as optional when tables are absent.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env> (e.g. staging)}"
cd "$ROOT"

SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
ADMIN_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"
WRITE_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --write)"

col_exists() {
  local schema="$1" table="$2" column="$3"
  psql "$ADMIN_URI" -tAc "
    SELECT EXISTS (
      SELECT 1 FROM information_schema.columns
      WHERE table_schema='$schema' AND table_name='$table' AND column_name='$column'
    )"
}

table_exists() {
  local schema="$1" table="$2"
  psql "$ADMIN_URI" -tAc "
    SELECT to_regclass('\"$schema\".\"$table\"') IS NOT NULL"
}

echo "=== Verify tenant migrations ($ENV_NAME) ==="

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

fail=0
while IFS= read -r schema; do
  [[ -z "$schema" ]] && continue
  echo "--- $schema ---"
  issues=()

  if [[ "$(col_exists "$schema" contact phone_number_idx)" != "t" ]]; then
    issues+=("missing contact.phone_number_idx (run ./scripts/apply-pii-schema-cloud.sh $ENV_NAME)")
  fi
  if [[ "$(col_exists "$schema" contact birth_date)" != "t" ]]; then
    issues+=("missing contact.birth_date (re-run apply-pii-schema-cloud.sh)")
  fi
  if [[ "$(col_exists "$schema" contact status)" != "t" ]]; then
    issues+=("missing contact.status (run ./scripts/apply-tenant-schema-cloud.sh $ENV_NAME)")
  fi
  if [[ "$(table_exists "$schema" branch)" != "t" ]]; then
    issues+=("missing branch table (run ./scripts/apply-tenant-schema-cloud.sh $ENV_NAME)")
  fi
  if [[ "$(table_exists "$schema" business_price_type)" != "t" ]]; then
    issues+=("missing business_price_type (run ./scripts/apply-tenant-schema-cloud.sh $ENV_NAME)")
  fi
  if [[ "$(table_exists "$schema" order)" == "t" && "$(col_exists "$schema" order income_wallet_id)" != "t" ]]; then
    issues+=("missing order.income_wallet_id (run ./scripts/apply-tenant-schema-cloud.sh $ENV_NAME)")
  fi

  plaintext="$(psql "$WRITE_URI" -tAc "
    SELECT count(*) FROM \"$schema\".contact
    WHERE deleted_at IS NULL
      AND (phone_number_enc IS NULL OR phone_number_enc = '')
      AND NULLIF(TRIM(phone_number), '') IS NOT NULL
      AND phone_number <> $'•';" 2>/dev/null || echo "?")"
  if [[ "$plaintext" =~ ^[0-9]+$ && "$plaintext" -gt 0 ]]; then
    issues+=("$plaintext contact(s) still plaintext (run ./scripts/backfill-pii-cloud.sh $ENV_NAME)")
  fi

  if [[ "$(table_exists "$schema" fin_wallet)" == "t" ]]; then
    if [[ "$(col_exists "$schema" fin_recurring title_enc)" != "t" ]]; then
      issues+=("finance: missing fin_recurring.title_enc")
    fi
  else
    echo "  (finance module not provisioned — ok for commerce-only tenant)"
  fi

  if [[ "$(table_exists "$schema" evt_event)" == "t" ]]; then
    if [[ "$(col_exists "$schema" evt_event_person full_name_enc)" != "t" ]]; then
      issues+=("events: missing evt_event_person.full_name_enc")
    fi
  else
    echo "  (events module not provisioned — ok for commerce-only tenant)"
  fi

  if ! psql "$WRITE_URI" -v ON_ERROR_STOP=1 -c "SET search_path TO \"$schema\", public; SELECT 1 FROM contact LIMIT 1;" >/dev/null 2>&1; then
    issues+=("encore_writer cannot SELECT contact (run ./scripts/fix-cloud-db-grants.sh $ENV_NAME)")
  fi

  if [[ ${#issues[@]} -eq 0 ]]; then
    echo "  ok — migrations & PII backfill look complete"
  else
    fail=1
    for msg in "${issues[@]}"; do
      echo "  ISSUE: $msg" >&2
    done
  fi
done <<< "$schemas"

echo
if [[ "$fail" -ne 0 ]]; then
  echo "Some schemas need attention. Suggested order:" >&2
  echo "  1. ./scripts/apply-tenant-schema-cloud.sh $ENV_NAME" >&2
  echo "  2. ./scripts/apply-pii-schema-cloud.sh $ENV_NAME" >&2
  echo "  3. ./scripts/backfill-pii-cloud.sh $ENV_NAME" >&2
  echo "  4. ./scripts/fix-cloud-db-grants.sh $ENV_NAME  (if writer SELECT fails)" >&2
  echo "  5. Re-run this script" >&2
  exit 1
fi
echo "All tenant schemas passed migration checks."
