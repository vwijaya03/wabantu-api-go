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
| `inv_cost_layer` | (A2) Layer biaya FIFO/LIFO: `qty_remaining`, `unit_cost`, `batch_no`, `expiry_date`, `source_movement_id` |
| `inv_stock_balance` | (A2) Snapshot per (item, gudang): `on_hand`, `reserved`, `avg_unit_cost`, `total_value` |
| `inv_stock_movement` | (A2) Buku besar append-only: tiap operasi stok = 1 baris (qty, unit_cost, total_cost/COGS, qty_after, ref dokumen, source_movement_id) |

Seed otomatis saat migrasi:
- 1 gudang default (`Gudang Utama`, `external_location_id = -1`).
- 1 baris `inv_setting` (`average`, blok stok minus).
- Kategori finance sistem: **HPP / COGS**, **Pembelian Persediaan**,
  **Penyesuaian Nilai Persediaan**, **Selisih Persediaan**.

### 2.2 Rencana (PR berikutnya)

| Tabel | PR | Keterangan |
|-------|----|------------|
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
| GET | `/api/v1/inventory/stock` | tenant | (A2) Saldo stok per item/gudang (on_hand, available, avg, nilai) |
| GET | `/api/v1/inventory/movements` | tenant | (A2) Buku besar/kartu stok (filter item, gudang, tipe) |
| POST | `/api/v1/inventory/adjustments` | owner | (A3) Penyesuaian stok ± |
| POST | `/api/v1/inventory/transfers` | owner | (A3) Transfer antar gudang |
| POST | `/api/v1/inventory/opening-balance` | owner | (A3) Saldo awal (bulk) |
| POST | `/api/v1/inventory/revaluations` | owner | (A3) Revaluasi HPP |
| GET | `/api/v1/inventory/bundles/:id/components` | tenant | (A4) Komponen bundle |
| PUT | `/api/v1/inventory/bundles/:id/components` | owner | (A4) Set komponen bundle |
| POST | `/api/v1/inventory/purchase-orders` | owner | (A5) Buat PO |
| GET | `/api/v1/inventory/purchase-orders` | tenant | (A5) Daftar PO |
| GET | `/api/v1/inventory/purchase-orders/:id` | tenant | (A5) Detail PO |
| POST | `/api/v1/inventory/purchase-orders/:id/close` | owner | (A5) Tutup PO |
| POST | `/api/v1/inventory/purchase-orders/:id/cancel` | owner | (A5) Batalkan PO |
| POST | `/api/v1/inventory/bills` | owner | (A6) Terima barang (GRN) |
| GET | `/api/v1/inventory/bills` | tenant | (A6) Daftar bill |
| GET | `/api/v1/inventory/bills/:id` | tenant | (A6) Detail bill |

> ACL granular (staff read-only, dll.) ditambahkan di **A9**. Saat ini tulis =
> owner/super_admin (`CanPerformOwnerActions`).

---

## 5. Costing engine (PR-A2)

### 5.1 Buku besar = sumber kebenaran

Semua perubahan stok lewat `PostMovement` (`inventory/movement.go`), dijalankan
**di dalam transaksi caller** (`*sql.Tx` dengan `search_path` tenant), sehingga
satu Bill/Order bisa mempost beberapa movement + transaksi finance secara atomik.

Tiap operasi menulis **1 baris** `inv_stock_movement` (qty selalu positif, arah
`in`/`out`), memutakhirkan `inv_stock_balance`, dan untuk FIFO/LIFO memutakhirkan
`inv_cost_layer`.

### 5.2 Tiga metode

| Metode | Stok masuk (`in`) | Stok keluar (`out`) |
|--------|-------------------|---------------------|
| **AVERAGE** | `avg = (onHand·avg + qty·cost) / (onHand+qty)`; tanpa layer | COGS = `avg · qty`; avg tetap |
| **FIFO** | buat `inv_cost_layer` baru | `planConsumption` ambil layer terlama dulu |
| **LIFO** | buat `inv_cost_layer` baru | `planConsumption` ambil layer terbaru dulu |

Metode efektif per SKU = `inv_sku.costing_method` (override) → `inv_setting.default_costing_method` → `average`.

### 5.3 Pure functions (teruji unit, tanpa DB)

`inventory/costing_engine.go`:
- `planConsumption(layers, qty)` → draw per-layer, total COGS, shortfall
- `applyReceiptAverage`, `issueCostAverage`, `weightedUnitCost`
- `applyIn` / `applyOut` (transisi snapshot saldo)

