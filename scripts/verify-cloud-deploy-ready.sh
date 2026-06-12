#!/usr/bin/env bash
# Pre-deploy check: DB ownership compatible with Encore Cloud dynamic grants.
#
# Usage: ./scripts/verify-cloud-deploy-ready.sh staging
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env> (e.g. staging)}"
cd "$ROOT"

SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
TENANT_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"

SYSTEM_OWNER="$(psql "$SYSTEM_URI" -tAc "
  SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = current_database()" | tr -d '[:space:]')"
TENANT_OWNER="$(psql "$TENANT_URI" -tAc "
  SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = current_database()" | tr -d '[:space:]')"

echo "=== Encore deploy readiness ($ENV_NAME) ==="
echo "System DB owner expected: $SYSTEM_OWNER"
echo "Tenant DB owner expected: $TENANT_OWNER"
echo

fail=0

sm_owner="$(psql "$SYSTEM_URI" -tAc "
  SELECT tableowner FROM pg_tables
  WHERE schemaname = 'public' AND tablename = 'schema_migrations' LIMIT 1" | tr -d '[:space:]')"
if [[ -z "$sm_owner" ]]; then
  echo "WARN: public.schema_migrations not found (fresh DB — OK on first deploy)"
elif [[ "$sm_owner" != "$SYSTEM_OWNER" ]]; then
  echo "FAIL: schema_migrations owner=$sm_owner (want $SYSTEM_OWNER)"
  fail=1
else
  echo "OK: schema_migrations owner=$sm_owner"
fi

registered=()
while IFS= read -r line; do
  line="$(echo "$line" | tr -d '[:space:]')"
  [[ -z "$line" ]] && continue
  registered+=("$line")
done < <(psql "$SYSTEM_URI" -tAc "
  SELECT tc.schema_name
  FROM tenant_company tc
  JOIN tenant t ON t.id = tc.tenant_id
  WHERE t.deleted_at IS NULL
    AND tc.schema_name IS NOT NULL AND tc.schema_name <> ''")

bad_registered=()
for s in "${registered[@]}"; do
  owner="$(psql "$TENANT_URI" -tAc "SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname = '$s'" | tr -d '[:space:]')"
  usage="$(psql "$TENANT_URI" -tAc "SELECT has_schema_privilege('$TENANT_OWNER', '$s', 'USAGE')" | tr -d '[:space:]')"
  if [[ -z "$owner" ]]; then
    bad_registered+=("$s (missing)")
  elif [[ "$usage" != "t" ]]; then
    bad_registered+=("$s (owner=$owner, no USAGE for $TENANT_OWNER)")
  elif [[ "$owner" != "$TENANT_OWNER" ]]; then
    echo "WARN: $s owner=$owner (prefer $TENANT_OWNER) — OK if $TENANT_OWNER has USAGE"
  fi
done
if [[ ${#bad_registered[@]} -gt 0 ]]; then
  echo "FAIL: registered schemas not deploy-ready: ${bad_registered[*]}"
  fail=1
else
  echo "OK: ${#registered[@]} registered tenant schema(s) deploy-ready"
fi

orphan_list=()
all_schemas=()
while IFS= read -r line; do
  line="$(echo "$line" | tr -d '[:space:]')"
  [[ -z "$line" ]] && continue
  all_schemas+=("$line")
done < <(psql "$TENANT_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_'")
for s in "${all_schemas[@]}"; do
  found=0
  for r in "${registered[@]}"; do
    [[ "$s" == "$r" ]] && found=1 && break
  done
  [[ "$found" -eq 0 ]] && orphan_list+=("$s")
done
if [[ ${#orphan_list[@]} -gt 0 ]]; then
  echo "WARN: orphan schemas (not in tenant_company): ${orphan_list[*]}"
  echo "      Drop orphans in Encore SQL console if deploy fails on them"
fi

echo
if [[ "$fail" -ne 0 ]]; then
  echo "Run: ./scripts/fix-cloud-db-grants.sh $ENV_NAME" >&2
  exit 1
fi
echo "DB ready for Encore deploy."
