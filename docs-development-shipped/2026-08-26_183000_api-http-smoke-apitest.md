# API HTTP smoke — `internal/apitest` (28 service)

**Tanggal:** 2026-08-26  
**PR:** [#140](https://github.com/vwijaya03/wabantu-api-go/pull/140) (`test/apitest-http-smoke`)  
**Tipe:** test  
**Status:** Merged ke `master`

## Masalah / Kebutuhan

335+ endpoint Encore tidak punya smoke HTTP terintegrasi per service. Regression struktural (`internal/apiregistry`) memvalidasi inventaris route, tetapi tidak memanggil handler dengan auth + tenant schema nyata. Perlu baseline smoke Encore: bootstrap tenant, auth context, satu handler representatif per service.

## Perubahan

### Service baru `internal/apitest`

Encore service dengan endpoint private `GET /internal/apitest/ping` agar typed API test (`encore test`) dapat bootstrap service.

### Fixture & helper

| Helper | Fungsi |
|--------|--------|
| `BootstrapTenant` | Provision system DB + `RunTenantDDL` + branch seed |
| `BootstrapOwner` | Typed API smoke — `et.OverrideAuthInfo` saja |
| `BootstrapOwnerWithToken` | JWT via Redis (auth HTTP smoke) |
| `BootstrapSuperAdmin` | Super admin untuk admin/audit smoke |
| `WithOwnerAuth` / `WithSuperAdminAuth` | Set auth context di test |
| `RequireEncoreInfra` | Skip bila `-short` |
| `AssertJSONFields` / `AssertJSONArrayField` | Assert response handler |

Raw HTTP wrappers: `auth/http_testutil.go`, `webhook/http_testutil.go`, `payment/http_testutil.go`.

### Smoke per service (phase 1)

Satu atau lebih handler per service: health, auth, order, inbox, inventory, finance, events, billing, branch, leads, analytics, business, kb, usage, webhook, payment, flag, shipping, notification, whatsappapi, ai, broadcast, tenant, tenantaccess, **admin, audit, workflow, importcsv**.

`catalog_smoke_test.go` — peta coverage vs `catalog_snapshot.json` (apitest package).

### Perbaikan pendukung

- `tenant/tenant.go` — tabel `branch` ditambahkan ke `RunTenantDDL` (fixture smoke butuh branch default)
- `audit/audit.go` — fix scan UUID null untuk baris audit platform
- `importcsv/import_test.go` — unit test `parseCSV` / `suggestMapping`
- `auth/testutil.go` — helper issue token untuk test

### CI & script lokal

| File | Peran |
|------|------|
| `.github/workflows/api-smoke.yml` | PR ke `master` saat `internal/apitest/**` berubah |
| `scripts/run-api-smoke-tests.sh` | Runner lokal |
| `scripts/ensure-encore-test-redis.sh` | Auto-start container `wabantu-apitest-redis` |

CI mem-start Redis `127.0.0.1:6379` dan set secret lokal `RedisURL` sebelum `encore test`.

## File utama

- `internal/apitest/service.go` — ping endpoint
- `internal/apitest/*_smoke_test.go` — smoke per domain
- `internal/apitest/catalog_smoke_test.go` — coverage map
- `internal/apitest/catalog_snapshot.json` — mirror katalog endpoint
- `internal/apitest/README.md` — panduan operasional

## Testing

```bash
./scripts/ensure-encore-test-redis.sh   # lokal, jika Redis belum jalan
./scripts/run-api-smoke-tests.sh

# atau
encore test ./internal/apitest/ -count=1 -v

# skip infra berat
encore test ./internal/apitest/ -short
```

## Smoke yang belum / terbatas

| Service | Batasan |
|---------|---------|
| `importcsv` | `Preview` multipart gagal via typed API; `Execute` + Pub/Sub belum di-smoke |
| `workflow` | Full `RunSchemaPatches` butuh `pg_trgm` di test cluster |
| `admin` | Impersonation flow belum di-smoke |

## Catatan deploy

- Service `internal/apitest` hanya untuk test — endpoint `/internal/apitest/ping` tidak untuk produksi.
- Smoke penuh (auth JWT) butuh Redis lokal; lihat PR [#142](./2026-08-26_203200_apitest-encore-cloud-redis-fix.md) untuk perilaku di Encore Cloud build.
