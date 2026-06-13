#!/usr/bin/env bash
# Jalankan unit test paket ai (butuh Encore daemon + Postgres test cluster).
set -euo pipefail
cd "$(dirname "$0")/.."

echo "Checking Encore test Postgres cluster..."
./scripts/fix-encore-test-db.sh

echo ""
echo "Running ai package tests via encore test..."
encore test ./ai/ -count=1 "$@"
