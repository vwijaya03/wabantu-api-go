# Patch PII dijalankan setelah tabel `evt_*` dibuat

**Tanggal:** 2026-10-24
**Branch:** `fix/tenant-pii-after-events` (dari `origin/master` `3f66543`)
**Tipe:** fix
**Status:** commit lokal, belum push/PR/merge

## Masalah

Pada tenant yang baru register, `POST /api/v1/events/detail/:eventId/people` gagal 500:

```
column "full_name_enc" of relation "evt_event_person" does not exist (SQLSTATE 42703)
```

Ditemukan saat seeding stack lokal QA (lihat `audit/local-stack-oct24.md` di luar repo).

## Akar masalah

1. `PIISchemaPatchSQL` meng-`ALTER` tabel modul opsional hanya bila tabelnya sudah ada (`to_regclass`).
2. Saat signup (`runTenantBootstrapPatches`) dan migrate (`RunSchemaPatches`), `runPIISchemaOnConn` dipanggil **sebelum** `runFinanceSchemaAndSeed` → `runEventsSchemaAndSeed`, yang baru membuat `evt_event_person` / `evt_staff_roster` **tanpa** kolom PII.
3. Urutan yang sama ada di `cloudAdminTenantDDLBlocks()` (Encore Cloud): `"pii patch"` sebelum `"events patch"` dalam satu transaksi admin.
4. `tenantschema.PIIReady` hanya mengecek `contact`/`lead`, jadi patch PII tidak pernah dijalankan ulang.

Kode events selalu menulis `full_name_enc` (fail-closed, tanpa fallback plaintext), jadi hasilnya 500 — **bug availability, bukan kebocoran PII**.

## Perubahan

| File | Perubahan |
|---|---|
| `tenant/schema_patch.go:232,242,244-272` | `RunSchemaPatches` dan `runTenantBootstrapPatches` memakai `runTenantPatchSteps`; urutan `tenantPatchSteps()` = always-apply → finance+events+inventory → **pii (terakhir)** |
| `tenant/cloud_admin_ddl.go:49-57` | Blok `"pii patch"` dipindah ke posisi terakhir (setelah events/inventory/retrieval) |
| `shared/tenantschema/ready.go:185-217` | `PIIReady` juga mengecek `evt_event_person.full_name_enc` + `normalized_name` bila tabel ada (tenant tanpa modul events tetap ready) |
| `shared/tenantschema/pii_ready_test.go` | Test murni (fake catalog) untuk `PIIReady` |
| `tenant/pii_order_test.go` | Test urutan blok cloud & langkah patch, test DB tenant baru, test DB repair tenant terdampak + idempoten |
| `internal/apitest/events_smoke_test.go:53-` | Smoke: tenant baru → create event → create person sukses (201) dan `full_name` = `pii.Placeholder` (bukan plaintext), `full_name_enc` terisi |
| `docs/EVENTS_MODULE.md` | Catatan urutan patch + baris troubleshooting |

Tidak ada perubahan perilaku tulis data: tetap fail-closed, tidak ada fallback plaintext.

## Tenant lama yang sudah terlanjur kena

`PIISchemaPatchSQL` idempoten (`ADD COLUMN IF NOT EXISTS`, `DROP/CREATE INDEX IF [NOT] EXISTS`). Dengan `PIIReady` baru, tenant terdampak dilaporkan *not ready*:
- Encore Cloud: `CloudTenantReady` jadi false → `POST /api/v1/admin/migrate-tenant-schemas` tidak lagi men-skip tenant itu → `ProcessTenantSchemaMigration` → admin DDL (PII terakhir) menambah kolom. `GET /api/v1/tenant/readiness` akan menampilkan `cloudReady=false` sampai diperbaiki.
- Lokal: migrate men-skip tenant yang `schema_patch_version` sudah current, jadi perbaikan manual (SQL patch PII ke schema) atau bump `CurrentSchemaPatchVersion` (belum dilakukan di branch ini — keputusan CTO).

## Verifikasi

Lihat laporan PR / tiket. Ringkas: `go build ./...`, `go vet` paket tersentuh, `go test ./shared/tenantschema/`, `encore test` (salinan app tidak ter-link, Postgres 17 lokal) untuk `./tenant/... ./events/... ./shared/tenantschema/... ./internal/apitest/...` — semua lulus. Mutation check: revert fix → test gagal dengan error 42703 yang sama.
