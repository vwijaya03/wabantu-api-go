# Runbook: KB Pengiriman tenant demo (Omah Apparel)

**Status:** Catatan ops (runbook); eksekusi produksi hanya setelah approve Viko.

## Masalah / Kebutuhan

Canary lomba dan regresi buyerflow mengharapkan jawaban ETA pengiriman dari Knowledge Base (path `shipping_faq`), mis. pertanyaan **«berapa lama pengiriman?»** → jawaban memuat **«2-3 hari»** (selaras `internal/buyerflow/regression_cases.go`, `shipping_reply_test.go`, simulator fixture).

Tenant demo produksi: schema **`t_omah_apparel`**. Lingkungan Encore bernama **staging** = **produksi** (bukan tenant terpisah).

**Tidak** mengubah `delivery_area` / profil toko lewat runbook ini.

## Cara disarankan: dashboard (owner/staff)

1. Login ke app produksi sebagai **owner atau staff** tenant Omah Apparel (bukan sesi Pantau super admin).
2. Buka **Dashboard → Knowledge Base** (`/dashboard/knowledge-base`).
3. Di form **Tambah FAQ baru**, isi:
   | Field | Nilai |
   |-------|--------|
   | Pertanyaan | `Berapa lama pengiriman?` (varian tanpa tanda tanya juga OK) |
   | Kategori | `Pengiriman` (harus mengandung «pengiriman» agar `tryShippingKBFAQ` memilih entri) |
   | Jawaban | `Estimasi pengiriman 2-3 hari kerja via JNE.` |
4. Simpan. API `POST /api/v1/knowledge-base` otomatis mengantre indexing (`retrieval_outbox` + worker).
5. Verifikasi (opsional): kirim «berapa lama pengiriman?» lewat simulator/inbox demo; balasan harus memuat «2-3 hari» dan path `shipping_faq`.
6. Jika vector retrieval belum sempat index, tunggu worker atau gunakan **Admin → AI Retrieval** reindex tenant (lihat shipped RAG).

## SQL opsional (manual, setelah approve Viko)

File: [`scripts/seed-omah-shipping-kb.sql`](../scripts/seed-omah-shipping-kb.sql)

- **INSERT only**, idempotent (`WHERE NOT EXISTS`).
- **Tanpa** DELETE/UPDATE.
- Hanya schema **`t_omah_apparel`**.
- Jalankan lewat klien SQL dengan akses DB produksi; **jangan** commit secret atau key ke repo.
- Setelah INSERT manual, pertimbangkan reindex KB tenant jika balasan vector belum konsisten (dashboard create lebih aman karena outbox otomatis).

## File utama

| Path | Peran |
|------|--------|
| `web-frontend/app/(dashboard)/dashboard/knowledge-base/page.tsx` | UI tambah FAQ |
| `kb/kb.go` | Create + outbox indexing |
| `internal/buyerflow/shipping_reply.go` | Matching kategori Pengiriman |
| `scripts/seed-omah-shipping-kb.sql` | Seed opsional |

## Testing

- Lokal: `encore test ./internal/buyerflow/ -run Shipping -count=1`
- Regresi: `./scripts/run-ai-regression-tests.sh` (baris «berapa lama pengiriman?» → `shipping_faq`, substring «2-3 hari»)

## Catatan deploy

- Produksi = tenant demo; tidak ada staging terpisah untuk data KB ini.
- Rollback konten: nonaktifkan/hapus entri lewat dashboard (bukan lewat script seed).
