# Inventory & HPP Module — Dokumentasi Teknis (api-go)

Modul persediaan (stok), harga pokok penjualan (HPP/COGS), pergerakan stok, dan
revaluasi untuk tenant WABantu. Mendukung metode costing **FIFO / LIFO / Average**,
multi-gudang, bundle, Purchase Order → Bill, dan integrasi ke modul Finance.

> Status: **dalam pengembangan bertahap (PR-A1 … A10)**. Bagian bertanda _(rencana)_
> belum diimplementasikan; dokumen ini diperbarui tiap PR.
>
> Panduan produk (owner/CS): lihat `web-frontend/docs/INVENTORY_MODULE.md`.
> Pemetaan referensi Jubelio: lihat `docs/INVENTORY_JUBELIO_MAPPING.md` _(rencana)_.

---

## 1. Arsitektur & prinsip

### 1.1 Posisi dalam sistem

```
business_catalog_item (harga jual)  ──1:1 opsional──►  inv_sku (config stok per item)
                                                          │
inv_warehouse (gudang)  ──────────┐                       │
                                  ▼                        ▼
                         inv_stock_balance  ◄──────  inv_stock_movement (buku besar stok) (rencana A2)
                                  ▲                        │
                         inv_cost_layer (FIFO/LIFO) ◄──────┘ (rencana A2)
                                                           │
                              fin_transaction (COGS / Pembelian / Revaluasi) (rencana A6/A8)
```

### 1.2 Prinsip desain

1. **Semua pergerakan stok lewat satu buku besar** `inv_stock_movement` _(A2)_ —
   tidak ada perubahan saldo/HPP tanpa baris movement (mirip `item_movement` Jubelio).
2. **Cloud-safe migration**: modul ini **hanya membuat tabel baru** (`inv_*`).
   Tidak ada `ALTER` pada tabel inti (`business_catalog_item`, `"order"`), karena di
   Encore Cloud app DB role tidak boleh `ALTER` tabel yang bukan miliknya (SQLSTATE
   42501). Config per item disimpan di `inv_sku` (bukan kolom baru di katalog).
3. **Gate setup per tenant**: perilaku stok hanya aktif setelah `inv_setting.setup_completed = true`
   **dan** item punya `inv_sku.track_stock = true`. Tenant yang belum setup berperilaku
   persis seperti sebelum modul ini ada (order/finance tidak berubah).
4. **Aditif & idempotent**: semua DDL `CREATE TABLE/INDEX IF NOT EXISTS`. Aman dijalankan
   berulang; tidak pernah `DROP`/`RENAME` data lama.
5. **Costing sebagai pure function** _(A2)_: FIFO/LIFO/Average diuji unit tanpa DB.

### 1.3 Keputusan kunci (hasil interview)

| Topik | Keputusan |
|-------|-----------|
| Identitas SKU | `catalog_item_id` (tanpa tabel item terpisah); config di `inv_sku` |
| Qty | `NUMERIC(18,4)` — mendukung pcs **dan** pecahan (kg/gram) |
| Baris dokumen | tiap baris order/PO/Bill/Invoice punya `lineId` + `warehouseId` _(A5–A8)_ |
| Gudang | multi-gudang; default mirror Jubelio `location_id = -1`; **per-baris** |
| Pengurangan stok | saat order `processing`; tetap berkurang di `shipped`/`completed` |
| Cancel | stok naik kembali + invoice dihapus + transaksi finance dihapus _(A7/A8)_ |
| Retur | stok masuk dengan HPP **layer asli** (`source_movement_id`) _(A7)_ |
| Metode HPP | default per tenant + override per SKU; wizard AI; bisa diganti + recalculate _(A9)_ |
| Stok minus | default **diblok** (`inv_setting.block_negative_stock`) |
| Pembelian | PO → Bill (partial receive) _(A5/A6)_; adjustment ± untuk koreksi/opname _(A3)_ |

---

## 2. Tabel database (tenant schema `t_<slug>`)

### 2.1 Sudah ada (PR-A1)