COGS dihitung **eksak** (`total_cost`); `unit_cost` baris = `total_cost / qty`.

### 5.4 Stok minus

Jika `inv_setting.block_negative_stock = true` (default), `out` yang melebihi
on-hand ditolak (`stok tidak cukup`). Jika dimatikan, stok boleh minus dan porsi
tanpa layer di-cost pakai average terkini (estimasi terbaik).

### 5.5 Catatan

- FIFO mengurut `received_at` (FEFO/expiry-first menyusul bila dibutuhkan).
- Presisi `NUMERIC(18,4)` untuk qty/cost; pembulatan 4 desimal di engine,
  finance posting dibulatkan 2 desimal (A6/A8).
- Endpoint baca: `GET /inventory/stock`, `GET /inventory/movements` (kartu stok).

## 6. Pergerakan manual (PR-A3)

Semua endpoint berikut **owner-only**, jalan dalam satu transaksi (movement engine),
dan posting finance bersifat idempotent (reference_no = movement id).

| Operasi | Endpoint | Movement | Finance |
|---------|----------|----------|---------|
| **Adjustment ±** | `POST /inventory/adjustments` | `adjustment_plus` / `adjustment_minus` | minus → expense **Selisih Persediaan** |
| **Transfer antar gudang** | `POST /inventory/transfers` | `transfer_out` + `transfer_in` (cost pass-through) | — |
| **Saldo awal (bulk)** | `POST /inventory/opening-balance` | `opening_balance` (in) | — |
| **Revaluasi HPP** | `POST /inventory/revaluations` | `revaluation_cost` (qty 0) | naik → income / turun → expense **Penyesuaian Nilai Persediaan** |

Catatan:
- **Adjustment** signed qty: `+` menambah (butuh `unitCost`), `−` mengurangi (HPP dihitung
  FIFO/LIFO/average). Pengurangan dicek period-lock dulu (fail-fast) lalu posting expense.
- **Opening balance** menerima array (CSV di-parse di frontend → array). Otomatis
  membuat `inv_sku` (track_stock) untuk item yang belum dilacak — inilah cara mulai melacak stok.
- **Transfer**: `transfer_out` menghitung HPP dari gudang asal; `transfer_in` memakai
  HPP yang sama (pass-through), jadi nilai persediaan total tidak berubah.
- **Revaluasi**: qty tetap, hanya HPP berubah. Untuk FIFO/LIFO, unit_cost semua layer
  sisa diskalakan proporsional agar konsisten dengan total baru. Selisih → jurnal finance.
- Stok minus tetap diblok bila `block_negative_stock = true`.

Helper finance: `finance.RecordInventoryEntry` / `RemoveInventoryEntry` (idempotent per reference_no).

## 7. Bundle (PR-A4)

Bundle = item katalog yang dijual sebagai paket; stok diambil dari **SKU anak**
(ala Jubelio "ngambil stok anakan terkecil"). Tabel `inv_bundle_component`
(`parent_catalog_item_id`, `child_catalog_item_id`, `qty` per 1 bundle).

| Endpoint | Akses | Fungsi |
|----------|-------|--------|
| `GET /inventory/bundles/:catalogItemID/components` | tenant | Daftar komponen bundle |
| `PUT /inventory/bundles/:catalogItemID/components` | owner | Set komponen (replace); `[]` kosong = batalkan bundle |

Aturan:
- Parent bundle ditandai `inv_sku.is_bundle = true` dan **tidak menyimpan stok sendiri**
  (`track_stock = false`); stok berasal dari komponen.
- Set komponen kosong → `is_bundle = false`, `track_stock = true` (kembali jadi item biasa).
- Komponen tidak boleh: dirinya sendiri, duplikat, qty ≤ 0, atau berupa bundle
  (bundle bertingkat belum didukung di v1).
- Pure function untuk dipakai order flow (A8): `explodeBundle(components, bundleQty)`
  → daftar issue per anak; `bundleAvailableQty(onHandByChild, components)` → jumlah
  bundle yang bisa dipenuhi (min floor antar komponen).

## 8. Purchase Order (PR-A5)

PO = **rencana pembelian**; tidak mengubah stok/finance (itu terjadi di Bill/A6).
Nomor `WPO-000001` via `inv_document_sequence` (atomik per tenant).

