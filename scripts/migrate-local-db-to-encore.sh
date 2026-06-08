#!/usr/bin/env bash
# Migrate Postgres data from local Encore (encore run) to an Encore Cloud environment.
#
# Prerequisites:
#   1. Local app has data: `encore run` has been used locally.
#   2. Target env already deployed at least once (schema from Encore migrations exists).
#   3. pg_dump / pg_restore installed (PostgreSQL client tools).
#
# Usage:
#   ./scripts/migrate-local-db-to-encore.sh staging
#   ./scripts/migrate-local-db-to-encore.sh staging --dry-run
#
# Notes:
#   - Migrates `system` and `tenant` Encore databases (data only).
#   - Redis (sessions, import staging, AI retry counters) is NOT migrated — users re-login.
#   - Stop writes to local DB during export for a consistent snapshot.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env-name> [--dry-run]}"
DRY_RUN="${2:-}"

if [[ "$ENV_NAME" == "--help" || "$ENV_NAME" == "-h" ]]; then
  sed -n '2,20p' "$0"
  exit 0
fi

STAMP="$(date +%Y%m%d_%H%M%S)"
BACKUP_DIR="${BACKUP_DIR:-$ROOT/.db-migrate/$STAMP}"
mkdir -p "$BACKUP_DIR"

cd "$ROOT"

echo "=== WABantu DB migrate: local → Encore Cloud ($ENV_NAME) ==="
echo "Backup dir: $BACKUP_DIR"
echo

LOCAL_SYSTEM_URI="$(encore db conn-uri system)"
LOCAL_TENANT_URI="$(encore db conn-uri tenant)"
CLOUD_SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME")"
CLOUD_TENANT_URI="$(encore db conn-uri tenant --env="$ENV_NAME")"

SYSTEM_DUMP="$BACKUP_DIR/system_data_${STAMP}.dump"
TENANT_DUMP="$BACKUP_DIR/tenant_data_${STAMP}.dump"

echo "[1/4] Export local system DB (data only)..."
pg_dump "$LOCAL_SYSTEM_URI" -Fc --data-only --disable-triggers -f "$SYSTEM_DUMP"
echo "  → $SYSTEM_DUMP"

echo "[2/4] Export local tenant DB (data only, all schemas)..."
pg_dump "$LOCAL_TENANT_URI" -Fc --data-only --disable-triggers -f "$TENANT_DUMP"
echo "  → $TENANT_DUMP"

if [[ "$DRY_RUN" == "--dry-run" ]]; then
  echo
  echo "Dry run — dumps created, skipping restore."
  echo "Cloud URIs (temporary):"
  echo "  system: $CLOUD_SYSTEM_URI"
  echo "  tenant: $CLOUD_TENANT_URI"
  exit 0
fi

read -r -p "Restore into Encore Cloud env '$ENV_NAME'? This OVERWRITES cloud data. [y/N] " confirm
if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
  echo "Aborted. Dumps kept in $BACKUP_DIR"
  exit 1
fi

echo "[3/4] Restore system data to cloud..."
pg_restore -d "$CLOUD_SYSTEM_URI" --data-only --disable-triggers --no-owner --no-privileges "$SYSTEM_DUMP"

echo "[4/4] Restore tenant data to cloud..."
pg_restore -d "$CLOUD_TENANT_URI" --data-only --disable-triggers --no-owner --no-privileges "$TENANT_DUMP"

echo
echo "Done. Verify with:"
echo "  encore db shell system --env=$ENV_NAME --write"
echo "  encore db shell tenant --env=$ENV_NAME --write"
echo "  SELECT count(*) FROM tenant_company;"
echo
echo "Redis was not migrated — ask users to log in again."