| Tabel | Keterangan |
|-------|------------|
| `inv_setting` | Singleton per tenant: `setup_completed`, `default_costing_method` (fifo/lifo/average), `block_negative_stock`, `wizard_answers`, `wizard_recommendation` |
| `inv_warehouse` | Gudang. `is_default` (unik), `external_location_id` (−1 untuk default), `code` (unik, soft-delete aware) |
| `inv_sku` | Config inventory per `catalog_item_id`: `track_stock`, `is_bundle`, `costing_method` (override, nullable=inherit), `track_batch/serial/expiry`, `base_uom` |

Seed otomatis saat migrasi:
- 1 gudang default (`Gudang Utama`, `external_location_id = -1`).
- 1 baris `inv_setting` (`average`, blok stok minus).
- Kategori finance sistem: **HPP / COGS**, **Pembelian Persediaan**,
  **Penyesuaian Nilai Persediaan**, **Selisih Persediaan**.

### 2.2 Rencana (PR berikutnya)

| Tabel | PR | Keterangan |
|-------|----|------------|
| `inv_stock_movement` | A2 | Buku besar pergerakan + HPP per baris |
| `inv_stock_balance` | A2 | Snapshot `on_hand` + `avg_unit_cost` per (sku, gudang) |
| `inv_cost_layer` | A2 | Layer FIFO/LIFO (qty sisa, unit cost, batch, expiry) |
| `inv_bundle_component` | A4 | Bundle → komponen SKU anak |
| `pur_purchase_order(_line)` | A5 | Purchase Order (partial receive) |
| `pur_bill(_line)` | A6 | Bill/GRN → stok masuk + finance |
| `inv_invoice(_line)` | A7 | Invoice penjualan + COGS per baris |
| `inv_sales_return(_line)` | A7 | Retur penjualan |
| `inv_document_sequence` | A5 | Nomor dokumen per tenant (WPO/WBIL/WINV/WRET) |

---

## 3. Migrasi & keamanan database

### 3.1 Cara kerja

- DDL didefinisikan sekali di `shared/tenantschema/inventory_schema_sql.go`
  (`InventorySchemaSQL`).
- `tenant.runInventorySchemaAndSeed` dipanggil dari rantai `RunSchemaPatches`
  (setelah finance & events). Idempotent: cek `tenantschema.InventoryModuleReady`,
  jalankan DDL bila belum ada, lalu seed.
- Tenant **baru** dapat modul saat dibuat (`RunTenantDDL` → `RunSchemaPatches`).
- Tenant **lama** dapat modul saat `migrate-schemas` dipanggil / login berikutnya.

### 3.2 Encore Cloud

Lokal otomatis. Untuk cloud, jalankan (admin) bila perlu:

```bash
./scripts/apply-inventory-schema-cloud.sh staging
```

Script men-dump `InventorySchemaSQL` (`scripts/cmd/cloud-inventory-patch-sql`)
dan menerapkannya ke semua schema `t_*` dengan role admin.

---

## 4. Endpoint (PR-A1)

| Method | Path | Akses | Fungsi |
|--------|------|-------|--------|
| GET | `/api/v1/inventory/setting` | tenant | Status setup + metode default + jumlah gudang |
| PATCH | `/api/v1/inventory/setting` | owner | Ubah metode default / blok stok minus |
| POST | `/api/v1/inventory/setup/complete` | owner | Tandai setup selesai (buka fitur stok) |
| GET | `/api/v1/inventory/warehouses` | tenant | Daftar gudang |
| POST | `/api/v1/inventory/warehouses` | owner | Tambah gudang |
| PATCH | `/api/v1/inventory/warehouses/:id` | owner | Ubah gudang |
| DELETE | `/api/v1/inventory/warehouses/:id` | owner | Hapus gudang (default tidak bisa dihapus) |

> ACL granular (staff read-only, dll.) ditambahkan di **A9**. Saat ini tulis =
> owner/super_admin (`CanPerformOwnerActions`).

---

## 5. Costing engine _(rencana A2)_

## 6. Pergerakan manual: adjustment / transfer / opname / revaluasi _(rencana A3)_

## 7. Bundle _(rencana A4)_

## 8. Purchase Order _(rencana A5)_

## 9. Bill / penerimaan barang _(rencana A6)_

## 10. Invoice & retur penjualan _(rencana A7)_

## 11. Integrasi order + COGS _(rencana A8)_

## 12. Recalculate HPP, wizard AI, ACL _(rencana A9)_

## 13. AI WhatsApp stok real + backfill order lama _(rencana A10)_
