#!/usr/bin/env bash
# Migrate Postgres data from local Encore (encore run) to an Encore Cloud environment.
#
# Prerequisites:
#   1. Local app has data: `encore run` has been used locally.
#   2. Target env already deployed at least once (Encore app reachable).
#   3. pg_dump / pg_restore installed (PostgreSQL client tools).
#
# Usage:
#   ./scripts/migrate-local-db-to-encore.sh staging
#   ./scripts/migrate-local-db-to-encore.sh staging --dry-run
#
# Notes:
#   - Copies `system` + `tenant` schema (DDL) from local, then data.
#   - Cloud DB may be empty (no Encore migration tables) — script creates schema from local.
#   - Cloud restore uses `encore db conn-uri --admin` (default URI is read-only).
#   - Managed Postgres (Encore Cloud) cannot DISABLE system FK triggers — restore
#     omits --disable-triggers; harmless warnings (public exists, RI_ConstraintTrigger).
#   - Redis (sessions, import staging, AI retry counters) is NOT migrated — users re-login.
#   - Stop writes to local DB during export for a consistent snapshot.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env-name> [--dry-run]}"
DRY_RUN="${2:-}"

if [[ "$ENV_NAME" == "--help" || "$ENV_NAME" == "-h" ]]; then
  sed -n '2,22p' "$0"
  exit 0
fi

# pg_restore: 0 = ok, 1 = warnings (public exists, RI_ConstraintTrigger), 2+ = fatal
pg_restore_tolerant() {
  local label="${1:-pg_restore}"
  shift
  local log err=0
  log="$(mktemp)"
  pg_restore "$@" >"$log" 2>&1 || err=$?
  if grep -v -E 'already exists|RI_ConstraintTrigger|errors ignored on restore' "$log" | grep -qi 'error:'; then
    cat "$log" >&2
    rm -f "$log"
    [[ "$err" -gt 1 ]] && return "$err"
  elif [[ "$err" -ge 1 ]]; then
    echo "  $label: warnings only (managed Postgres — safe if row counts match below)"
  fi
  rm -f "$log"
  [[ "$err" -gt 1 ]] && return "$err"
  return 0
}

# pg_restore --no-privileges strips GRANTs; Encore app users need these on cloud.
cloud_grant_system() {
  local uri="$1"
  psql "$uri" -v ON_ERROR_STOP=1 <<'SQL'
GRANT USAGE ON SCHEMA public TO encore_writer, encore_reader, encore_services;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO encore_writer, encore_services;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO encore_reader;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO encore_writer, encore_services;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO encore_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO encore_writer, encore_services;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO encore_reader;
SQL
}

cloud_grant_tenant_schemas() {
  local uri="$1"
  psql "$uri" -v ON_ERROR_STOP=1 <<'SQL'
DO $$
DECLARE s text;
BEGIN
  FOR s IN SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' LOOP
    EXECUTE format('ALTER SCHEMA %I OWNER TO %I', s, current_user);
    EXECUTE format('GRANT USAGE, CREATE ON SCHEMA %I TO encore_writer, encore_reader, encore_services', s);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO encore_writer, encore_services', s);
    EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA %I TO encore_reader', s);
    EXECUTE format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %I TO encore_writer, encore_services', s);
    EXECUTE format('GRANT SELECT ON ALL SEQUENCES IN SCHEMA %I TO encore_reader', s);
  END LOOP;
END $$;
SQL
}

STAMP="$(date +%Y%m%d_%H%M%S)"
BACKUP_DIR="${BACKUP_DIR:-$ROOT/.db-migrate/$STAMP}"
mkdir -p "$BACKUP_DIR"

cd "$ROOT"

echo "=== WABantu DB migrate: local → Encore Cloud ($ENV_NAME) ==="
echo "Backup dir: $BACKUP_DIR"
echo

LOCAL_SYSTEM_URI="$(encore db conn-uri system)"
LOCAL_TENANT_URI="$(encore db conn-uri tenant)"
# Cloud default conn-uri is read-only (encore_reader). Restore needs --admin for
# INSERT + DISABLE TRIGGER; --write alone still fails on --disable-triggers.
CLOUD_SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
CLOUD_TENANT_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"

SYSTEM_SCHEMA_DUMP="$BACKUP_DIR/system_schema_${STAMP}.dump"
SYSTEM_DUMP="$BACKUP_DIR/system_data_${STAMP}.dump"
TENANT_SCHEMA_DUMP="$BACKUP_DIR/tenant_schema_${STAMP}.dump"
TENANT_DUMP="$BACKUP_DIR/tenant_data_${STAMP}.dump"

echo "[1/6] Export local system DB (schema + data)..."
pg_dump "$LOCAL_SYSTEM_URI" -Fc --schema-only --no-owner --no-privileges --schema=public \
  -f "$SYSTEM_SCHEMA_DUMP"
echo "  → $SYSTEM_SCHEMA_DUMP"
pg_dump "$LOCAL_SYSTEM_URI" -Fc --data-only --disable-triggers -f "$SYSTEM_DUMP"
echo "  → $SYSTEM_DUMP"

echo "[2/6] Export local tenant schemas (DDL) + data..."
TENANT_SCHEMAS=()
while IFS= read -r schema; do
  [[ -z "$schema" ]] && continue
  TENANT_SCHEMAS+=("$schema")
