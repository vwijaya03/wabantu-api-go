#!/usr/bin/env bash
# Perbaiki cluster Postgres *test* Encore yang rusak (hanya role postgres, tanpa encore-migrator).
#
# Gejala:
#   encore test ./ai/ ...
#   create db system: ERROR: role "encore-migrator" does not exist
#
# Penyebab: container test cluster dibuat/upgrade tanpa bootstrap role Encore
# (volume lama, daemon restart, atau upgrade image postgres).
#
# Usage:
#   ./scripts/fix-encore-test-db.sh
#   ./scripts/run-ai-unit-tests.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

APP_ID="$(python3 -c "import json; print(json.load(open('encore.app'))['id'])" 2>/dev/null || true)"
if [[ -z "$APP_ID" ]]; then
  echo "error: could not read app id from encore.app" >&2
  exit 1
fi

CONTAINER="sqldb-${APP_ID}-test-default"
MATCH="$(docker ps -aq -f "name=${CONTAINER}" 2>/dev/null || true)"

if [[ -z "$MATCH" ]]; then
  echo "No test Postgres container found for ${APP_ID}."
  echo "Run: encore test ./ai/ -run TestMatrix_ParseOrderQty -count=1"
  echo "Encore will create a fresh test cluster."
  exit 0
fi

echo "Checking roles in test cluster container(s): ${MATCH}"
BROKEN=0
while read -r cid; do
  [[ -z "$cid" ]] && continue
  if ! docker exec "$cid" psql -U postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname='encore-migrator'" 2>/dev/null | grep -q 1; then
    echo "  - $cid: MISSING encore-migrator (will remove)"
    BROKEN=1
  else
    echo "  - $cid: OK (encore-migrator exists)"
  fi
done <<< "$MATCH"

if [[ "$BROKEN" -eq 0 ]]; then
  echo "Test cluster looks healthy. If encore test still fails, try: encore db reset --all --test"
  exit 0
fi

echo "Removing broken test cluster container(s)..."
docker rm -f $MATCH >/dev/null
echo "Done. Encore will recreate the test cluster on the next 'encore test' run."
echo ""
echo "Next: ./scripts/run-ai-unit-tests.sh"
