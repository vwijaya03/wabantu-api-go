# Shipped: fix(import) — mapping header deterministik

**Status:** commit lokal, menunggu izin push dari Viko (draft PR ke `master`)  
**Branch:** `fix/import-header-mapping`, **ditumpuk di atas** `fix/import-job-tenant` (`c766e99`) ← `fix/import-preview-upload` (`8d6a1a9`); base PR tetap `master`  
**Urutan merge:** #273 → `fix/import-preview-upload` → `fix/import-job-tenant` → `fix/import-header-mapping`  
**Tanggal:** 2026-10-24  
**Modul:** [`docs/CATALOG_FILE_IMPORT.md`](../docs/CATALOG_FILE_IMPORT.md) (bagian `suggestions`)

---

## Masalah

QA: preview template produk resmi kadang menyarankan `barcode` → `external_code` (24 dari 40 preview salah).

- `suggestMapping` (`importcsv/import.go:282-320` di `c766e99`) melakukan `range` atas map `aliases` (urutan acak di Go) dan mencocokkan dengan `hl == alt || strings.Contains(hl, alt)`. `barcode` mengandung `code` (alias `external_code`), jadi hasilnya tergantung kolom mana yang kebetulan diiterasi duluan.
- Reproduksi lokal dengan salinan fungsi lama: 23/40 dan 63/100 run salah untuk header template (`audit/logs-import-mapping/00-before-c766e99-suggestMapping.log`).
- Efek lain: `Kode Produk` bisa jadi `name` (mengandung `produk`), dan beberapa header bisa disarankan ke kolom yang sama (`kode`, `sku`, `Kode Barang` → semuanya `external_code`).

## Perubahan

`importcsv/import.go` (bagian "Column suggestion"):

1. `suggestionAliases` — slice berurutan (bukan map) kolom katalog + alias ternormalisasi; ditambah `kode_produk`, `kode_barang`, `nama_produk`, dll. Alias KB lama dihapus (target KB sudah tidak valid).
2. `normalizeHeader` — lowercase, trim, rangkaian non-alfanumerik → `_`.
3. Pass exact dulu (`exactColumn`), lalu pass partial (`partialColumn`: alias terpanjang yang terkandung; seri → urutan kolom/alias).
4. `suggestMappingFor` — satu kolom ↔ satu header; exact menang atas partial, lalu header lebih awal menang. Header yang kolom terbaiknya sudah dipakai / tidak valid untuk target dibiarkan kosong (tanpa fallback ke kolom yang lebih lemah).
5. Nomor baris endpoint importcsv tidak bergeser (perubahan di bawah baris 280) → snapshot tidak diubah.

## Template resmi

FE `wabantu-web-frontend` `app/api/import/templates/product/route.ts:5-13` (origin/master `91958af`), dilink dari `/dashboard/import` ("1. Download template produk" → `/api/import/templates/product?format=csv|xlsx`): `external_code,name,description,sell_price,sell_unit,is_active,barcode`.

| Header | Sebelum (100 run) | Sesudah (100 run) |
|--------|-------------------|-------------------|
| external_code | external_code ×100 | external_code ×100 |
| name | name ×100 | name ×100 |
| description | description ×100 | description ×100 |
| sell_price | sell_price ×100 | sell_price ×100 |
| sell_unit | sell_unit ×100 | sell_unit ×100 |
| is_active | is_active ×100 | is_active ×100 |
| barcode | barcode ×37, **external_code ×63** | barcode ×100 |

## Dampak

- Hanya saran di respons preview (`suggestions`); execute tetap memakai `columnMapping` dari owner. Header yang dulu kebetulan dapat saran ganda kini hanya satu yang disarankan.
- Dari kode & git log saja; prod tidak dicek.

## Verifikasi

- `encore test ./importcsv/... ./internal/apiregistry/... ./internal/apitest/...` hijau di salinan unlinked `/tmp/wabantu-import-mapping-test` (log `audit/logs-import-mapping/`).
- `importcsv/suggest_mapping_test.go`: template 100× identik & benar; `barcode`/`Kode Barcode`/`Barcode Produk` → `barcode` (100× tiap kasus), tanpa kolom barcode → kosong; `kode`/`kode produk`/`sku` → `external_code`; konflik kolom ganda hanya satu; normalisasi header.
- Mutation (semua gagal sesuai harapan): kembalikan map range + Contains-first; slice berurutan tapi Contains-first satu pass; hapus cek kolom terpakai; partial ambil alias pertama bukan terpanjang; tanpa pass exact; fallback ke kolom valid saja.
