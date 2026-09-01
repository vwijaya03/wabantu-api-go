# API HTTP Smoke Tests

Encore integration smoke untuk **30 service / 345 endpoint** — bootstrap tenant + auth, panggil handler typed API (`et.OverrideAuthInfo`).

> **Perbedaan dengan `internal/apiregistry`:** registry struktural memakai golden **342 endpoint / 29 service** (`go test ./internal/apiregistry/`). Snapshot smoke ini sedikit lebih luas karena memindai anotasi tambahan — lihat [docs/API_ENDPOINT_REGISTRY.md](../../docs/API_ENDPOINT_REGISTRY.md).

## Prasyarat

| Komponen | Cara cek | Catatan |
|----------|----------|---------|
| **Encore CLI** | `encore version` | Wajib untuk `encore test` |
| **Docker** | `docker info` | Postgres test cluster + Redis |
| **Postgres test** | otomatis via Encore | `./scripts/fix-encore-test-db.sh` jika role `encore-migrator` hilang |
| **Redis** | `redis-cli -h 127.0.0.1 ping` → `PONG` | Wajib untuk smoke **auth JWT/session**; typed API smokes pakai `et.OverrideAuthInfo` tanpa Redis |
| **Secret lokal** | `encore secret list` → `RedisURL` | Set ke `redis://127.0.0.1:6379` (hindari `localhost` → IPv6 `[::1]` refused) |

```bash
# Redis via script smoke (auto-start container wabantu-apitest-redis)
./scripts/ensure-encore-test-redis.sh

# Atau infra shared
cd ../infra && docker compose up -d redis

# Set secret sekali per mesin
printf '%s' 'redis://127.0.0.1:6379' | encore secret set --type local RedisURL
```

## Menjalankan

```bash
./scripts/run-api-smoke-tests.sh
# atau
encore test ./internal/apitest/ -count=1 -v
```

Skip DB/Redis: `encore test ./internal/apitest/ -short`

## Cakupan endpoint (345)

Katalog endpoint di-generate dari `//encore:api`:

```bash
./scripts/gen-apiregistry-catalog.sh   # → internal/apitest/catalog_snapshot.json
```

`TestCatalog_*` memverifikasi inventori + peta coverage per service. Konstanta golden: `expectedEndpointCount = 345` di `catalog_smoke_test.go`.

Phase:

| Phase | Status | Service |
|-------|--------|---------|
| 1 | covered (smoke ada) | health, auth, order, inbox, inventory, finance, events, billing, branch, leads, analytics, business, kb, usage, webhook, payment (webhook), **flag** (termasuk `/flags/retrieval-*`), shipping, notification, whatsappapi, ai, broadcast, tenantaccess, tenant, **admin, audit, workflow, importcsv** |
| 2 | pending | — |
| 3 | pending | — |

**RAG:** endpoint retrieval ada di service `flag` (12 endpoint total di registry). Smoke `flag` saat ini memverifikasi handler dasar; rollout/indexing super_admin belum punya smoke HTTP penuh (butuh `BootstrapSuperAdmin` + fixture tenant).

Regenerasi katalog setelah menambah endpoint baru, lalu perluas smoke per service (satu GET/list handler per service minimum).

## Menambah smoke baru

1. `BootstrapOwner(t)` + `WithOwnerAuth(fx)` — atau `BootstrapSuperAdmin` untuk route `super_admin`
2. Panggil handler Encore langsung (bukan raw `//encore:api` — gunakan `Serve*HTTP` di package target)
3. `AssertJSONFields` / `AssertJSONArrayField`
4. Update `serviceSmokePhase` di `catalog_smoke_test.go`
5. Update `expectedEndpointCount` jika jumlah route berubah

Raw handler wrappers: `auth/http_testutil.go`, `webhook/http_testutil.go`, `payment/http_testutil.go`.

## Service yang butuh fixture khusus

| Service | Pendekatan smoke | Catatan |
|---------|------------------|---------|
| **admin** | `BootstrapSuperAdmin` + `WithSuperAdminAuth` → `admin.ListTenants` | `requireSuperAdmin` cek `auth.Data().Role == super_admin`; impersonation belum di-smoke |
| **audit** | `BootstrapSuperAdmin` + `WithSuperAdminAuth` → `audit.ListAuditLogs` | `RecordAudit` private — tidak di-smoke HTTP |
| **flag** | `BootstrapOwner` + `WithOwnerAuth` → `flag.ListFlags` | Route `/flags/retrieval-*` butuh `super_admin` — smoke penuh pending |
| **workflow** | `ensureWorkflowRuleTable` (DDL minimal, tanpa `pg_trgm`) → `workflow.ListRules` | Full `RunSchemaPatches` gagal di Encore test cluster (`gin_trgm_ops` missing) |
| **importcsv** | `ImportJobStatus` (NotFound) + unit `parseCSV`/`suggestMapping` di `importcsv/import_test.go` | `Preview` multipart via typed API gagal (`FileHeader.Open`); `Execute` + Pub/Sub belum di-smoke |

Fixture super admin: `super_admin_fixture.go` (`BootstrapSuperAdmin`, `WithSuperAdminAuth`).

## Encore Cloud build (`encore test ./...`)

Deploy staging/production menjalankan **semua** package test termasuk `internal/apitest`. Encore Cloud **tidak** menyediakan Redis di `127.0.0.1:6379`.

- Typed API smokes (`BootstrapOwner` + `WithOwnerAuth`) **tidak butuh Redis** — lulus di build cloud.
- Auth JWT smokes (`RequireRedis`, `BootstrapOwnerWithToken`) **di-skip** otomatis bila Redis tidak reachable.
- Smoke penuh (termasuk auth login/me) jalan di CI `api-smoke.yml` atau lokal via `./scripts/run-api-smoke-tests.sh`.

## CI

`.github/workflows/api-smoke.yml` — PR ke `master` saat `internal/apitest/**` berubah. CI mem-start Redis (`127.0.0.1:6379`) dan set secret `RedisURL` sebelum `encore test`.
