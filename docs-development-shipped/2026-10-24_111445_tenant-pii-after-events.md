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
| `shared/tenantschema/ready.go:185-226` | `PIIReady` juga mengecek `evt_event_person.full_name_enc` + `normalized_name` dan `evt_staff_roster.full_name_enc` + `normalized_name_idx`, masing-masing hanya bila tabelnya ada (`eventsPIITables`, :220-226; tenant tanpa modul events tetap ready) |
| `tenant/schema_version.go:3-8` | `CurrentSchemaPatchVersion` 3 → **4** agar tenant terdampak sembuh sendiri via lazy migrate |
| `tenant/schema_manifest.go:16` | Deskripsi manifest v4 |
| `shared/tenantschema/pii_ready_test.go` | Test murni (fake catalog) untuk `PIIReady`, termasuk `evt_staff_roster` (:114-160) dan cek bahwa setiap kolom yang dicek memang ditambah `PIISchemaPatchSQL` |
| `tenant/pii_order_test.go` | Test urutan blok cloud & langkah patch, test DB tenant baru, test DB repair tenant terdampak + idempoten, guard versi ≥ 4 (:135), repair roster saja (:145) |
| `internal/apitest/schema_v4_migration_test.go` | `TestSchemaV4Migration_IdempotentOnHealthyOldTenant`: tenant sehat berisi data terenkripsi (person + roster) diset v3 → `ProcessTenantSchemaMigration` 2× → tanpa error, versi = 4, isi baris (`row_to_json`, termasuk `*_enc`) `evt_event`, `evt_event_person`, `evt_staff_roster`, `evt_volunteer_role`, `contact`, `business_profile` identik |
| `internal/apitest/events_smoke_test.go:53-` | Smoke: tenant baru → create event → create person sukses (201) dan `full_name` = `pii.Placeholder` (bukan plaintext), `full_name_enc` terisi |
| `docs/EVENTS_MODULE.md` | Catatan urutan patch + baris troubleshooting |

Tidak ada perubahan perilaku tulis data: tetap fail-closed, tidak ada fallback plaintext.

## Tenant lama yang sudah terlanjur kena — bump v4 (self-heal)

`PIISchemaPatchSQL` idempoten (`ADD COLUMN IF NOT EXISTS`, `DROP/CREATE INDEX IF [NOT] EXISTS`). `CurrentSchemaPatchVersion` dinaikkan ke **4** (keputusan CTO), sehingga:

- **Setelah deploy**, setiap tenant dengan `schema_patch_version < 4` (semua tenant lama) pada akses pertama memicu `PrepareTenantAccess` → `PublishLazySchemaMigration` (async, sekali per proses per schema, tidak memblok request) → `ProcessTenantSchemaMigration`: cloud = admin DDL (PII terakhir) lalu `RunSchemaPatches`; lokal = `RunSchemaPatches` (PII terakhir) → versi dicatat 4.
- Tenant terdampak (kolom `evt_*` PII hilang) mendapat kolomnya; sampai migrate selesai, tambah staf/relawan tetap 500 (fail-closed, tanpa fallback plaintext).
- Tenant sehat: migrate no-op terhadap data (dibuktikan test idempotensi v4).
- Encore Cloud: `PIIReady` (contact/lead + `evt_event_person` + `evt_staff_roster`) membuat `CloudTenantReady` false untuk tenant yang masih kurang kolom, jadi `GET /api/v1/tenant/readiness` menampilkan `cloudReady=false` dan `POST /api/v1/admin/migrate-tenant-schemas` tidak men-skip tenant itu walau versinya sudah 4.
- Tenant baru: register langsung benar (PII terakhir) dan dicatat v4.
- Beban: semua tenant menjalankan full patch set sekali; bisa dipercepat/diratakan dengan `migrate-tenant-schemas` (rollout) setelah deploy.

Catatan test (bukan perubahan prod): di cluster test Encore `pg_trgm` tidak terpasang dan role test tidak bisa `CREATE` di `public`, jadi `CREATE EXTENSION IF NOT EXISTS pg_trgm` memasang ekstensi di schema tenant pertama dan schema lain gagal `gin_trgm_ops does not exist` (sudah tercatat di `internal/apitest/README.md`). Test DB baru memanggil `resetTestTrgm` (DROP EXTENSION di DB test sekali pakai) sebelum menjalankan patch penuh. Perlu dipastikan di prod/cloud bahwa `pg_trgm` ada di `public` (inferensi, belum diverifikasi; perilaku ini sudah ada sejak sebelum branch ini).

## Verifikasi

Lihat laporan PR / tiket. Ringkas: `go build ./...`, `go vet` paket tersentuh, `go test ./shared/tenantschema/`, `encore test` (salinan app tidak ter-link, Postgres 17 lokal) untuk `./tenant/... ./events/... ./shared/tenantschema/... ./internal/apitest/...` — semua lulus. Mutation check: revert fix → test gagal dengan error 42703 yang sama; versi kembali 3 → guard versi + test v4 gagal; roster dihapus dari `PIIReady` → 3 test murni + test DB roster gagal; patch dibuat mengubah `full_name_enc` → test idempotensi v4 gagal ("rows of evt_event_person changed").
