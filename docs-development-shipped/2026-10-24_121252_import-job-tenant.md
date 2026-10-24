# Shipped: fix(import) — status job tidak bocor antar tenant

**Status:** commit lokal, menunggu izin push dari Viko (draft PR ke `master`)  
**Branch:** `fix/import-job-tenant` (dari `origin/master` `3f66543`)  
**Tanggal:** 2026-10-24  
**Terkait:** `fix/import-preview-upload` (preview raw multipart; `docs/CATALOG_FILE_IMPORT.md` ada di branch itu)

---

## Masalah

QA (stack :4001) mengonfirmasi: owner tenant B membaca status import tenant A lewat `GET /api/v1/import/status/:jobId` dan mendapat **200**.

- `importcsv/import.go:187-203` (master): `ImportJobStatus` membaca `import:result:<jobId>` dari Redis tanpa mengecek tenant sama sekali.
- `jobId` = `imp_<time.Now().UnixNano()>` (`import.go:157` master), jadi mudah ditebak/di-enumerasi.
- Hasil worker (`handleImport`) disimpan tanpa tenant, jadi status memang tidak punya data untuk dicek.
- Execute: `loadStaging` hanya membandingkan `TenantSchema` dan membalas 403 "belongs to another tenant" (membocorkan bahwa jobId ada).

## Perubahan

1. `importcsv/jobid.go` (baru): `newImportJobID() (string, error)` → `imp_` + 32 hex dari `crypto/rand` (128 bit). Hex aman langsung dipakai FE di path `/import/status/${jobId}`. Helper dan file **identik** dengan yang ada di `fix/import-preview-upload`.
2. Staging (`import:staging:<jobId>`) menyimpan `tenantId` (`importStagingPayload.TenantID`).
3. Hasil worker (`import:result:<jobId>`) disimpan sebagai `storedImportResult{tenantId, ImportResult...}` (JSON tetap flat; respons API tetap `ImportResult` tanpa `tenantId`).
4. Status: `tenantId` hasil harus sama dengan `TenantID` sesi; beda atau kosong (termasuk hasil lama sebelum fix) → **404** dengan pesan yang sama dengan "tidak ditemukan".
5. Execute (`loadStaging`): `tenantId` **dan** `tenantSchema` staging harus cocok dengan sesi; beda/kosong → **404** "import preview expired or not found — upload again" (sebelumnya 403).
6. Logika di `importcsv/tenant_scope.go` (baru) agar bisa diuji tanpa Redis; snapshot endpoint (`internal/apiregistry`, `internal/apitest`) hanya nomor baris importcsv.

## Dampak

- Hasil import dan staging yang ditulis sebelum deploy tidak punya `tenantId` → 404 (TTL 24 jam). Preview di master selalu 400 (bug `fix/import-preview-upload`), jadi praktis tidak ada staging yang hidup.
- Dari kode & git log saja; prod tidak dicek.

## Verifikasi

- `encore test ./importcsv/... ./internal/apiregistry/... ./internal/apitest/...` hijau di salinan unlinked `/tmp/wabantu-import-tenant-test` (log `audit/logs-import-tenant/`).
- Test: status lintas tenant 404, tenant sama 200 (tanpa `tenantId` di respons), tenant kosong/hasil lama 404, staging lintas tenant/schema 404, format `imp_[0-9a-f]{32}` + unik.
- Mutation: hapus cek tenant status / staging → test gagal.
- Belum diuji end-to-end via HTTP (perlu Redis + dua sesi tenant) — QA ulangi skenario :4001 setelah merge.
