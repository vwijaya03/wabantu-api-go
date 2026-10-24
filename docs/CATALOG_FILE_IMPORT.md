# Import katalog dari file (CSV & XLSX)

Owner upload file CSV/XLSX di dashboard (`/dashboard/import`, FE `lib/api/import.ts`) → **preview** (parse + staging Redis) → owner atur mapping kolom → **execute** (Pub/Sub `file-import`) → upsert ke `business_catalog_item` (`source='import'`). Preview dan execute hanya menerima target katalog (`business_catalog_item`), end-to-end.

Tidak memakai AI; tidak mengurangi kuota `ai_token`.

## API (owner)

| Method | Path | Tipe | Deskripsi |
|--------|------|------|-----------|
| `POST` | `/api/v1/import/preview` | **raw** multipart (`importcsv/import.go` `PreviewHTTP`) | Parse file, simpan semua baris di Redis `import:staging:<jobId>` (TTL 24 jam), balas `jobId` + 5 baris sampel |
| `POST` | `/api/v1/import/execute` | typed JSON | `{ jobId, targetTable, columnMapping }` → publish ke `file-import`. `targetTable` wajib sama dengan target staging (kosong = target staging) dan hanya `business_catalog_item` → selain itu 400 |
| `GET` | `/api/v1/import/status/:jobId` | typed | Hasil worker (`import:result:<jobId>`) |

Semua endpoint `auth` + `tag:owner`, dan **ketiganya** mengecek `CanPerformOwnerActions()` (owner, atau super_admin yang sedang impersonate) → staff 403. Preview memakai `AuthenticateHTTP`; execute/status memakai `requireImportOwner` (`importcsv/import.go`). Isolasi tenant (`importcsv/tenant_scope.go`): staging (`import:staging:<jobId>`) menyimpan `tenantId` + `tenantSchema` sesi (diisi `servePreview`), hasil worker (`import:result:<jobId>`) menyimpan `tenantId`. `execute` (`loadStaging` → `checkStagingTenant`) dan `status/:jobId` (`decodeImportResultForTenant`) hanya melayani tenant yang sama; beda atau kosong → **404** dengan pesan yang sama seperti tidak ditemukan. Respons status tetap `ImportResult` (tanpa `tenantId`).

`jobId` = `imp_` + 32 hex dari `crypto/rand` (`newImportJobID`, `importcsv/jobid.go`): tidak bisa ditebak dan aman langsung dipakai di path `/import/status/${jobId}`.

Allowlist target (`validColumns`) kini hanya `business_catalog_item`. `processRow` masih punya cabang `knowledge_base_entry` hanya agar pesan `file-import` lama yang sudah antre tetap bisa diproses.

### Kenapa preview harus `raw`

API Encore bertipe me-marshal parameter request melewati boundary API. `*multipart.FileHeader` hanya menyimpan isi file di field unexported, jadi parameter `file *multipart.FileHeader` pada API bertipe **tidak pernah bisa dibuka** → dulu selalu `400 "cannot open uploaded file"` (bug sejak commit awal `ceaff06`, diperbaiki 2026-10-24). Pola yang benar sama dengan upload lain (`business/catalog_image_http.go`, `events/image_import_http.go`, `finance/transaction_image_http.go`): `//encore:api auth raw` + `ParseMultipartForm`. Test: `importcsv/preview_rootcause_test.go`.

### Request preview (multipart/form-data)

| Field | Wajib | Nilai |
|-------|-------|-------|
| `file` | ya | file `.csv` atau `.xlsx` (hanya file pertama yang dipakai) |
| `targetTable` | tidak | hanya `business_catalog_item` (kosong = `business_catalog_item`); `knowledge_base_entry` dan nilai lain → 400 (keputusan CTO 2026-10-24). Execute menegakkan aturan yang sama. |

Contoh: `curl -H "Authorization: Bearer $TOKEN" -F file=@katalog.csv -F targetTable=business_catalog_item $API/api/v1/import/preview`

Nama field `file` dan `targetTable` dipakai FE — **jangan diganti**.

### Response 200 (sama dengan bentuk `PreviewResponse` sebelumnya, tanpa envelope)

```json
{ "jobId": "imp_3f9c0a1b2c3d4e5f60718293a4b5c6d7", "targetTable": "business_catalog_item", "headers": ["Nama","Kode"],
  "sampleRows": [["Item A","SKU1"]], "suggestions": {"Nama":"name","Kode":"external_code"}, "totalRows": 1 }
```

### Error (bentuk standar Encore `{code, message, details}`; pesan generik, detail hanya di `rlog`)

| HTTP | Kapan |
|------|-------|
| 401 | Tanpa token / sesi tidak valid |
| 403 | Bukan owner (mis. staff) |
| 413 | Body > 10 MB (`Content-Length` atau terpotong `http.MaxBytesReader`) |
| 400 | Bukan multipart, field `file` kosong, `targetTable` selain `business_catalog_item`, ekstensi bukan `.csv`/`.xlsx`, isi tidak cocok ekstensi, CSV rusak, XLSX rusak/zip bomb, hanya header, > 50.000 baris |
| 500 | Gagal membuat `jobId` / staging Redis (pesan generik) |

Execute / status: 403 bukan owner; 400 `targetTable` beda dari staging atau bukan katalog, kolom mapping tidak valid; 404 staging/hasil tidak ada.

Log `rlog` tidak memuat nama file dari user — hanya ukuran dan ekstensi ternormalisasi (`.csv`/`.xlsx`/`other`).

## Batas & keamanan upload (`importcsv/preview_upload.go`)

| Konstanta | Nilai | Fungsi |
|-----------|-------|--------|
| `PreviewMaxBodyBytes` | 10 MiB | Batas keras body (`http.MaxBytesReader`) → 413 |
| `previewMaxMemoryBytes` | 8 MiB | Memori `ParseMultipartForm`; sisanya temp file (tetap ≤ 10 MiB), dihapus `RemoveAll` |
| `xlsxUnzipSizeLimit` | 64 MiB | `excelize.Options.UnzipSizeLimit` — total ukuran ekstrak (anti zip bomb) |
| `xlsxUnzipXMLSizeLimit` | 16 MiB | `excelize.Options.UnzipXMLSizeLimit` — XML worksheet di memori |
| `maxImportRows` | 50.000 | Batas baris data; parser berhenti di baris ke-50.001 (tidak memuat semuanya dulu) |

Validasi tipe file: ekstensi (`.csv`/`.xlsx`, case-insensitive) **dan** sniff 512 byte pertama — XLSX wajib diawali `PK\x03\x04`; CSV wajib teks UTF-8 (bukan zip, tanpa byte NUL, `DetectContentType` = `text/*`). BOM UTF-8 di header CSV dibuang. Entri zip yang memalsukan ukuran (declared kecil, isi besar) tidak di-inflate melewati ukuran declared oleh `archive/zip`.

## Test

`encore test ./importcsv/...` — `preview_http_test.go` (CSV/XLSX OK, 401, 403, 413 Content-Length & streamed, tipe salah, zip bomb, field hilang, > 50.000 baris CSV & XLSX, error generik, gagal jobId), `execute_status_test.go` (target sama OK, target beda/KB 400, staff 403 di execute & status), `jobid_test.go` (format & unik), `tenant_scope_test.go` (status lintas tenant 404, tenant sama 200, staging lintas tenant 404), `preview_tenant_test.go` (preview HTTP → staging berisi `TenantID` → execute tenant sama OK, tenant lain 404) dan `preview_rootcause_test.go`.
