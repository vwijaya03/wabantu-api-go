# Shipped: fix(import) — status job tidak bocor antar tenant

**Status:** commit lokal, menunggu izin push dari Viko (draft PR ke `master`)  
**Branch:** `fix/import-job-tenant`, **ditumpuk di atas** `fix/import-preview-upload` (`8d6a1a9`); base PR tetap `master`  
**Urutan merge:** #273 → `fix/import-preview-upload` → `fix/import-job-tenant`  
**Tanggal:** 2026-10-24  
**Modul:** [`docs/CATALOG_FILE_IMPORT.md`](../docs/CATALOG_FILE_IMPORT.md) (bagian isolasi tenant)

---

## Masalah

QA (stack :4001) mengonfirmasi: owner tenant B membaca status import tenant A lewat `GET /api/v1/import/status/:jobId` dan mendapat **200**.

- `importcsv/import.go:187-203` (master `3f66543`): `ImportJobStatus` membaca `import:result:<jobId>` dari Redis tanpa mengecek tenant sama sekali.
- `jobId` = `imp_<time.Now().UnixNano()>` (`import.go:157` master), jadi mudah ditebak/di-enumerasi.
- Hasil worker (`handleImport`) disimpan tanpa tenant, jadi status memang tidak punya data untuk dicek.
- Execute: `loadStaging` hanya membandingkan `TenantSchema` dan membalas 403 "belongs to another tenant" (membocorkan bahwa jobId ada).

## Perubahan

Branch ini di-rebase ke atas `fix/import-preview-upload`, jadi memakai preview raw (`servePreview`), `executeImport`, `importJobStatus`, dan `requireImportOwner` dari branch itu.

1. `importcsv/jobid.go` — `newImportJobID()` (`imp_` + 32 hex `crypto/rand`) sudah ada dari `fix/import-preview-upload`; dipakai apa adanya.
2. Staging (`import:staging:<jobId>`) menyimpan `tenantId`: `importStagingPayload.TenantID`, diisi `servePreview` (`importcsv/preview_upload.go`, `TenantID: user.TenantID`).
3. Hasil worker (`import:result:<jobId>`) disimpan sebagai `storedImportResult{tenantId, ImportResult...}` (JSON tetap flat; respons API tetap `ImportResult` tanpa `tenantId`).
4. Status (`importJobStatus`): `tenantId` hasil harus sama dengan `TenantID` sesi; beda atau kosong (termasuk hasil lama) → **404** dengan pesan yang sama dengan "tidak ditemukan". Cek owner (staff 403) tetap dari branch preview-upload.
5. Execute (`loadStaging` → `checkStagingTenant`): `tenantId` **dan** `tenantSchema` staging harus cocok dengan sesi; beda/kosong → **404** (dulu 403 yang membocorkan keberadaan job).
6. Logika di `importcsv/tenant_scope.go` (baru); snapshot endpoint hanya nomor baris importcsv.
7. Test `importcsv/preview_tenant_test.go`: preview lewat handler HTTP → staging wajib berisi `TenantID` sesi → execute tenant lain 404, tenant sama sukses. Mutation: hapus `TenantID: user.TenantID` di `servePreview` → test gagal.

## Dampak

- Hasil import dan staging yang ditulis sebelum deploy tidak punya `tenantId` → 404 (TTL 24 jam). Preview di master selalu 400 (bug `fix/import-preview-upload`), jadi praktis tidak ada staging yang hidup.
- Dari kode & git log saja; prod tidak dicek.

## Verifikasi

- `encore test ./importcsv/... ./internal/apiregistry/... ./internal/apitest/...` hijau di salinan unlinked `/tmp/wabantu-import-tenant-test` (log `audit/logs-import-tenant/`).
- Test: preview→execute membawa `TenantID`, status lintas tenant 404, tenant sama 200 (tanpa `tenantId` di respons), tenant kosong/hasil lama 404, staging lintas tenant/schema 404, format `imp_[0-9a-f]{32}` + unik.
- Mutation: hapus cek tenant status / staging → test gagal; hapus pengisian `TenantID` di `servePreview` → `TestPreviewThenExecute_StagingCarriesTenantID` & `TestPreview_CSV_OK` gagal.
- Belum diuji end-to-end via HTTP (perlu Redis + dua sesi tenant) — QA ulangi skenario :4001 setelah merge.
