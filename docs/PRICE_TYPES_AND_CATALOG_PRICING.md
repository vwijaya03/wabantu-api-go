# Tipe Harga & Harga Katalog per Kontak

Fitur master data **tipe harga** (umum, reseller, grosir, dll.) dan penetapan harga produk per tipe. Dipakai saat membuat pesanan manual berdasarkan tipe harga kontak.

---

## Tabel (tenant schema)

| Tabel | Keterangan |
|-------|------------|
| `business_price_type` | Label tipe harga (`code`, `label`, `is_default`, `is_system`) |
| `business_catalog_item_price` | Harga per `(catalog_item_id, price_type_id)` |
| `contact.price_type_id` | Opsional; `NULL` = pakai tipe default tenant |

Seed sistem (idempotent): `umum` (default), `reseller`.

---

## Endpoint

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/business/price-types?q=&page=&pageSize=` | auth |
| POST | `/api/v1/business/price-types` | owner |
| PATCH | `/api/v1/business/price-types/:id` | owner |
| DELETE | `/api/v1/business/price-types/:id` | owner (bukan `is_system`) |

Katalog (perubahan):

- `GET /api/v1/business/catalog?contactId=` — setiap item menyertakan `prices[]` dan `effectiveSellPrice` untuk tipe kontak (jika `contactId` diisi).
- `POST` / `PATCH` katalog — body boleh berisi `prices[]`; `sell_price` diselaraskan dengan harga tipe default.

Kontak:

- `POST` / `PATCH /api/v1/inbox/contacts` — field `priceTypeId` (opsional).

Pesanan:

- Create/update/batch — item dengan `catalogItemId` di-resolve ulang ke harga sesuai `contactId` (lihat `shared/pricing`).

---

## Index katalog & soft delete

Index unik SKU: `(source, external_code) WHERE deleted_at IS NULL`.

- Produk yang di-soft-delete tidak memblokir SKU yang sama.
- `POST` katalog: baris `manual` yang sudah dihapus dengan SKU sama akan **di-restore** alih-alih insert baru.

Patch index otomatis via `ensureCatalogIndexes` (dipanggil dari `ensurePricingSchema`).

---

## UI (web-frontend)

- `/dashboard/catalog/price-types` — CRUD tipe harga
- `/dashboard/catalog` — form harga per tipe; revamp tabel + Sheet (lihat [web-frontend/docs/CATALOG_MODULE.md](../../web-frontend/docs/CATALOG_MODULE.md))
- `/dashboard/catalog/import-image` — import screenshot AI ([CATALOG_IMAGE_IMPORT.md](./CATALOG_IMAGE_IMPORT.md))
- `/dashboard/catalog/import-text` — import teks/caption AI ([CATALOG_TEXT_IMPORT.md](./CATALOG_TEXT_IMPORT.md))
- `/dashboard/contacts` — pilih tipe harga (tanpa opsi duplikat “default” sintetis)
- `/dashboard/orders` — harga katalog mengikuti kontak terpilih

---

## Paket `shared/pricing`

Logika resolve harga dipisah dari `business` agar tidak terjadi import cycle (`business` ↔ `ai`).
