#!/usr/bin/env bash
# Jalankan suite AI berat (1000/2000+ skenario) — TIDAK dijalankan saat Encore Cloud build.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "Checking Encore test Postgres cluster..."
./scripts/fix-encore-test-db.sh

echo ""
echo "Running AI integration tests (WABANTU_AI_INTEGRATION=1)..."
WABANTU_AI_INTEGRATION=1 encore test ./ai/ -count=1 "$@"