done < <(psql "$LOCAL_TENANT_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1")

if [[ ${#TENANT_SCHEMAS[@]} -eq 0 ]]; then
  echo "  No t_* schemas found locally — tenant export skipped."
  touch "$TENANT_SCHEMA_DUMP" "$TENANT_DUMP"
else
  echo "  Schemas: ${TENANT_SCHEMAS[*]}"
  SCHEMA_ARGS=()
  for schema in "${TENANT_SCHEMAS[@]}"; do
    SCHEMA_ARGS+=(--schema="$schema")
  done
  pg_dump "$LOCAL_TENANT_URI" -Fc --schema-only --no-owner --no-privileges \
    "${SCHEMA_ARGS[@]}" -f "$TENANT_SCHEMA_DUMP"
  echo "  → $TENANT_SCHEMA_DUMP"
  pg_dump "$LOCAL_TENANT_URI" -Fc --data-only --disable-triggers \
    "${SCHEMA_ARGS[@]}" -f "$TENANT_DUMP"
  echo "  → $TENANT_DUMP"
fi

if [[ "$DRY_RUN" == "--dry-run" ]]; then
  echo
  echo "Dry run — dumps created, skipping restore."
  echo "Cloud URIs use --admin (required for restore)."
  exit 0
fi

read -r -p "Restore into Encore Cloud env '$ENV_NAME'? This OVERWRITES cloud data. [y/N] " confirm
if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
  echo "Aborted. Dumps kept in $BACKUP_DIR"
  exit 1
fi

CLOUD_SYSTEM_TABLES="$(psql "$CLOUD_SYSTEM_URI" -tAc "SELECT count(*) FROM pg_tables WHERE schemaname='public'" 2>/dev/null || echo 0)"
if [[ "$CLOUD_SYSTEM_TABLES" == "0" ]]; then
  echo "  Cloud system DB has no tables — will restore schema (DDL) from local first."
  echo "[3/6] Restore system schema (DDL) to cloud..."
  pg_restore_tolerant "system schema" -d "$CLOUD_SYSTEM_URI" --schema-only --no-owner --no-privileges "$SYSTEM_SCHEMA_DUMP"
else
  echo "  Cloud system DB has $CLOUD_SYSTEM_TABLES table(s) — skip schema DDL (avoids 'public already exists')."
  echo "[3/6] Skipped system schema restore."
fi

echo "[4/6] Restore system data to cloud..."
pg_restore_tolerant "system data" -d "$CLOUD_SYSTEM_URI" --data-only --no-owner --no-privileges "$SYSTEM_DUMP"

LOCAL_TENANT_ROWS="$(psql "$LOCAL_SYSTEM_URI" -tAc "SELECT count(*) FROM tenant" 2>/dev/null || echo 0)"
CLOUD_TENANT_ROWS="$(psql "$CLOUD_SYSTEM_URI" -tAc "SELECT count(*) FROM tenant" 2>/dev/null || echo 0)"
if [[ "$LOCAL_TENANT_ROWS" != "$CLOUD_TENANT_ROWS" ]]; then
  echo "ERROR: system DB tenant row count mismatch (local=$LOCAL_TENANT_ROWS cloud=$CLOUD_TENANT_ROWS)." >&2
  echo "  Check pg_restore output above; re-run after fixing cloud schema." >&2
  exit 1
fi
echo "  ok system tenant rows: $CLOUD_TENANT_ROWS"

if [[ ${#TENANT_SCHEMAS[@]} -gt 0 ]]; then
  CLOUD_TENANT_SCHEMAS="$(psql "$CLOUD_TENANT_URI" -tAc "SELECT count(*) FROM pg_namespace WHERE nspname ~ '^t_'" 2>/dev/null || echo 0)"
  if [[ "$CLOUD_TENANT_SCHEMAS" -lt "${#TENANT_SCHEMAS[@]}" ]]; then
    echo "[5/6] Restore tenant schemas (DDL) to cloud ($CLOUD_TENANT_SCHEMAS/${#TENANT_SCHEMAS[@]} present)..."
    pg_restore_tolerant "tenant schema" -d "$CLOUD_TENANT_URI" --schema-only --no-owner --no-privileges "$TENANT_SCHEMA_DUMP"
  else
    echo "[5/6] Skipped tenant schema restore (${CLOUD_TENANT_SCHEMAS} t_* schemas already on cloud)."
  fi

  echo "[6/6] Restore tenant data to cloud..."
  pg_restore_tolerant "tenant data" -d "$CLOUD_TENANT_URI" --data-only --no-owner --no-privileges "$TENANT_DUMP"

else
  echo "[5/6] Skipped — no tenant schemas."
fi

echo "  Fixing ownership + GRANTs (encore-migrator)..."
"$ROOT/scripts/fix-cloud-db-grants.sh" "$ENV_NAME"
"$ROOT/scripts/verify-cloud-deploy-ready.sh" "$ENV_NAME"

echo
echo "Done. Verify with:"
echo "  psql \"\$(encore db conn-uri system --env=$ENV_NAME --admin)\" -c 'SELECT count(*) FROM tenant;'"
echo "  psql \"\$(encore db conn-uri tenant --env=$ENV_NAME --admin)\" -c \"SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_';\""
echo
echo "Redis was not migrated — ask users to log in again."
