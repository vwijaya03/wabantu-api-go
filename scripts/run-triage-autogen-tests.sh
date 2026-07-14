#!/usr/bin/env bash
# Regression auto-gen only — used by ai-triage-fix workflow (not full golden suite).
set -euo pipefail
cd "$(dirname "$0")/.."

echo "Checking Encore test Postgres cluster..."
./scripts/fix-encore-test-db.sh

echo ""
echo "Running auto-generated AI triage regression..."
encore test ./ai/ -run TestConversationRegressionAutoGen -count=1 "$@"
