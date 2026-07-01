#!/usr/bin/env bash
# Run cloud DDL scripts in waves (used by GitHub Actions and local ops).
#
# Usage:
#   ./scripts/run-cloud-ddl-waves.sh staging [script] [limit] [cursor] [run_all_waves]
#
# script: tenant | inventory | all (default all)
# run_all_waves: true | false (default true)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

ENV_NAME="${1:?Usage: $0 <encore-env> [script] [limit] [cursor] [run_all_waves]}"
SCRIPT="${2:-all}"
LIMIT="${3:-1000}"
CURSOR="${4:-0}"
RUN_ALL="${5:-true}"

run_tenant_wave() {
  local cursor="$1"
  ./scripts/apply-tenant-schema-cloud.sh "$ENV_NAME" --limit "$LIMIT" --cursor "$cursor"
}

run_inventory() {
  ./scripts/apply-inventory-schema-cloud.sh "$ENV_NAME"
}

apply_tenant_waves() {
  local cursor="$CURSOR"
  if [[ "$RUN_ALL" != "true" ]]; then
    run_tenant_wave "$cursor"
    return 0
  fi

  while true; do
    local out
    if ! out="$(run_tenant_wave "$cursor" 2>&1)"; then
      echo "$out" >&2
      return 1
    fi
    echo "$out"
    if echo "$out" | grep -q "No t_* schemas in this batch"; then
      break
    fi
    local next
    next="$(echo "$out" | grep "Next cursor:" | awk '{print $NF}' || true)"
    if [[ -z "$next" ]]; then
      break
    fi
    cursor="$next"
  done
}

echo "=== Cloud DDL waves ($ENV_NAME) script=$SCRIPT limit=$LIMIT cursor=$CURSOR run_all=$RUN_ALL ==="

case "$SCRIPT" in
  tenant)
    apply_tenant_waves
    ;;
  inventory)
    run_inventory
    ;;
  all)
    apply_tenant_waves
    run_inventory
    ;;
  *)
    echo "ERROR: unknown script '$SCRIPT' (tenant|inventory|all)" >&2
    exit 1
    ;;
esac

echo "=== Cloud DDL waves done ==="
