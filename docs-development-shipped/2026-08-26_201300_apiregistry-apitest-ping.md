# Apiregistry — izinkan `/internal/` + sync 336 endpoint

**Tanggal:** 2026-08-26  
**PR:** [#141](https://github.com/vwijaya03/wabantu-api-go/pull/141) (`fix/apiregistry-apitest-ping`)  
**Tipe:** fix  
**Status:** Merged ke `master`

## Masalah / Kebutuhan

Setelah PR #140, service `internal/apitest` menambahkan `GET /internal/apitest/ping`. CI `regression-fast` gagal karena:

1. `TestEndpointPathsWellFormed` menolak prefix `/internal/`
2. Golden snapshot masih **335 endpoint / 28 service** — drift vs kenyataan **336 / 29**

## Perubahan

- `internal/apiregistry/registry_test.go` — izinkan path dengan prefix `/internal/` (endpoint test-only)
- Regenerate `catalog_snapshot.json` dan `service_counts.json`:
  - **336 endpoint**
  - **29 service** (termasuk `internal` dengan 1 endpoint)

## File utama

- `internal/apiregistry/registry_test.go`
- `internal/apiregistry/catalog_snapshot.json`
- `internal/apiregistry/service_counts.json`

## Testing

```bash
go test ./internal/apiregistry/ -count=1 -v
./scripts/run-ai-regression-tests.sh
```

## Catatan deploy

- Tidak ada perubahan runtime produksi — hanya gate test + snapshot.
- Setiap endpoint baru wajib regenerate snapshot sebelum merge.
