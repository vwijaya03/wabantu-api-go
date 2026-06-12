#!/usr/bin/env bash
# Backfill legacy plaintext PII into encrypted columns for all tenant schemas on Encore Cloud.
#
# Prerequisite: ./scripts/apply-pii-schema-cloud.sh <env>
#
# Usage:
#   ./scripts/backfill-pii-cloud.sh staging
#
# Reads DATA_ENCRYPTION_KEY from ../api/.env when not set in environment.
# Must match Encore secret DataEncryptionKey for that environment.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
API_ENV="${ROOT}/../api/.env"
ENV_NAME="${1:?Usage: $0 <encore-env> (e.g. staging)}"
cd "$ROOT"

if [[ -z "${DATA_ENCRYPTION_KEY:-}" && -f "$API_ENV" ]]; then
  DATA_ENCRYPTION_KEY="$(grep -E '^DATA_ENCRYPTION_KEY=' "$API_ENV" | head -1 | cut -d= -f2- | tr -d '"' | tr -d "'")"
  export DATA_ENCRYPTION_KEY
fi
if [[ -z "${DATA_ENCRYPTION_KEY:-}" ]]; then
  echo "ERROR: set DATA_ENCRYPTION_KEY or add to api/.env" >&2
  exit 1
fi

WRITE_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --write)"
ADMIN_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"
export DATABASE_URL="$WRITE_URI"

echo "=== PII backfill all tenants ($ENV_NAME) ==="

schemas="$(psql "$ADMIN_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1")"
if [[ -z "$schemas" ]]; then
  echo "No t_* schemas found."
  exit 0
fi

fail=0
skipped=0
done_count=0

while IFS= read -r schema; do
  [[ -z "$schema" ]] && continue
  echo "--- $schema ---"

  has_pii="$(psql "$WRITE_URI" -tAc "
    SELECT EXISTS (
      SELECT 1 FROM information_schema.columns
      WHERE table_schema='$schema' AND table_name='contact' AND column_name='phone_number_idx'
    )")"
  if [[ "$has_pii" != "t" ]]; then
    echo "  skip (PII columns missing — run ./scripts/apply-pii-schema-cloud.sh $ENV_NAME)"
    skipped=$((skipped + 1))
    continue
  fi

  if ! go run ./scripts/cmd/backfill-pii/ -schema="$schema"; then
    echo "  ERROR: backfill failed for $schema" >&2
    fail=1
    continue
  fi
  done_count=$((done_count + 1))
done <<< "$schemas"

echo
echo "Summary: $done_count backfilled, $skipped skipped"
if [[ "$fail" -ne 0 ]]; then
  echo "Some schemas failed — fix errors above and re-run." >&2
  exit 1
fi
echo "Done."