| Endpoint | Akses | Fungsi |
|----------|-------|--------|
| `POST /inventory/purchase-orders` | owner | Buat PO + baris (per-baris gudang) |
| `GET /inventory/purchase-orders` | tenant | Daftar (filter status, q) |
| `GET /inventory/purchase-orders/:id` | tenant | Detail + baris |
| `POST /inventory/purchase-orders/:id/close` | owner | Tutup sisa (open/partial → closed) |
| `POST /inventory/purchase-orders/:id/cancel` | owner | Batalkan (hanya bila belum ada penerimaan) |

- Baris: `qty_ordered`, `qty_received` (diisi Bill), `unit_cost`, `warehouse_id` per baris.
- Status: `open → partial → received` (oleh Bill), atau `closed`/`cancelled` manual.
- Pure-function: `formatDocNumber`, `poStatusFromReceipts` (dipakai Bill A6).

## 9. Bill / penerimaan barang (PR-A6)

Bill = penerimaan barang (GRN). Menambah stok via `PostMovement(purchase_receive)`
per baris, dan (opsional) posting finance. Nomor `WBIL-000001`.

| Endpoint | Akses | Fungsi |
|----------|-------|--------|
| `POST /inventory/bills` | owner | Terima barang (boleh standalone atau dari PO) |
| `GET /inventory/bills` | tenant | Daftar bill |
| `GET /inventory/bills/:id` | tenant | Detail + baris |

- **Partial receive**: tiap baris boleh kaitkan `purchaseOrderLineId` → `qty_received`
  PO bertambah; status PO dihitung ulang (`open → partial → received`).
- **Per-baris gudang**, batch, expiry didukung.

### Toggle pengakuan biaya (`inv_setting.purchase_posts_expense`)

Karena Finance WABantu berbasis arus kas (tanpa akun aset), biaya diakui **di satu titik**
untuk menghindari dobel:

| Mode | `purchase_posts_expense` | Saat Bill | Saat jual (A8) |
|------|--------------------------|-----------|----------------|
| **Akrual** (default) | `false` | nilai persediaan naik, **tanpa** expense | expense **HPP / COGS** |
| **Cashflow** | `true` | expense **Pembelian Persediaan** (kas keluar) | **tanpa** COGS |

Owner mengubah via `PATCH /inventory/setting`. Default = akrual (laba-rugi akurat).
Pembayaran supplier (AP) detail menyusul sebagai enhancement.

## 10. Invoice & retur penjualan _(rencana A7)_

## 11. Integrasi order + COGS (PR-A8)

Modul order memanggil `inventory.SyncOrderStock` (idempotent, **gated** `setup_completed`)
setiap kali status/baris pesanan berubah. Reconcile berbasis _desired state_:

| Status pesanan | Efek stok |
|----------------|-----------|
| committed (processing/shipped/completed/paid/confirmed) | stok **dikeluarkan** (`sale_issue`) sebanyak kebutuhan |
| draft / cancelled | stok **dikembalikan** (`sale_cancel_restore`) |

- **Idempotent**: hitung selisih `required − net_issued` per (item, gudang); hanya
  movement selisih yang dibuat. Aman dipanggil berulang (processing→shipped tidak dobel potong).
- **Bundle**: otomatis di-explode ke SKU anak saat reconcile.
- **Per-baris gudang**: tiap baris pesanan punya `warehouseId` (default = gudang utama)
  dan `lineId` (UUID) untuk telusur movement.
- **Qty pecahan**: `order.items.qty` kini `float64` (dukung kg/gram); modul AI tetap
  pakai qty bulat internal lalu konversi.
- **Block oversell**: sebelum commit transisi ke committed, `PrecheckOrderStock` menolak
  bila `block_negative_stock` dan stok kurang (fail-fast, pesanan tidak jadi pindah status).
- **COGS** (mode akrual): `resyncOrderCOGS` menyetel expense **HPP / COGS** = net COGS
  movement penjualan (idempotent, ref `cogs:<orderId>`). Mode cashflow: tidak ada COGS.
- **Cancel/Delete cascade**: stok kembali + income pesanan dihapus (existing) + COGS dihapus.

Tidak ada tabel/endpoint baru — perubahan pada `order` service + helper `inventory/order_sync.go`.
Tenant tanpa `setup_completed` → perilaku order **persis seperti sebelumnya**.

## 12. Recalculate HPP, wizard AI, ACL _(rencana A9)_

## 12. Recalculate HPP, wizard AI, ACL _(rencana A9)_

## 13. AI WhatsApp stok real + backfill order lama _(rencana A10)_
