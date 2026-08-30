#!/usr/bin/env bash
# Smoke test RAG retrieval di local — tanpa WA.
# Usage:
#   ./scripts/rag-local-smoke.sh          # unit + eval tests saja
#   ./scripts/rag-local-smoke.sh --live   # + cek secrets Encore untuk Pinecone/OpenAI
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "== RAG unit tests (MockEmbedder, no network) =="
encore test ./shared/retrieval/... ./kb/... ./internal/buyerflow/...

if [[ "${1:-}" == "--live" ]]; then
  echo ""
  echo "== Encore secrets (local) =="
  for s in OpenAIApiKey PineconeApiKey PineconeIndexHost; do
    if encore secret list 2>&1 | grep -F "$s" | grep -q '✓'; then
      echo "  ok  $s (local configured)"
    else
      echo "  MISSING $s — jalankan: ./scripts/setup-secrets-from-env.sh" >&2
      exit 1
    fi
  done
  echo ""
  echo "Secrets OK. Untuk indexing live:"
  echo "  1. encore run"
  echo "  2. Buat FAQ di Knowledge Base (owner)"
  echo "  3. POST /api/v1/knowledge-base/reindex"
  echo "  4. Cek log: retrieval-index worker + embedding_status=indexed"
fi

echo ""
echo "== Manual E2E (setelah encore run) =="
cat <<'EOF'
1. Reset DB jika migrasi bentrok (mis. codesim v21):
     encore db reset --all

2. Sync secrets dari api/.env:
     ./scripts/setup-secrets-from-env.sh

3. Jalankan API:
     encore run

4. Login owner → buat 2–3 FAQ di Knowledge Base

5. Trigger backfill:
     curl -X POST http://localhost:4000/api/v1/knowledge-base/reindex \
       -H "Authorization: Bearer <JWT>" -H "Content-Type: application/json" \
       -d '{"batchSize":50}'

6. Aktifkan retrieval (super_admin → POST /api/v1/flags):
     ai_retrieval_mode_shadow  → log vector, respons tetap lexical
     ai_retrieval_mode_vector  → RRF mempengaruhi FAQ direct & prompt

7. Uji chat WA atau simulator — pantau log:
     retrieval shadow | retrieval query | retrieval KB failed
EOF
