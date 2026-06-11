#!/usr/bin/env bash
# Apply PII column DDL to all tenant schemas on Encore Cloud (requires --admin).
#
# Usage:
#   ./scripts/apply-pii-schema-cloud.sh staging
#
# Webhook/inbox use encrypted contact writes only when phone_number_idx exists.
# Until this runs, the app falls back to legacy phone_number columns.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env> (e.g. staging)}"
cd "$ROOT"

ADMIN_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"

PATCH_SQL="$(go run ./scripts/cmd/pii-patch-sql/)"

echo "=== Apply PII schema patch ($ENV_NAME) ==="

schemas="$(psql "$ADMIN_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1")"
if [[ -z "$schemas" ]]; then
  echo "ERROR: no t_* schemas found" >&2
  exit 1
fi

while IFS= read -r schema; do
  [[ -z "$schema" ]] && continue
  echo "  -> $schema"
  psql "$ADMIN_URI" -v ON_ERROR_STOP=1 -c "SET search_path TO \"$schema\", public;" -c "$PATCH_SQL"
done <<< "$schemas"

echo "Done. Verify: ./scripts/verify-cloud-tenant-schemas.sh $ENV_NAME"
