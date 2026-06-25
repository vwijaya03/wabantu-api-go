#!/usr/bin/env bash
# Unduh modul Go memakai toolchain Encore (bukan `go` sistem).
#
# Gejala:
#   encore run
#   could not find package "github.com/xuri/excelize/v2"
#
# Penyebab: parser Encore memakai encore-go (ENCORE_GOROOT) dengan resolusi modul
# terpisah. `go mod download` dari Go sistem saja tidak selalu cukup.
#
# Usage:
#   ./scripts/ensure-go-deps.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v encore >/dev/null 2>&1; then
  echo "error: encore CLI not found in PATH" >&2
  exit 1
fi

ENCORE_GOROOT="$(encore daemon env 2>/dev/null | sed -n 's/^ENCORE_GOROOT=//p' || true)"
if [[ -z "$ENCORE_GOROOT" || ! -x "$ENCORE_GOROOT/bin/go" ]]; then
  echo "error: could not locate encore-go (ENCORE_GOROOT/bin/go)" >&2
  echo "hint: run 'encore daemon' once, then retry" >&2
  exit 1
fi

ENCORE_GO="$ENCORE_GOROOT/bin/go"
echo "Using encore-go: $($ENCORE_GO version)"
echo "GOMODCACHE: $($ENCORE_GO env GOMODCACHE)"

echo "Downloading modules..."
"$ENCORE_GO" mod download

echo "Verifying excelize..."
"$ENCORE_GO" list -m github.com/xuri/excelize/v2

echo "Done. Run: encore run"
