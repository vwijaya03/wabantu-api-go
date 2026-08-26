#!/usr/bin/env bash
# Regression auto-gen only — used by ai-triage-fix workflow (not full golden suite).
set -euo pipefail
cd "$(dirname "$0")/.."

echo "Running auto-generated AI triage regression..."
go test ./internal/buyerflow/ -run TestRegressionAutoGen -count=1 "$@"
