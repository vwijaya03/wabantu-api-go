#!/usr/bin/env bash
# Loop regresi AI percakapan — pure Go, tanpa Encore/Postgres.
# Tambah skenario baru di internal/buyerflow/regression_cases.go
set -euo pipefail
cd "$(dirname "$0")/.."

echo "Running AI conversation regression (go test ./internal/buyerflow/)..."
go test ./internal/buyerflow/ -run 'TestRegression|TestRegressionScript|TestRegressionShippingScript|TestRegressionOrderRevisionScript|TestRegressionAutoGen|TestTryPaymentFAQAnswer|TestIsOrderRefStatusLookup' -count=1 "$@"

echo ""
echo "Running AI pure-Go subset (encore test ./ai/)..."
encore test ./ai/ -run 'Order|Catalog|Greeting|Ground|Structured|Amend' -count=1 "$@"

echo ""
echo "Running retrieval unit tests (go test ./shared/retrieval/)..."
go test ./shared/retrieval/ -count=1 "$@"

echo ""
echo "Running API endpoint registry (go test ./internal/apiregistry/)..."
go test ./internal/apiregistry/ -count=1 "$@"
