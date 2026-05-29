# Import transaksi dari gambar (AI vision)

Fitur untuk mencatat banyak transaksi sekaligus dari screenshot halaman **Transaksi** (WABantu) atau daftar mutasi serupa. Owner upload gambar → **Claude Haiku vision** mengekstrak baris → **halaman konfirmasi** → simpan ke `fin_transaction`.

## Model & kuota

| Langkah | Pakai AI? | Model | Kuota |
|---------|-----------|-------|-------|
| `POST .../import-image/preview` | Ya | `claude-haiku-4-5-20251001` | Mengurangi `ai_token` bulanan |
| `POST .../commit` | Tidak | — | — |

- Purpose aktivitas: `transaction_import` (`usage.PurposeTransactionImport`).
- Vision code: `aivision` package (hindari import cycle `finance` → `ai` → `order` → `finance`).
- Secret: `AnthropicAPIKey` di struct `secrets` pada package `finance` (nama struct harus `secrets`, sama seperti `business`). Set via `encore secret set --type local AnthropicAPIKey` — nilai global, dipakai juga import katalog.

## Deteksi pemasukan vs pengeluaran

AI + fallback backend memakai petunjuk berikut (digabung):

| Petunjuk | Pemasukan (`income`) | Pengeluaran (`expense`) |
|----------|----------------------|-------------------------|
| WABantu UI | Ikon hijau, tanda **+**, nominal hijau | Ikon merah, tanda **−**, nominal merah |
| Awalan nominal | `+` / tanpa minus | `−` / kurung |
| Label | Pemasukan, Masuk, Terima, CR (bank) | Pengeluaran, Keluar, Bayar, DB (bank) |
| `typeSignals` | `green_amount`, `plus_prefix`, … | `red_amount`, `minus_prefix`, … |

User dapat mengubah jenis di halaman konfirmasi sebelum commit.

## Batasan upload

Sama dengan import katalog: 5 MB/file, 20 MB/batch, 5 file, 50 baris/job. Konstanta di `finance/transaction_image.go`.

## API (owner)

| Method | Path |
|--------|------|
| GET | `/api/v1/finance/transactions/import-image-limits` |
| POST | `/api/v1/finance/transactions/import-image/preview` (multipart `files`) |
| GET | `/api/v1/finance/transactions/import-image/draft/:jobId` |
| POST | `/api/v1/finance/transactions/import-image/draft/:jobId/commit` |

Redis key: `finance:txn:image:staging:{jobId}`, TTL 24 jam.

Commit: `reference_no=imgimport:{jobId}:{draftKey}` (idempoten), tag `image-import`, status `approved`.

## Troubleshooting

| Gejala | Penyebab | Solusi |
|--------|----------|--------|
| `kunci Anthropic belum dikonfigurasi` / `anthropic API key not configured` | Struct secret bukan nama `secrets`, atau secret belum di-set | Pastikan `finance` memakai `var secrets struct { AnthropicAPIKey string }`; `encore secret set --type local AnthropicAPIKey` lalu **restart** `encore run` |
| Import katalog jalan, transaksi tidak | Bug lama `txnImageSecrets` | Deploy perbaikan terbaru; keduanya memakai `AnthropicAPIKey` yang sama |

## Frontend

- `/dashboard/finance/transactions/import-image`
- Tombol **Import dari gambar** di halaman Transaksi (owner).
- Client: `lib/api/transactionImage.ts`
