#!/usr/bin/env bash
# Apply core tenant schema patches on Encore Cloud (pricing, contact.status, branch, …).
#
# Prerequisite: encore auth login, admin access to tenant DB.
#
# Usage:
#   ./scripts/apply-tenant-schema-cloud.sh staging
#   ./scripts/apply-tenant-schema-cloud.sh staging --limit 1000 --cursor 0
#
# On cloud the app runtime cannot CREATE/ALTER — this script uses --admin.
# Run before or after apply-pii-schema-cloud.sh (order does not matter).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env> (e.g. staging)}"
shift || true

LIMIT=0
CURSOR=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --limit) LIMIT="${2:?}"; shift 2 ;;
    --cursor) CURSOR="${2:?}"; shift 2 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

cd "$ROOT"

ADMIN_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"
PATCH_SQL="$(go run ./scripts/cmd/cloud-tenant-patch-sql/)"

echo "=== Apply tenant schema patch ($ENV_NAME) limit=$LIMIT cursor=$CURSOR ==="

if [[ "$LIMIT" -gt 0 ]]; then
  schemas="$(psql "$ADMIN_URI" -tAc "
    SELECT nspname FROM pg_namespace
    WHERE nspname ~ '^t_'
    ORDER BY 1
    OFFSET $CURSOR LIMIT $LIMIT
  ")"
else
  schemas="$(psql "$ADMIN_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1")"
fi

if [[ -z "$schemas" ]]; then
  echo "No t_* schemas in this batch (cursor=$CURSOR limit=$LIMIT)." >&2
  exit 0
fi

fail=0
count=0
while IFS= read -r schema; do
  [[ -z "$schema" ]] && continue
  count=$((count + 1))
  echo "  -> $schema"
  if ! psql "$ADMIN_URI" -v ON_ERROR_STOP=1 -c "SET search_path TO \"$schema\", public;" -c "$PATCH_SQL"; then
    echo "  ERROR: patch failed for $schema" >&2
    fail=1
  fi
done <<< "$schemas"

if [[ "$fail" -ne 0 ]]; then
  echo "Some schemas failed — fix errors above and re-run." >&2
  exit 1
fi

next_cursor=$((CURSOR + count))
echo "Done. Processed $count schema(s). Next cursor: $next_cursor"
echo "Verify: ./scripts/verify-tenant-migrations-cloud.sh $ENV_NAME"
if [[ "$LIMIT" -gt 0 ]]; then
  echo "Next wave: $0 $ENV_NAME --limit $LIMIT --cursor $next_cursor"
fi
