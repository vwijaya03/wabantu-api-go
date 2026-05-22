# Import katalog dari gambar (AI vision)

Fitur untuk tenant yang **tidak bisa export CSV** (mis. screenshot daftar produk Shopee/Tokopedia). Owner upload gambar → **Claude Haiku vision** mengekstrak SKU → **halaman konfirmasi** → simpan ke `business_catalog_item`.

## Model & kuota

| Langkah | Pakai AI? | Model | Kuota |
|---------|-----------|-------|-------|
| `POST .../import-image/preview` | Ya | `claude-haiku-4-5-20251001` (vision) | Mengurangi `ai_token` bulanan |
| `POST .../commit` | Tidak | — | — |

- Cek kuota: `usage.CheckQuota(tenantSchema, "ai_token")` sebelum preview.
- Pencatatan: `usage.RecordEvent(..., "ai_token", tokens)` + `usage.RecordAIActivity` dengan `PurposeCatalogImport`.
- Path aktivitas: `catalog_image_preview`.

## Batasan upload

| Aturan | Nilai |
|--------|--------|
| Ukuran maksimal per file | **5 MB** (`CatalogImageMaxBytes` = 5 MiB) |
| Total ukuran per batch | **20 MB** (`CatalogImageMaxBatchBytes`) |
| Ukuran minimal | **1 KB** |
| Format | JPG, JPEG, PNG, WEBP |
| File per request | Maks. **5** (`CatalogImageMaxFilesPerBatch`); field multipart `files` (atau satu `file` untuk kompatibilitas) |
| Item hasil AI per job | Maks. **50** baris (gabungan semua gambar; SKU duplikat digabung) |

Konstanta backend: `business/catalog_image.go`. Frontend: `web-frontend/lib/catalog-image-limits.ts` (harus sama angkanya).

## API (owner)

| Method | Path | Deskripsi |
|--------|------|-----------|
| GET | `/api/v1/business/catalog/import-image-limits` | Batasan upload (JSON) |
| POST | `/api/v1/business/catalog/import-image/preview` | `multipart` field `files` (atau `file`) → vision per gambar → merge → draft Redis |
| GET | `/api/v1/business/catalog/import-image/draft/:jobId` | Ambil draft |
| POST | `/api/v1/business/catalog/import-image/draft/:jobId/commit` | Body `{ items: [...] }` → insert DB |

Draft Redis key: `catalog:image:staging:{jobId}`, TTL **24 jam**.

## Mapping varian marketplace

Satu produk induk + banyak varian (L/XL/XXL) → **beberapa baris** `business_catalog_item`:

- `source` = `image_import`
- `external_code` = kode variasi / SKU
- `name` = judul + varian
- `ON CONFLICT (source, external_code) DO UPDATE`

## Kode utama

| File | Peran |
|------|--------|
| `ai/vision.go` | Vision + prompt JSON |
| `business/catalog_image.go` | API commit / limits / batch preview logic |
| `business/catalog_image_http.go` | Raw HTTP handler multipart batch preview |
| `ai/catalog_reply.go` | Balasan chat dari DB (bukan IG) — terpisah dari import |

## Frontend

- Halaman: `/dashboard/catalog/import-image`
- Client: `lib/api/catalogImage.ts`, validasi: `lib/catalog-image-limits.ts`
- Peringatan kuota AI di banner (copy + sisa `ai_token` dari `usage/summary`).

## Parsing respons AI

Screenshot Shopee sering berisi **beberapa produk induk** dalam satu gambar. Model kadang mengembalikan dua objek JSON berurutan (`{...}{...}`) — itu tidak valid untuk `json.Unmarshal` tunggal.

Backend (`decodeCatalogVisionPayloads` di `business/catalog_image.go`):

- Membaca **beberapa objek JSON** berurutan dengan `json.Decoder`.
- Mendukung juga array `[{...},{...}]`.
- Menggabungkan semua `items` ke satu draft (SKU duplikat di-skip).

Prompt vision (`ai/vision.go`) menegaskan: **satu objek JSON**, semua varian dalam satu array `items`, harga IDR tanpa titik ribuan.

## Troubleshooting

| Gejala | Penyebab umum | Solusi |
|--------|----------------|--------|
| `invalid character '{' after top-level value` | Dua+ objek JSON dari AI | Sudah ditangani decoder multi-objek; restart API setelah deploy |
| `tidak ada produk terdeteksi` | Gambar blur / bukan daftar produk | Upload ulang screenshot yang memuat tabel varian |
| `format gambar: JPG, PNG, atau WEBP` | Ekstensi/MIME tidak dikenali | Rename `.png`/`.jpg` atau pilih format yang didukung |
| `kuota token AI ... habis` | `ai_token` bulanan 0 | Tunggu periode baru atau upgrade paket |
| Pesan error generik `Bad Request` di UI | Raw handler memakai `errs.Error.Message` | Pastikan deploy terbaru (`catalogImageErrMessage`) |

## Uji manual

1. `encore run`, login owner.
2. Upload satu atau beberapa screenshot Shopee (≤ 5 file, ≤ 5 MB/file, ≤ 20 MB total).
3. **Proses dengan AI** → cek pratinjau (mis. varian `LETO-M`, `LETO-L` dari satu screenshot dua produk induk) → edit → **Simpan ke katalog**.
4. `GET /usage/summary` → `ai_token.used` naik setelah preview, bukan setelah commit.
