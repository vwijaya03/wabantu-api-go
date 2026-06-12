#!/usr/bin/env bash
# Drop tenant schemas t_* that have no row in system.tenant (orphans from old migrations).
# Orphans block Encore deploy dynamic grants if ownership is broken.
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
TENANT_URI="$(encore db conn-uri tenant --env="$ENV_NAME" --admin)"

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
done < <(psql "$TENANT_URI" -tAc "SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1")

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
printf '  %s\n' "${orphans[@]}"

if [[ "$APPLY" != "--apply" ]]; then
  echo
  echo "Dry-run only. To drop: $0 $ENV_NAME --apply"
  exit 0
fi

echo
read -r -p "DROP ${#orphans[@]} orphan schema(s)? [y/N] " ans
[[ "$ans" == "y" || "$ans" == "Y" ]] || exit 0

for s in "${orphans[@]}"; do
  echo "  DROP SCHEMA $s CASCADE"
  psql "$TENANT_URI" -v ON_ERROR_STOP=1 -c "DROP SCHEMA IF EXISTS \"$s\" CASCADE;"
done

echo "Done. Run ./scripts/fix-cloud-db-grants.sh $ENV_NAME && ./scripts/verify-cloud-deploy-ready.sh $ENV_NAME"
