# RAG — Catalog Semantic Search

## Masalah / Kebutuhan

Matching katalog lexical saja kurang untuk sinonim; vector tidak boleh menebak SKU.

## Perubahan

- Indexing katalog via outbox (`business/catalog_retrieval.go`)
- `internal/buyerflow/catalog_vector.go` — semantic + ambiguity guard
- Worker katalog di `kb/retrieval_worker_catalog.go`

## Testing

`encore test ./internal/buyerflow/...`

## Catatan deploy

Sama seperti KB: reindex katalog setelah secrets siap; rollout flag mengikuti KB.
