#!/usr/bin/env bash
# Re-apply Postgres GRANTs after migrate-local-db-to-encore.sh (pg_restore --no-privileges).
# Without this, login returns {"message":"db error"} and tenant APIs fail.
#
# Usage: ./scripts/fix-cloud-db-grants.sh staging
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env-name> (e.g. staging)}"
cd "$ROOT"

SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
TENANT_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"

echo "GRANT system ($ENV_NAME)..."
psql "$SYSTEM_URI" -v ON_ERROR_STOP=1 <<'SQL'
GRANT USAGE ON SCHEMA public TO encore_writer, encore_reader, encore_services;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO encore_writer, encore_services;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO encore_reader;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO encore_writer, encore_services;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO encore_reader;
SQL

echo "GRANT tenant t_* ($ENV_NAME)..."
psql "$TENANT_URI" -v ON_ERROR_STOP=1 <<'SQL'
DO $$
DECLARE s text;
BEGIN
  FOR s IN SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' LOOP
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO encore_writer, encore_reader, encore_services', s);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO encore_writer, encore_services', s);
    EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA %I TO encore_reader', s);
    EXECUTE format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %I TO encore_writer, encore_services', s);
    EXECUTE format('GRANT SELECT ON ALL SEQUENCES IN SCHEMA %I TO encore_reader', s);
  END LOOP;
END $$;
SQL

echo "Done. Retry login."
