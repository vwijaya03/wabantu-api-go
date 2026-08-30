#!/usr/bin/env bash
# Apply pending system DB migrations on Encore Cloud (requires --admin).
#
# Encore deploy migrator fails with SQLSTATE 42501 when public tables (e.g.
# tenant_company) are not owned by the database owner role after pg_restore.
#
# Usage:
#   ./scripts/apply-system-schema-cloud.sh staging
#
# Always run before Encore deploy on cloud if verify-cloud-deploy-ready.sh fails.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env> (e.g. staging)}"
cd "$ROOT"

echo "=== Step 1: fix system DB table ownership ==="
"$ROOT/scripts/fix-cloud-db-grants.sh" "$ENV_NAME"

ADMIN_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
SYSTEM_OWNER="$(psql "$ADMIN_URI" -tAc "
  SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = current_database()" | tr -d '[:space:]')"

echo ""
echo "=== Step 2: apply system schema migrations ($ENV_NAME) owner=$SYSTEM_OWNER ==="

# Clear dirty flags left by failed Encore deploy attempts.
psql "$ADMIN_URI" -v ON_ERROR_STOP=1 -c "
  UPDATE schema_migrations SET dirty = false WHERE dirty = true;"

MIGRATIONS=(
  "6:system/migrations/6_tenant_schema_migrated_at.up.sql"
  "7:system/migrations/7_tenant_schema_patch_version.up.sql"
  "8:system/migrations/8_tenant_schema_migration_jobs.up.sql"
  "9:system/migrations/9_tenant_schema_migration_job_lane.up.sql"
  "10:system/migrations/10_ai_triage_job.up.sql"
  "11:system/migrations/11_ai_triage_anomaly.up.sql"
  "12:system/migrations/12_ai_triage_llm_scan.up.sql"
  "13:system/migrations/13_ai_triage_report.up.sql"
  "14:system/migrations/14_tenant_access_consent.up.sql"
  "15:system/migrations/15_rag_rollout_jobs.up.sql"
)

for entry in "${MIGRATIONS[@]}"; do
  version="${entry%%:*}"
  file="${entry#*:}"
  if [[ ! -f "$file" ]]; then
    echo "ERROR: missing $file" >&2
    exit 1
  fi

  applied="$(psql "$ADMIN_URI" -tAc "
    SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $version AND dirty = false)" | tr -d '[:space:]')"
  if [[ "$applied" == "t" ]]; then
    echo "  -> migration $version already recorded — skip"
    continue
  fi

  echo "  -> migration $version ($file)"
  psql "$ADMIN_URI" -v ON_ERROR_STOP=1 -f "$file"
  psql "$ADMIN_URI" -v ON_ERROR_STOP=1 -c "
    INSERT INTO schema_migrations (version, dirty) VALUES ($version, false)
    ON CONFLICT (version) DO UPDATE SET dirty = false;"
done

echo ""
echo "=== Step 3: GRANT runtime roles on new public tables ==="
psql "$ADMIN_URI" -v ON_ERROR_STOP=1 -c "
  GRANT USAGE ON SCHEMA public TO encore_writer, encore_reader, encore_services;
  GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO encore_writer, encore_services;
  GRANT SELECT ON ALL TABLES IN SCHEMA public TO encore_reader;
  GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO encore_writer, encore_services;
  GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO encore_reader;"

echo ""
echo "Done. Verify: ./scripts/verify-cloud-deploy-ready.sh $ENV_NAME"
