# Revamp UI katalog + import teks AI + fix upsert partial index

## Masalah / Kebutuhan

1. Halaman katalog dashboard perlu UX modern: tabel lebar, form Sheet, aksi duplikat, bulk delete, refresh list.
2. Seller sering punya deskripsi produk dalam **teks** (bukan screenshot) — butuh import AI selain vision gambar.
3. Commit import katalog gagal di PostgreSQL: `there is no unique or exclusion constraint matching the ON CONFLICT specification` (`42P10`) karena index unik partial `(source, external_code) WHERE deleted_at IS NULL` tidak cocok dengan `ON CONFLICT` tanpa klausa `WHERE`.
4. Modal edit transaksi **Saldo Awal** di inventory: tombol tutup ikut scroll; peringatan a11y Radix Dialog tanpa `Description`.

## Perubahan

### api-go — import teks AI

| Method | Path |
|--------|------|
| `POST` | `/api/v1/business/catalog/import-text/preview` |
| `GET` | `/api/v1/business/catalog/import-text/draft/:jobId` |
| `POST` | `/api/v1/business/catalog/import-text/draft/:jobId/commit` |

- Input teks 10–12.000 karakter → Haiku (`ai/catalog_text.go`) → staging Redis `catalog:text:staging:{jobId}`, job ID `ctxt_*`.
- `source` commit: `text_import`.
- Reuse tipe draft `CatalogImageDraftItem`, parser `parseCatalogVisionJSON`, response preview/commit dari import gambar.

### api-go — shared commit + fix upsert

- Ekstrak `commitCatalogDraftItems` di `business/catalog_text.go` — dipakai commit **gambar** dan **teks**.
- SQL upsert: `ON CONFLICT (source, external_code) WHERE deleted_at IS NULL DO UPDATE SET ...`
- Perbaikan sama di `importcsv/import.go` untuk import CSV produk.

### web-frontend — katalog dashboard

| Fitur | Lokasi |
|-------|--------|
| Tabel Shadcn full-width, truncate SKU + tooltip | `app/(dashboard)/dashboard/catalog/page.tsx` |
| Form tambah/edit di Sheet (bukan dialog kecil) | `components/catalog/*` |
| Menu aksi: edit, duplikat, hapus | dropdown per baris |
| Bulk delete toolbar | loop `catalogApi.remove()` paralel |
| Tombol refresh list | `refetch()` + icon spin |
| Duplikat produk | pre-fill Sheet, SKU baru via `generateSkuFromProductName` |
| Import teks AI | `/dashboard/catalog/import-text` |
| Editor deskripsi markdown | `description-rich-editor.tsx` — form + draft import |
| Draft table bersama | `catalog-import-draft-table.tsx` |

### web-frontend — inventory dialog

- `stock-transaction-edit-dialog.tsx`: header tetap, body scroll, `pr-12` untuk close button, kartu per baris saldo awal, `DialogFooter`.
- `components/ui/dialog.tsx`: context `DialogDescription` + fallback `aria-describedby={undefined}` jika tidak ada deskripsi.

## File utama

**api-go**

- `ai/catalog_text.go`, `ai/catalog_text_test.go`
- `business/catalog_text.go`
- `business/catalog_image.go` — refactor commit ke helper bersama
- `importcsv/import.go` — partial index upsert
- `docs/CATALOG_TEXT_IMPORT.md`, update `docs/CATALOG_IMAGE_IMPORT.md`

**web-frontend**

- `app/(dashboard)/dashboard/catalog/page.tsx`
- `app/(dashboard)/dashboard/catalog/import-text/page.tsx`
- `components/catalog/catalog-import-draft-table.tsx`
- `components/catalog/description-rich-editor.tsx`
- `lib/api/catalogText.ts`, `lib/catalog/form.ts`, `lib/markdown/simple.ts`
- `components/inventory/stock-transaction-edit-dialog.tsx`
- `components/ui/dialog.tsx`
- `docs/CATALOG_MODULE.md`

## Testing

```bash
# api-go
cd api-go
encore test ./ai/... ./business/...

# web-frontend
cd web-frontend
npm run lint && npm run build
```

**Manual QA katalog**

1. List katalog: search, pagination, refresh, bulk delete.
2. Tambah/edit via Sheet; duplikat → SKU baru.
3. Import teks: preview → edit deskripsi markdown → commit → item muncul di list.
4. Re-commit SKU sama → update, bukan error 42P10.

**Manual QA inventory**

1. Operasi Stok → Saldo Awal → buka edit transaksi lama.
2. Tombol X tetap di pojok kanan atas saat scroll.
3. Tidak ada warning Radix "Missing Description" di konsol.

## Catatan deploy

- Tidak ada migrasi schema baru; fix upsert murni SQL.
- Setelah deploy api-go, regen API catalog jika menambah endpoint: `go run scripts/gen-api-catalog.go`.
- Import commit **belum** memanggil `afterCatalogItemWritten` / indexing RAG — gap yang sama dengan import gambar (CRUD manual sudah enqueue outbox).

## Dokumentasi

- Spesifikasi: [docs/CATALOG_TEXT_IMPORT.md](../docs/CATALOG_TEXT_IMPORT.md)
- Frontend: [web-frontend/docs/CATALOG_MODULE.md](../../web-frontend/docs/CATALOG_MODULE.md)
