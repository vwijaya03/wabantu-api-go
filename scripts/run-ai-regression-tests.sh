#!/usr/bin/env bash
# Loop regresi AI percakapan — cepat, tanpa WABANTU_AI_INTEGRATION.
# Tambah skenario baru di ai/conversation_regression_test.go setelah bug WA ditemukan.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "Checking Encore test Postgres cluster..."
./scripts/fix-encore-test-db.sh

echo ""
echo "Running AI conversation regression loop..."
encore test ./ai/ -run 'TestConversationRegression|TestConversationRegressionAutoGen|TestTryPaymentFAQAnswer|TestIsOrderRefStatusLookup|TestIsPaymentProofInbound' -count=1 "$@"
