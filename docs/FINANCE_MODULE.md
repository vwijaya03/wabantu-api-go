# Finance Module — Dokumentasi Teknis (api-go)

Modul keuangan operasional untuk tenant WABantu.  
**Bukan** sistem akuntansi penuh — tidak ada double-entry, jurnal ledger, atau tax engine.

Target: UMKM, toko kecil, bisnis keluarga.

> Panduan produk (untuk CS/sales/owner): lihat `web-frontend/docs/FINANCE_MODULE.md`.

---

## Daftar layanan & file

| File | Isi |
|------|-----|
| `finance/finance.go` | Wallet, kategori, transaksi (CRUD + duplikat), approval, period lock, audit log, dashboard summary |
| `finance/helpers.go` | Refresh saldo, visibility staff, approval config, parse `TEXT[]` tags |
| `finance/transaction_types.go` | CRUD jenis transaksi (`fin_transaction_type`) |
| `finance/budget.go` | Anggaran per kategori + sisa + alert, category spending, monthly comparison |
| `finance/investment.go` | Aset investasi, trade beli/jual, dividen per aset, harga manual, portfolio (P&L) |
| `finance/investment_units.go` | Default satuan & multiplier per tipe aset (lot/lembar, gram, unit, koin) |
| `finance/recurring.go` | Transaksi berulang CRUD + cron harian 07:00 WIB |
| `finance/checklist.go` | Checklist harian + template CRUD dasar |
| `finance/checklist_billing.go` | Tagihan bulanan per periode, toggle checkbox, auto-post transaksi expense per item |
| `finance/checklist_clone.go` | Clone `fin_recurring` (bulanan + expense) → template tagihan bulanan |
| `finance/transaction_image.go` | Import transaksi dari screenshot (AI vision + staging Redis + commit) |
| `finance/transaction_image_http.go` | Raw multipart handler preview import transaksi |
| `aivision/vision.go` | Vision Haiku tanpa import cycle `ai` → `order` → `finance` |
| `finance/report.go` | Export laporan CSV/PDF async, progress job, batching all-time, data URL download |
| `finance/order_income.go` | Pemasukan otomatis dari pesanan `completed`; hapus saat `draft`/`cancelled` |
| `tenant/finance_seed.go` | Seed kategori + jenis transaksi default + wallet Kas Tunai |

---

## Tabel database (tenant schema `t_<slug>`)

| Tabel | Keterangan |
|-------|------------|
| `fin_wallet` | Kas, bank, e-wallet, kripto, investasi |
| `fin_wallet_balance` | Snapshot saldo materialized (recalculate saat transaksi berubah) |
| `fin_category` | Kategori + sub-kategori (seeded otomatis) |
| `fin_transaction_type` | Label & flow jenis transaksi (seed + CRUD owner) |
| `fin_transaction` | Semua transaksi (append-only, soft delete); kolom `asset_*` untuk investasi |
| `fin_asset` | Definisi aset investasi (`unit_multiplier`, `price_unit_name` untuk saham IDX) |
| `fin_asset_price` | Riwayat harga manual per aset |
| `fin_recurring` | Konfigurasi transaksi berulang |
| `fin_recurring_log` | Log eksekusi cron per recurring |
| `fin_budget` | Anggaran per kategori per periode |
| `fin_checklist_template` | Template checklist harian/bulanan |
| `fin_checklist_item` | Instance checklist per tanggal |
| `fin_approval_setting` | Konfigurasi workflow persetujuan |
| `fin_period_lock` | Kunci periode (YYYY-MM) — blokir edit/hapus |
| `fin_audit_log` | Riwayat perubahan (append-only, tidak bisa dihapus) |
| `fin_report_job` | Job export async (status: processing → done/failed), `params.format` menyimpan `csv`/`pdf`, `error_msg` dipakai untuk progress/error |

---

## Endpoint ringkas

### Dashboard & Wallet

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/finance/dashboard?period=YYYY-MM` | owner + staff |
| GET | `/api/v1/finance/wallets` | owner (semua) / staff (visibility=all) |
| POST | `/api/v1/finance/wallets` | owner |
| PUT | `/api/v1/finance/wallets/:id` | owner |
| DELETE | `/api/v1/finance/wallets/:id` | owner (ditolak jika masih ada transaksi/aset/recurring terkait) |

### Kategori

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/finance/categories` | semua |
| POST | `/api/v1/finance/categories` | owner |
| DELETE | `/api/v1/finance/categories/:id` | owner (non-system) |

