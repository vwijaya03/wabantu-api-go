#!/usr/bin/env bash
# Loop regresi AI percakapan — cepat, tanpa WABANTU_AI_INTEGRATION.
# Tambah skenario baru di ai/conversation_regression_test.go setelah bug WA ditemukan.
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ "${GITHUB_ACTIONS:-}" != "true" ]]; then
  echo "Checking Encore test Postgres cluster..."
  ./scripts/fix-encore-test-db.sh
else
  echo "CI: skip fix-encore-test-db (runner baru, cluster dibuat oleh encore test)."
fi

echo ""
echo "Running AI conversation regression loop..."
encore test ./ai/ -run 'TestConversationRegression|TestConversationRegressionAutoGen|TestTryPaymentFAQAnswer|TestIsOrderRefStatusLookup|TestIsPaymentProofInbound' -count=1 "$@"
