#!/usr/bin/env bash
# Immutable TestBehavior_* only — used by ai-triage-behavior-fix.yml.
# Prove RED for Composer self-heal (`TestBehavior_`). Do not filter TestRegressionAutoGen.
set -euo pipefail
cd "$(dirname "$0")/.."

shopt -s nullglob
files=(internal/buyerflow/regression_behavior_*_test.go)
if [ ${#files[@]} -eq 0 ]; then
  echo "triage-behavior-tests: no regression_behavior_*_test.go — apply step failed?" >&2
  exit 1
fi
if ! grep -q 'func TestBehavior_' "${files[@]}"; then
  echo "triage-behavior-tests: generated file missing func TestBehavior_" >&2
  exit 1
fi

echo "Running generated AI triage behavior tests..."
go test ./internal/buyerflow/ -run '^TestBehavior_' -count=1 "$@"
