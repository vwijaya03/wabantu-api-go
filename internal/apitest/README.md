# API HTTP Smoke Tests

Encore integration smoke untuk **28 service / 335 endpoint** — bootstrap tenant + auth, panggil handler typed API (`et.OverrideAuthInfo`).

## Prasyarat

| Komponen | Cara cek | Catatan |
|----------|----------|---------|
| **Encore CLI** | `encore version` | Wajib untuk `encore test` |
| **Docker** | `docker info` | Postgres test cluster + Redis |
| **Postgres test** | otomatis via Encore | `./scripts/fix-encore-test-db.sh` jika role `encore-migrator` hilang |
| **Redis** | `redis-cli -h 127.0.0.1 ping` → `PONG` | **Tidak** di-provision Encore; wajib untuk `BootstrapOwner` / session JWT |
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

## Cakupan endpoint (335)

Katalog endpoint di-generate dari `//encore:api`:

```bash
./scripts/gen-apiregistry-catalog.sh   # → internal/apitest/catalog_snapshot.json
```

`TestCatalog_*` memverifikasi inventori + peta coverage per service. Phase:

| Phase | Status | Service |
|-------|--------|---------|
| 1 | covered (smoke ada) | health, auth, order, inbox, inventory, finance, events, billing, branch, leads, analytics, business, kb, usage, webhook, payment (webhook), flag, shipping, notification, whatsappapi, ai, broadcast, tenantaccess, tenant, **admin, audit, workflow, importcsv** |
| 2 | pending | — |
| 3 | pending | — |

Regenerasi katalog setelah menambah endpoint baru, lalu perluas smoke per service (satu GET/list handler per service minimum).

## Menambah smoke baru

1. `BootstrapOwner(t)` + `WithOwnerAuth(fx)`
2. Panggil handler Encore langsung (bukan raw `//encore:api` — gunakan `Serve*HTTP` di package target)
3. `AssertJSONFields` / `AssertJSONArrayField`
4. Update `serviceSmokePhase` di `catalog_smoke_test.go`

Raw handler wrappers: `auth/http_testutil.go`, `webhook/http_testutil.go`, `payment/http_testutil.go`.

## Service yang butuh fixture khusus

| Service | Pendekatan smoke | Catatan |
|---------|------------------|---------|
| **admin** | `BootstrapSuperAdmin` + `WithSuperAdminAuth` → `admin.ListTenants` | `requireSuperAdmin` cek `auth.Data().Role == super_admin`; impersonation belum di-smoke |
| **audit** | `BootstrapSuperAdmin` + `WithSuperAdminAuth` → `audit.ListAuditLogs` | `RecordAudit` private — tidak di-smoke HTTP |
| **workflow** | `ensureWorkflowRuleTable` (DDL minimal, tanpa `pg_trgm`) → `workflow.ListRules` | Full `RunSchemaPatches` gagal di Encore test cluster (`gin_trgm_ops` missing) |
| **importcsv** | `ImportJobStatus` (NotFound) + unit `parseCSV`/`suggestMapping` di `importcsv/import_test.go` | `Preview` multipart via typed API gagal (`FileHeader.Open`); `Execute` + Pub/Sub belum di-smoke |

Fixture super admin: `super_admin_fixture.go` (`BootstrapSuperAdmin`, `WithSuperAdminAuth`).

## CI

`.github/workflows/api-smoke.yml` — PR ke `master` saat `internal/apitest/**` berubah. CI mem-start Redis (`127.0.0.1:6379`) dan set secret `RedisURL` sebelum `encore test`.
