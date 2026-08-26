# Apitest — Encore Cloud build tanpa Redis + fix `encore run` panic

**Tanggal:** 2026-08-26  
**PR:** [#142](https://github.com/vwijaya03/wabantu-api-go/pull/142) (`fix/apitest-encore-cloud-no-redis`)  
**Tipe:** fix  
**Status:** Merged ke `master`

## Masalah / Kebutuhan

Dua blocker setelah merge PR #140:

### A) Encore Cloud build gagal

Deploy staging menjalankan `encore test ./...` di build step. Encore Cloud **tidak** menyediakan Redis di `127.0.0.1:6379`. Semua smoke yang memanggil `BootstrapOwner` → `IssueTestAccessToken` gagal:

```
redis set session: connection refused
```

### B) `encore run` panic lokal

Import `encore.dev/et` ada di file non-`_test.go` di package service `apitest`:

```
panic: et: cannot create manager in non-test environment
```

## Perubahan

### Redis opsional untuk typed API smoke

| Sebelum | Sesudah |
|---------|---------|
| `BootstrapOwner` selalu issue JWT (butuh Redis) | `BootstrapOwner` hanya `et.OverrideAuthInfo` — **tanpa Redis** |
| — | `BootstrapOwnerWithToken` + `RequireRedis` untuk auth JWT smoke |
| — | `auth.RedisAvailable()` helper |
| `BootstrapSuperAdmin` issue JWT | `BootstrapSuperAdmin` tanpa JWT issuance |

Auth JWT smokes (`auth_smoke_test.go`) memakai `RequireRedis` — **di-skip otomatis** bila Redis tidak reachable (Encore Cloud build). Smoke penuh tetap jalan di CI `api-smoke.yml` dan lokal via `run-api-smoke-tests.sh`.

### Pindahkan helper ke `*_test.go`

Semua file yang import `encore.dev/et` di-rename ke `*_test.go`:

- `fixture_test.go`
- `tenant_fixture_test.go`
- `super_admin_fixture_test.go`
- `http_helpers_test.go`
- `redis_check_test.go`

Package `internal/apitest` untuk runtime hanya: `service.go` + `encore.gen.go`.

## File utama

- `internal/apitest/tenant_fixture_test.go` — `BootstrapOwner` vs `BootstrapOwnerWithToken`
- `internal/apitest/redis_check_test.go` — `RequireRedis`
- `auth/auth.go` — `RedisAvailable()`
- `internal/apitest/README.md` — dokumentasi prasyarat Redis

## Testing

```bash
# Typed smokes — lulus tanpa Redis (sama seperti Encore Cloud build)
encore test ./internal/apitest/ -run TestHealth -count=1

# Smoke penuh + auth JWT — butuh Redis
./scripts/run-api-smoke-tests.sh

# encore run tidak panic
encore run
```

## Catatan deploy

- **Wajib merge** sebelum deploy staging berikutnya — tanpa fix ini, `encore test ./...` di Encore Cloud build gagal.
- Tidak ada migrasi DB atau secret baru di cloud; Redis tetap diperlukan di runtime produksi (bukan di build step).
