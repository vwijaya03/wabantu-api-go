# API Endpoint Registry

Inventaris **342 endpoint** Encore (`//encore:api`) dengan regression struktural cepat (`go test`, tanpa HTTP).

> **Catatan:** katalog HTTP smoke di `internal/apitest` memindai sedikit lebih luas (**345 endpoint / 30 service**, termasuk path `internal/*` dan utilitas) — lihat [internal/apitest/README.md](../internal/apitest/README.md).

## Kenapa bukan regression HTTP per endpoint?

| Aspek | AI buyerflow (`internal/buyerflow`) | Semua endpoint HTTP |
|-------|-------------------------------------|---------------------|
| Jumlah | ~10 golden cases | **342+ route** |
| Infra | Pure Go | Auth JWT, tenant schema, Redis, WA, Midtrans, Pinecone, … |
| Waktu | <1 detik | Menit–jam (fixtures per domain) |
| PR scope | Satu concern | Epic multi-PR per modul |

**Kesimpulan:** regression **perilaku** untuk semua endpoint = project terpisah (per-domain integration tests), bukan satu PR.

Yang kita punya sekarang (production-ready):

1. **`internal/apiregistry`** — scan `//encore:api`, cek duplikat, golden snapshot (**342 endpoint, 29 service**)
2. **`internal/buyerflow`** — regression routing AI (simulator = production FSM) + triage autogen
3. **`internal/apitest`** — HTTP smoke Encore per service (**345 endpoint**, 30 service di snapshot smoke)
4. **Encore smoke (master)** — subset `encore test ./ai/` + workflow CI terpisah

## Regression per service (struktural)

`internal/apiregistry/service_regression_test.go` memvalidasi:

- Setiap service punya ≥1 endpoint
- Golden `service_counts.json` (29 service, drift = regenerate)
- Endpoint non-raw punya HTTP method
- Route publik kritis (health, auth, webhook) ada

Regenerate katalog + counts:

```bash
go run scripts/gen-api-catalog.go
```

## Endpoint baru RAG (flag service, super_admin)

| Method | Path |
|--------|------|
| GET | `/api/v1/flags/retrieval-mode/:tenantId` |
| PUT | `/api/v1/flags/retrieval-mode` |
| GET | `/api/v1/flags/retrieval-indexing/:tenantId` |
| GET | `/api/v1/flags/retrieval-observability` |
| POST | `/api/v1/flags/retrieval-rollout` |
| GET | `/api/v1/flags/retrieval-rollout/jobs/:jobId` |
| GET | `/api/v1/flags/retrieval-rollout/active-jobs` |
| POST | `/api/v1/flags/retrieval-rollout/jobs/:jobId/cancel` |

Plus `POST /api/v1/knowledge-base/reindex` (kb) untuk backfill embedding. Detail: [RAG_VECTOR_RETRIEVAL.md](./RAG_VECTOR_RETRIEVAL.md).

## Menambah endpoint baru

1. Tambah `//encore:api ...` di handler Go
2. Update snapshot:

```bash
go run scripts/gen-api-catalog.go
./scripts/gen-apiregistry-catalog.sh   # sinkronkan juga apitest smoke catalog
```

3. Commit `internal/apiregistry/catalog_snapshot.json`, `service_counts.json`, dan `internal/apitest/catalog_snapshot.json` bersama handler
4. Perbarui `expectedEndpointCount` di `internal/apitest/catalog_smoke_test.go` jika jumlah berubah

## Menjalankan registry test

```bash
go test ./internal/apiregistry/ -count=1 -v
```

## Roadmap regression HTTP (per domain)

| Fase | Modul | Contoh |
|------|-------|--------|
| R1 | `health`, `auth` | register/login/me smoke |
| R2 | `order`, `inbox` | tenant fixture + CRUD |
| R3 | `inventory`, `finance` | owner auth + schema |
| R4 | `events` | public + auth paths |
| R5 | `flag` (retrieval) | super_admin rollout smoke |

Encore MCP `api_describe` (staging) bisa dipakai untuk cross-check deploy vs snapshot — opsional di CI nightly.
