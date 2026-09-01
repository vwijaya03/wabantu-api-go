# RAG — Outbox & Indexing Reliability

## Masalah / Kebutuhan

Indexing vector harus eventually consistent dengan PostgreSQL tanpa race out-of-order atau kehilangan event saat crash setelah commit.

## Perubahan

- Tabel `retrieval_outbox` + kolom `embedding_*` di `knowledge_base_entry` dan `business_catalog_item`
- Vector ID deterministik `kb:{id}:v{version}:c{chunk}`
- Worker idempotent dengan cek `embedding_version`
- Retry/DLQ (`MaxIndexAttempts=8`)

## File utama

- `tenant/schema_patch_retrieval.go`
- `shared/retrieval/retry.go`, `indexer.go`
- `kb/retrieval_outbox.go`, `kb/retrieval_worker.go`

## Testing

`encore test ./shared/retrieval/... ./kb/...`

## Catatan deploy

Jalankan migrasi tenant (`schema_patch_version` → 2). Secrets Pinecone/OpenAI wajib untuk indexing produksi.
