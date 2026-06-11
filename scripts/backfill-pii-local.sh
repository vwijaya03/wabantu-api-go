#!/usr/bin/env bash
# Apply PII DDL + backfill plaintext rows for all local tenant schemas.
#
# Usage:
#   ./scripts/backfill-pii-local.sh
#
# Reads DATA_ENCRYPTION_KEY from ../api/.env when not set in environment.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
API_ENV="${ROOT}/../api/.env"
cd "$ROOT"

if [[ -z "${DATA_ENCRYPTION_KEY:-}" && -f "$API_ENV" ]]; then
  DATA_ENCRYPTION_KEY="$(grep -E '^DATA_ENCRYPTION_KEY=' "$API_ENV" | head -1 | cut -d= -f2- | tr -d '"' | tr -d "'")"
  export DATA_ENCRYPTION_KEY
fi
if [[ -z "${DATA_ENCRYPTION_KEY:-}" ]]; then
  echo "ERROR: set DATA_ENCRYPTION_KEY or add to api/.env" >&2
  exit 1
fi

ENV_NAME="${ENCORE_ENV:-local}"
SUPER_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --superuser)"
WRITE_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --write)"
export DATABASE_URL="$WRITE_URI"
PATCH_SQL="$(go run ./scripts/cmd/pii-patch-sql/)"

echo "=== PII DDL + backfill ($ENV_NAME) ==="

schemas="$(psql "$SUPER_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1")"
if [[ -z "$schemas" ]]; then
  echo "No t_* schemas found."
  exit 0
fi

while IFS= read -r schema; do
  [[ -z "$schema" ]] && continue
  echo "--- $schema ---"
  has_pii="$(psql "$WRITE_URI" -tAc "
    SELECT EXISTS (
      SELECT 1 FROM information_schema.columns
      WHERE table_schema='$schema' AND table_name='contact' AND column_name='phone_number_idx'
    )")"
  if [[ "$has_pii" != "t" && "${SKIP_DDL:-}" != "1" ]]; then
    if ! psql "$SUPER_URI" -v ON_ERROR_STOP=1 -c "SET search_path TO \"$schema\", public;" -c "$PATCH_SQL" 2>/dev/null; then
      echo "  DDL skipped (table owner is encore-service on restored DBs)."
      echo "  Start API once: encore run — then open Inbox (applies PII DDL), re-run: SKIP_DDL=1 $0"
      continue
    fi
  fi
  if [[ "$has_pii" != "t" ]]; then
    has_pii="$(psql "$WRITE_URI" -tAc "
      SELECT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema='$schema' AND table_name='contact' AND column_name='phone_number_idx'
      )")"
  fi
  if [[ "$has_pii" != "t" ]]; then
    echo "  skip backfill (PII columns missing — trigger webhook or inbox once)"
    continue
  fi
  go run ./scripts/cmd/backfill-pii/ -schema="$schema"
done <<< "$schemas"

echo "Done."
