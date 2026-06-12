#!/usr/bin/env bash
# Jalankan semua unit test paket ai (butuh encore run environment / DB lokal).
set -euo pipefail
cd "$(dirname "$0")/.."
echo "Running ai package tests via encore test..."
encore test ./ai/ -count=1 "$@"
