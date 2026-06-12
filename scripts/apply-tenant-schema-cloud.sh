#!/usr/bin/env bash
# Apply core tenant schema patches on Encore Cloud (pricing, contact.status, branch, …).
#
# Prerequisite: encore auth login, admin access to tenant DB.
#
# Usage:
#   ./scripts/apply-tenant-schema-cloud.sh staging
#
# On cloud the app runtime cannot CREATE/ALTER — this script uses --admin.
# Run before or after apply-pii-schema-cloud.sh (order does not matter).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env> (e.g. staging)}"
cd "$ROOT"

ADMIN_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"
PATCH_SQL="$(go run ./scripts/cmd/cloud-tenant-patch-sql/)"

echo "=== Apply tenant schema patch ($ENV_NAME) ==="

schemas="$(psql "$ADMIN_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1")"
if [[ -z "$schemas" ]]; then
  echo "ERROR: no t_* schemas found" >&2
  exit 1
fi

fail=0
while IFS= read -r schema; do
  [[ -z "$schema" ]] && continue
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

echo "Done. Verify: ./scripts/verify-tenant-migrations-cloud.sh $ENV_NAME"