### Transaksi

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/finance/transactions` | owner (semua) / staff (approved + pending milik sendiri); query `search`, `period`, `page` |
| POST | `/api/v1/finance/transactions` | semua — status tergantung `approval_setting` |
| PUT | `/api/v1/finance/transactions/:id` | owner / staff (draft/pending milik sendiri) |
| DELETE | `/api/v1/finance/transactions/:id` | owner (jika periode tidak terkunci) |
| POST | `/api/v1/finance/transactions/duplicate` | semua |
| POST | `/api/v1/finance/transactions/approve` | owner |
| GET/POST | `/api/v1/finance/transactions/import-image/*` | owner — screenshot → AI → konfirmasi → bulk insert; lihat [TRANSACTION_IMAGE_IMPORT.md](./TRANSACTION_IMAGE_IMPORT.md) |

### Budget & Laporan

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/finance/budgets?period=YYYY-MM` | semua |
| POST | `/api/v1/finance/budgets` | owner |
| DELETE | `/api/v1/finance/budgets/:id` | owner |
| GET | `/api/v1/finance/budgets/summary` | semua |
| GET | `/api/v1/finance/reports/category-spending` | semua |
| GET | `/api/v1/finance/reports/monthly-comparison?months=6` | semua |

**Export CSV/PDF:** `POST /finance/reports/export` menerima `type=monthly|custom|all_time`, `format=csv|pdf`, `period=YYYY-MM` untuk bulanan, atau `startDate`/`endDate` untuk custom. Backend membuat `fin_report_job` dengan status `processing`, lalu worker background memuat transaksi per batch 250 baris, mengisi progress seperti `Memuat transaksi... 250 baris`, membuat CSV/PDF, dan menyimpan `download_url` sebagai `data:` URL.

All-time export tidak memakai satu query besar; loader memakai batch + `statement_timeout` 15 detik supaya query lambat gagal eksplisit, bukan stuck `processing`. Index `idx_fin_txn_export(status, transaction_date DESC, created_at DESC) WHERE deleted_at IS NULL` wajib ada di tenant schema untuk mempercepat report all-time.

### Jenis transaksi (konfigurasi)

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/finance/transaction-types` | owner |
| POST | `/api/v1/finance/transaction-types` | owner |
| PUT | `/api/v1/finance/transaction-types/:id` | owner |
| DELETE | `/api/v1/finance/transaction-types/:id` | owner |

### Investasi

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/finance/investments/portfolio` | owner; query `search`, `page`, `pageSize` |
| POST | `/api/v1/finance/investments/assets` | owner |
| PUT | `/api/v1/finance/investments/assets/:id` | owner |
| DELETE | `/api/v1/finance/investments/assets/:id` | owner (ditolak jika qty > 0) |
| POST | `/api/v1/finance/investments/assets/:id/trades` | owner — body: `side`, `quantity`, `pricePerUnit`, `fee` atau `feePercent` |
| POST | `/api/v1/finance/investments/assets/:id/dividends` | owner — body: `amount`, `transactionDate`, `description?`; insert `type=dividend` + `asset_id` |
| GET | `/api/v1/finance/investments/assets/:id/trades` | owner (beli/jual/dividen terkait aset) |
| DELETE | `/api/v1/finance/investments/assets/:id/trades/:txnId` | owner |
| POST | `/api/v1/finance/investments/prices` | owner |
| GET | `/api/v1/finance/investments/assets/:id/prices` | owner |

**Satuan default saat create aset** (`investment_units.go`):

| Tipe | `unit_name` | `unit_multiplier` | `price_unit_name` |
|------|-------------|-------------------|-------------------|
| `stock` | `lot` | 100 | `lembar` |
| `gold` | `gram` | 1 | `gram` |
| `mutual_fund` | `unit` | 1 | `unit` |
| `crypto` | `coin` | 1 | `coin` |
| `other` | `unit` | 1 | `unit` |

**Saham IDX (lot):** qty di `asset_qty` = jumlah **lot**; nilai transaksi = `lot × multiplier × harga_per_lembar ± biaya`.

**Dividen:** `totalDividend` di portfolio = `SUM(amount)` transaksi `type=dividend` + `asset_id` + `status=approved`.  
`POST /finance/transactions` **tidak** mengisi `asset_id` — dividen harus lewat endpoint dividen di atas.

**Seed jenis transaksi:** `investment_buy`, `investment_sell`, `dividend` → `show_in_quick=false` (UI Catat Transaksi tidak menampilkan beli/jual/dividen).

### Recurring

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/finance/recurring` | owner |
| POST | `/api/v1/finance/recurring` | owner |
| DELETE | `/api/v1/finance/recurring/:id` | owner |

### Checklist & tagihan bulanan

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/finance/checklist/today` | semua — checklist harian (frekuensi `daily` + bulanan jatuh tempo hari ini) |
| POST | `/api/v1/finance/checklist/action` | semua — `done` / `skip` (tanpa auto-transaksi) |
| GET | `/api/v1/finance/checklist/templates` | semua — daftar template aktif (legacy, tanpa pagination) |
| GET | `/api/v1/finance/checklist/templates/manage` | owner — CRUD list: `q`, `page`, `pageSize`, `frequency`, `activeOnly` |
| POST | `/api/v1/finance/checklist/templates` | owner — default `frequency=monthly`; bulanan wajib `amountHint` + `dueDate` (YYYY-MM-DD) |
| PATCH | `/api/v1/finance/checklist/templates/:id` | owner |
| DELETE | `/api/v1/finance/checklist/templates/:id` | owner — soft (`is_active=false`) |
| GET | `/api/v1/finance/checklist/monthly?period=YYYY-MM` | semua — checklist tagihan bulanan (auto-upsert item per template) |
| POST | `/api/v1/finance/checklist/monthly/toggle` | semua — body `{ itemId, checked }`; centang → auto-post `expense` per item (uncheck → hapus transaksi terkait) |
| POST | `/api/v1/finance/checklist/clone-from-recurring` | owner — body `{ recurringIds[] }` → template tagihan bulanan dari `fin_recurring` (bulanan + expense) |

**Pola data (best practice):**

- **Template** (`fin_checklist_template`, `frequency=monthly`) = master tagihan tetap (judul, nominal, dompet/kategori opsional, `due_anchor_date` + `day_of_month` 1–31).
- **Instance** (`fin_checklist_item`, `due_date` = tanggal jatuh tempo di bulan tersebut) = checklist per bulan; unik `(template_id, due_date)`.
- **Checkbox** = `status` `pending` ↔ `done`; setiap centang langsung memicu auto-post (jika nominal valid).
- **Auto-catat transaksi** setiap item yang `done` dan belum punya transaksi (langsung saat dicentang, tidak perlu menunggu semua tagihan selesai) — satu transaksi `expense` per item, `reference_no=checklist:<itemId>` (idempoten), `transaction_date=due_date`, tag `checklist-billing`. Sinkron ulang juga dijalankan saat `GET /checklist/monthly` (repair + post sisa yang belum tercatat).
- **Uncheck** menghapus (soft-delete) transaksi terkait dan mengosongkan `transaction_id`.
- Badge **Tercatat** hanya jika transaksi masih ada (bukan terhapus).
- Periode terkunci (`fin_period_lock`) memblokir batch post sama seperti transaksi manual.

**Koneksi DB:** Satu `*sql.Conn` tenant hanya satu query aktif. Jangan memanggil query lain (termasuk `financeNow` untuk timezone) selama cursor `Rows` masih terbuka — selesaikan iterasi lalu `Rows.Close()` sebelum query berikutnya.

### Lainnya

| Method | Path | Role |
|--------|------|------|
| GET/PUT | `/api/v1/finance/approval-setting` | owner |
| POST | `/api/v1/finance/period-lock` | owner |
| GET | `/api/v1/finance/locked-periods` | owner |
| GET | `/api/v1/finance/audit-log` | owner |
| POST | `/api/v1/finance/reports/export` | semua |
| GET | `/api/v1/finance/reports/jobs/:id` | semua |
| GET | `/api/v1/finance/reports/jobs` | semua |

---

## Arsitektur saldo wallet

Saldo **tidak** disimpan sebagai kolom mutable di `fin_wallet`. Pola yang dipakai:

1. Setiap transaksi di-insert ke `fin_transaction` (append-only).
2. `refreshWalletBalance(ctx, conn, walletID)` dipanggil setelah setiap perubahan — kalkulasi ulang dari semua transaksi `approved`.
3. Hasil disimpan di `fin_wallet_balance` via `INSERT ... ON CONFLICT DO UPDATE`.
4. Concurrent update diamankan oleh connection pool Postgres — setiap `TenantConn` memakai koneksi terpisah.

Keuntungan: saldo bisa di-replay dari history, tidak ada race condition, rollback cukup soft-delete transaksi.

---

## Cron recurring

```
Jadwal: 00:00 UTC = 07:00 WIB (setiap hari)
Job: "finance-recurring"
```

Alur per tenant:
1. Query semua `fin_recurring` aktif dengan `next_run_date <= TODAY`.
2. Jika `mode=auto` → insert `fin_transaction` status `approved` + refresh balance.
3. Jika `mode=reminder` → catat di `fin_recurring_log` status `reminded` (notifikasi dihandle service lain).
4. Hitung `next_run_date` berikutnya (`calcNextRunDate`).
5. Jika 3 kegagalan berturut-turut → pause otomatis (`is_active=false`).

---

## Period lock

Owner bisa mengunci periode `YYYY-MM` via `POST /finance/period-lock`.  
Setelah dikunci, semua create/edit/delete transaksi di periode itu **diblokir** di level API (bukan hanya UI).

```go
// Cek di setiap endpoint yang memodifikasi transaksi
locked, _ := periodLocked(ctx, conn, period)
if locked {
    return nil, appErrs.Forbidden("periode sudah dikunci")
}
```

---

## Approval workflow

Dikonfigurasi per tenant via `fin_approval_setting`:

| Setting | Perilaku |
|---------|----------|
| `enabled=false` | Semua transaksi langsung `approved` |
| `enabled=true, amount_threshold=null` | Semua transaksi staff → `pending_approval` |
| `enabled=true, amount_threshold=500000` | Transaksi staff ≥ Rp 500.000 → `pending_approval` |

Owner selalu `approved` langsung. `adjustment` selalu owner-only.

---

## Seed default saat tenant baru

`tenant/finance_seed.go` dipanggil dari `RunTenantDDL` **dan** setelah `runFinanceSchemaAndSeed` pada migrasi tenant lama:

- Kategori sistem: Pemasukan (4 sub), Pengeluaran (8 sub), Investasi (6 sub)
- Wallet default: **Kas Tunai** (type=cash, visibility=all)
- Approval setting: disabled

Idempoten — skip jika sudah ada data.

## Migrasi tenant yang sudah ada

Tabel `fin_*` **tidak** otomatis muncul hanya karena deploy kode. Jalankan sekali:

```bash
encore exec ./cmd/migrate-tenant-schemas
```

atau `POST /api/v1/admin/migrate-tenant-schemas` (super_admin).  
Patch di `tenant/finance_schema.go`; index `to_char(...)` pada periode **tidak** dipakai (bukan IMMUTABLE di PostgreSQL).

---

## Integrasi pesanan (penjualan)

Saat pesanan berstatus **`completed`** (create, update, atau batch status):

- Insert transaksi `income` **approved** ke dompet default (biasanya Kas Tunai).
- Kategori sistem **Penjualan Produk** bila ada.
- `reference_no` = UUID pesanan (idempoten — tidak dobel jika di-update ulang ke completed).

Saat status kembali **`draft`** atau **`cancelled`** (atau cancel endpoint):

- Soft-delete transaksi `income` dengan `reference_no` tersebut.
- Refresh saldo dompet terkait.

Implementasi: `finance/order_income.go`, dipanggil dari `order/order.go`.

---

## Changelog

| Tanggal | Catatan |
|---------|---------|
| 2026-05-29 | Fix deadlock `*sql.Conn`: jangan panggil query (mis. `financeNow`) saat `Rows` masih terbuka — `templates/manage` timeout ~30s / `context canceled` |
| 2026-05-27 | Tagihan bulanan: checklist per `YYYY-MM`, toggle checkbox, auto-post expense saat semua selesai; template CRUD + search/pagination; `due_anchor_date` + input tanggal |
| 2026-05-27 | Pemasukan otomatis dari pesanan selesai; hapus saat draft/dibatalkan |
| 2026-05 | Modul finance awal — wallet, transaksi, anggaran, investasi, recurring, checklist, laporan |
| 2026-05-24 | Production hardening: `fin_transaction_type`, trade investasi, lot×100, biaya %, list transaksi + search, hapus dompet/aset dengan guard, dedupe kategori tenant, fix scan `tags` TEXT[] |
| 2026-05-25 | Dividen per aset (`POST .../dividends`), satuan per tipe aset + migrasi lot→gram/unit/coin, beli/jual/dividen off quick picker, wallet update type guard |
