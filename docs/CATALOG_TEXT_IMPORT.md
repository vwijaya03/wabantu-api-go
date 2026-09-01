# Import katalog dari teks (AI)

Fitur untuk tenant yang punya **deskripsi produk dalam teks** (caption marketplace, chat WhatsApp, catatan toko) tanpa screenshot atau CSV. Owner tempel teks → **Claude Haiku** mengekstrak SKU/varian → **halaman konfirmasi** → simpan ke `business_catalog_item`.

Alur dan staging **sama pola** dengan [import gambar](./CATALOG_IMAGE_IMPORT.md); perbedaan utama: input teks (bukan multipart), `source = text_import`, job ID prefix `ctxt_`.

## Model & kuota

| Langkah | Pakai AI? | Model | Kuota |
|---------|-----------|-------|-------|
| `POST .../import-text/preview` | Ya | `claude-haiku-4-5-20251001` | Mengurangi `ai_token` bulanan |
| `POST .../commit` | Tidak | — | — |

- Cek kuota: `usage.CheckQuota(tenantSchema, "ai_token")` sebelum preview.
- Pencatatan: `usage.RecordEvent(..., "ai_token", tokens)` + `usage.RecordAIActivity` dengan `PurposeCatalogImport`.
- Path aktivitas: `catalog_text_preview`.

## Batasan input

| Aturan | Nilai |
|--------|--------|
| Panjang minimal teks | **10** karakter (`catalogTextMinLen`) |
| Panjang maksimal teks | **12.000** karakter (`catalogTextMaxLen`) |
| Item hasil AI per job | Maks. **50** baris (sama prompt vision) |

Konstanta backend: `business/catalog_text.go`. Validasi panjang teks juga di UI (`import-text/page.tsx`).

## API (owner)

| Method | Path | Deskripsi |
|--------|------|-----------|
| `POST` | `/api/v1/business/catalog/import-text/preview` | Body `{ "text": "..." }` → AI ekstrak → draft Redis |
| `GET` | `/api/v1/business/catalog/import-text/draft/:jobId` | Ambil draft |
| `POST` | `/api/v1/business/catalog/import-text/draft/:jobId/commit` | Body `{ items: [...] }` → upsert DB |

Draft Redis key: `catalog:text:staging:{jobId}`, TTL **24 jam** (sama `catalogImageStagingTTL`).

Job ID format: `ctxt_{nanoseconds}`.

## Mapping varian & deskripsi

- Satu produk induk + banyak varian (rasa/ukuran/pack) → **beberapa baris** `business_catalog_item`.
- Bullet fitur (dairy free, rendah gula, dll.) → gabung ke `description` (newline).
- Pack count (`Isi 12`) → tambah ke `name` + `sellUnit` (`box`, `pack`, `pcs`).
- `source` = `text_import`
- `external_code` = SKU singkat (uppercase, max 32 karakter)
- Upsert: `ON CONFLICT (source, external_code) WHERE deleted_at IS NULL DO UPDATE`

Index partial unik di schema tenant (`idx_catalog_source_code`) **wajib** memakai klausa `WHERE deleted_at IS NULL` di `ON CONFLICT` — tanpa itu PostgreSQL error `42P10`.

## Kode utama

| File | Peran |
|------|--------|
| `ai/catalog_text.go` | Prompt + panggilan Haiku (`ExtractCatalogFromText`) |
| `business/catalog_text.go` | Preview / draft / commit endpoints |
| `business/catalog_image.go` | `parseCatalogVisionJSON`, tipe `CatalogImageDraftItem`, staging TTL |
| `business/catalog_text.go` | `commitCatalogDraftItems` — helper commit bersama image + text import |
| `importcsv/import.go` | CSV import — upsert dengan klausa partial index yang sama |

## Frontend

- Halaman: `/dashboard/catalog/import-text`
- Client: `lib/api/catalogText.ts`
- Draft table bersama import gambar: `components/catalog/catalog-import-draft-table.tsx`
- Editor deskripsi markdown: `components/catalog/description-rich-editor.tsx`

## Parsing respons AI

Reuse `parseCatalogVisionJSON` dari import gambar — format JSON sama (`parentTitle` + `items[]`). Model diinstruksikan **satu objek JSON** tanpa markdown.

## Troubleshooting

| Gejala | Penyebab umum | Solusi |
|--------|----------------|--------|
| `teks terlalu pendek` | &lt; 10 karakter | Tempel deskripsi produk yang lebih lengkap |
| `tidak ada produk terdeteksi` | Teks bukan daftar produk | Tambahkan nama, harga, atau varian eksplisit |
| `hasil AI tidak valid` | JSON rusak dari model | Coba ulang preview atau rapikan format teks |
| `draft import tidak ditemukan` | TTL 24 jam habis / job ID salah | Proses ulang dari preview |
| Error commit `42P10` | `ON CONFLICT` tanpa `WHERE deleted_at IS NULL` | Deploy kode terbaru (`commitCatalogDraftItems`) |
| `kuota token AI ... habis` | `ai_token` bulanan 0 | Tunggu periode baru atau upgrade paket |

## Uji manual

1. `encore run`, login owner.
2. Buka `/dashboard/catalog/import-text`, tempel teks produk (mis. caption Shopee dengan beberapa varian).
3. **Proses dengan AI** → edit baris di tabel draft (deskripsi pakai rich editor) → **Simpan ke katalog**.
4. `GET /usage/summary` → `ai_token.used` naik setelah preview, bukan setelah commit.
5. Commit ulang SKU sama → baris ter-update (bukan duplikat), karena partial unique index.
