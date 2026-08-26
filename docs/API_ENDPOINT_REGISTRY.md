# API Endpoint Registry

Inventaris **336 endpoint** Encore (`//encore:api`) dengan regression struktural cepat (`go test`, tanpa HTTP).

## Kenapa bukan regression HTTP per endpoint?

| Aspek | AI buyerflow (`internal/buyerflow`) | Semua endpoint HTTP |
|-------|-------------------------------------|---------------------|
| Jumlah | ~10 golden cases | **336 route** |
| Infra | Pure Go | Auth JWT, tenant schema, Redis, WA, Midtrans, … |
| Waktu | <1 detik | Menit–jam (fixtures per domain) |
| PR scope | Satu concern | Epic multi-PR per modul |

**Kesimpulan:** regression **perilaku** untuk 336 endpoint = project terpisah (per-domain integration tests), bukan satu PR.

Yang kita punya sekarang (production-ready):

1. **`internal/apiregistry`** — scan `//encore:api`, cek duplikat, golden snapshot (**336 endpoint, 29 service**)
2. **`internal/buyerflow`** — regression routing AI (simulator = production FSM) + triage autogen
3. **`internal/apitest`** — HTTP smoke Encore per service (28 service produksi + `internal/apitest` ping)
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

## Menambah endpoint baru

1. Tambah `//encore:api ...` di handler Go
2. Update snapshot:

```bash
go run scripts/gen-api-catalog.go
```

3. Commit `catalog_snapshot.json` bersama handler

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

Encore MCP `api_describe` (staging) bisa dipakai untuk cross-check deploy vs snapshot — opsional di CI nightly.
