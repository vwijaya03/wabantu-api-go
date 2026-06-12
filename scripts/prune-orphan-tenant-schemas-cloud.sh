#!/usr/bin/env bash
# Drop tenant schemas t_* not registered in tenant_company (orphans block Encore deploy).
#
# Orphans owned by encore_container / encore_writer cannot be dropped with --admin.
# This script uses --superuser (encore_superuser_*).
#
# Usage:
#   ./scripts/prune-orphan-tenant-schemas-cloud.sh staging          # dry-run
#   ./scripts/prune-orphan-tenant-schemas-cloud.sh staging --apply
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env> [--apply]}"
APPLY="${2:-}"
cd "$ROOT"

SYSTEM_URI="$(encore db conn-uri system --env="$ENV_NAME" --admin)"
TENANT_ADMIN_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"
TENANT_SUPER_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --superuser)"

echo "=== Orphan tenant schemas ($ENV_NAME) ==="

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
    AND tc.schema_name IS NOT NULL AND tc.schema_name <> ''
  ORDER BY 1")

all_schemas=()
while IFS= read -r line; do
  line="$(echo "$line" | tr -d '[:space:]')"
  [[ -z "$line" ]] && continue
  all_schemas+=("$line")
done < <(psql "$TENANT_ADMIN_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1")

orphans=()
for s in "${all_schemas[@]}"; do
  found=0
  for r in "${registered[@]}"; do
    [[ "$s" == "$r" ]] && found=1 && break
  done
  [[ "$found" -eq 0 ]] && orphans+=("$s")
done

if [[ ${#orphans[@]} -eq 0 ]]; then
  echo "No orphan schemas."
  exit 0
fi

echo "Orphan schemas (in tenant DB but not tenant_company):"
for s in "${orphans[@]}"; do
  owner="$(psql "$TENANT_ADMIN_URI" -tAc "SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname = '$s'" | tr -d '[:space:]')"
  echo "  $s (owner: $owner)"
done

if [[ "$APPLY" != "--apply" ]]; then
  echo
  echo "Dry-run only. To drop (requires --superuser): $0 $ENV_NAME --apply"
  exit 0
fi

echo
read -r -p "DROP ${#orphans[@]} orphan schema(s) via superuser? [y/N] " ans
[[ "$ans" == "y" || "$ans" == "Y" ]] || exit 0

for s in "${orphans[@]}"; do
  echo "  DROP SCHEMA $s CASCADE"
  psql "$TENANT_SUPER_URI" -v ON_ERROR_STOP=1 -c "DROP SCHEMA IF EXISTS \"$s\" CASCADE;"
done

echo
echo "Done. Run: ./scripts/fix-cloud-db-grants.sh $ENV_NAME && ./scripts/verify-cloud-deploy-ready.sh $ENV_NAME"
