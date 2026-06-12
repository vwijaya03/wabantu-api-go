#!/usr/bin/env bash
# Re-apply Postgres ownership + GRANTs after migrate-local-db-to-encore.sh.
#
# Without this:
#   - login returns {"message":"db error"}
#   - Encore deploy fails: permission denied for schema t_* (dynamic grants)
#
# Usage: ./scripts/fix-cloud-db-grants.sh staging
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env-name> (e.g. staging)}"
cd "$ROOT"

SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
TENANT_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"

ADMIN_ROLE="$(psql "$TENANT_URI" -tAc "SELECT current_user" | tr -d '[:space:]')"
if [[ -z "$ADMIN_ROLE" ]]; then
  echo "ERROR: could not resolve admin DB role" >&2
  exit 1
fi
echo "Using admin role: $ADMIN_ROLE"

echo "GRANT system ($ENV_NAME)..."
psql "$SYSTEM_URI" -v ON_ERROR_STOP=1 <<'SQL'
GRANT USAGE ON SCHEMA public TO encore_writer, encore_reader, encore_services;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO encore_writer, encore_services;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO encore_reader;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO encore_writer, encore_services;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO encore_reader;
SQL

echo "Fix tenant schema ownership + GRANTs ($ENV_NAME)..."
psql "$TENANT_URI" -v ON_ERROR_STOP=1 <<'SQL'
DO $$
DECLARE s text;
BEGIN
  FOR s IN SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1 LOOP
    -- pg_restore --no-owner leaves schemas owned by a role Encore deploy cannot GRANT on.
    EXECUTE format('ALTER SCHEMA %I OWNER TO %I', s, current_user);

    EXECUTE format('GRANT USAGE, CREATE ON SCHEMA %I TO encore_writer, encore_reader, encore_services', s);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO encore_writer, encore_services', s);
    EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA %I TO encore_reader', s);
    EXECUTE format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %I TO encore_writer, encore_services', s);
    EXECUTE format('GRANT SELECT ON ALL SEQUENCES IN SCHEMA %I TO encore_reader', s);

    EXECUTE format(
      'ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO encore_writer, encore_services',
      s);
    EXECUTE format(
      'ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT SELECT ON TABLES TO encore_reader',
      s);
  END LOOP;
END $$;
SQL

echo
echo "Schema owners (verify all t_* → $ADMIN_ROLE):"
psql "$TENANT_URI" -c "
  SELECT nspname AS schema, pg_get_userbyid(nspowner) AS owner
  FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1;"

echo "Done. Retry Encore deploy / login."
