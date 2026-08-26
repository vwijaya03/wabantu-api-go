#!/usr/bin/env bash
# HTTP smoke tests per domain (health, auth, order, inbox, inventory, finance).
# Requires Encore CLI + Docker (test Postgres + Redis). Skips with -short.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "Checking Encore test Postgres cluster..."
./scripts/fix-encore-test-db.sh

echo ""
echo "Checking Redis for auth sessions..."
./scripts/ensure-encore-test-redis.sh

echo ""
echo "Running API smoke tests (encore test ./internal/apitest/)..."
encore test ./internal/apitest/ -count=1 "$@"
