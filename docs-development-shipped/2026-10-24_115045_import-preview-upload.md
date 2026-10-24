# Shipped: fix(import) — upload file preview selalu 400

**Status:** commit lokal, menunggu izin push dari Viko (draft PR ke `master`)  
**Branch:** `fix/import-preview-upload` (dari `origin/master` `3f66543`)  
**Tanggal:** 2026-10-24  
**Modul:** [`docs/CATALOG_FILE_IMPORT.md`](../docs/CATALOG_FILE_IMPORT.md)

---

## Masalah

`POST /api/v1/import/preview` selalu membalas `400 "cannot open uploaded file"` untuk CSV maupun XLSX (ditemukan QA di stack lokal berbasis master). Import katalog dari FE (`lib/api/import.ts`, halaman `/dashboard/import`) ikut gagal.

## Akar masalah

`importcsv/import.go:132-133` (master) adalah API Encore **bertipe**: `//encore:api auth method=POST path=/api/v1/import/preview tag:owner` dengan `func Preview(ctx, file *multipart.FileHeader)`. API bertipe me-marshal parameter request melewati boundary API, sedangkan `multipart.FileHeader` menyimpan isi file di field unexported (`content`/`tmpfile`), jadi `file.Open()` (`import.go:143-145`) selalu gagal.

Bukti: di `encore test` pada master, `Preview()` dipanggil dengan FileHeader hasil `ParseMultipartForm` dari request bergaya `curl -F file=@x.csv -F targetTable=business_catalog_item` (yang di luar Encore bisa dibuka) dan tetap gagal `invalid_argument: cannot open uploaded file` — log `audit/logs-import-preview/01-red-rootcause-master.log`. Kesimpulan: **endpoint raw memang wajib**.

Catatan: `targetTable` dari form juga diabaikan (target di-hardcode `business_catalog_item`).

## Perubahan

1. Preview jadi `//encore:api auth raw method=POST path=/api/v1/import/preview tag:owner` (`PreviewHTTP`), path/method sama, field `file` + `targetTable` sama, response JSON sama (`PreviewResponse` tanpa envelope), error tetap bentuk Encore `{code,message,details}`.
2. Auth: `appauth.AuthenticateHTTP` + `CanPerformOwnerActions()` (pola upload raw lain); staging pakai `TenantSchema` sesi.
3. `targetTable` dibaca dan dibatasi: kosong → `business_catalog_item`; hanya `business_catalog_item` yang diterima, `knowledge_base_entry` dan nilai lain → 400 (keputusan CTO 2026-10-24). `execute` tidak diubah.
4. Keamanan: `http.MaxBytesReader` 10 MiB (+ fast path `Content-Length`) → 413; `ParseMultipartForm(8 MiB)` + `RemoveAll`; validasi ekstensi + sniff isi; `excelize.Options{UnzipSizeLimit: 64 MiB, UnzipXMLSizeLimit: 16 MiB}`; batas 50.000 baris dihitung saat streaming (CSV `Read` loop, XLSX `Rows` iterator); pesan error generik, detail di `rlog`.
5. Endpoint lain di `importcsv` (`execute`, `status/:jobId`) tidak memakai `FileHeader` — tidak terdampak.
6. Snapshot katalog endpoint (`internal/apiregistry`, `internal/apitest`) diperbarui (annotation `auth raw`, nomor baris).

## Dampak ke prod (dari kode & git log saja — prod tidak dicek)

- Parameter `file *multipart.FileHeader` sudah ada sejak commit pertama `ceaff06` (2026-05-17, path `/import/preview`), path jadi `/api/v1/import/preview` di `36a8b82` (2026-05-18); tidak pernah diubah sampai `3f66543`.
- Jadi **setiap build yang pernah di-deploy** sejak saat itu membawa bug ini. Deploy staging otomatis setelah merge ke master + AI Regression lulus sejak `c47ba09` (2026-08-26, `.github/workflows/deploy-encore-staging.yml`). Riwayat deploy Encore Cloud sendiri tidak dicek.
- Repo FE dimulai `9990b65` (2026-07-08) dan sudah berisi halaman import + `lib/api/import.ts`, jadi import CSV/XLSX dari UI kemungkinan besar tidak pernah berhasil di lingkungan mana pun yang di-deploy (inferensi).

## Verifikasi

- `encore test ./importcsv/... ./internal/apiregistry/... ./internal/apitest/...` hijau (salinan unlinked `/tmp/wabantu-import-test`).
- Mutation: hapus `MaxBytesReader` → `TestPreview_Oversize_Streamed_413` gagal.
- Setelah merge: QA menguji import XLSX #273 end-to-end.
