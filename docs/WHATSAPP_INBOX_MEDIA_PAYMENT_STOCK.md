# Roadmap: Media Inbox, Bukti Transfer, & Stok AI (WhatsApp)

Dokumen spesifikasi pengembangan fitur inbox media, verifikasi bukti transfer, dan penjagaan stok di alur AI WhatsApp.

**Status:** Disetujui untuk development (keputusan produk 2026-06)  
**Scope:** `api-go/` + `web-frontend/` — PR terpisah per repo per fase  
**Base branch:** `master`

---

## Ringkasan

Tiga kebutuhan yang saling terkait dalam alur jualan WhatsApp:

1. **Gambar di Inbox** — pelanggan sering kirim foto (produk, bukti transfer); saat ini tersimpan di DB tapi tidak tampil di UI.
2. **Bukti transfer + flag order** — deteksi gambar bukti, link ke order, status pembayaran dengan kontrol abuse; verifikasi manual atau auto (setting tenant).
3. **Stok tersedia & item habis** — AI jawab stok real; tolak qty melebihi stok saat order flow (tanpa rekomendasi produk alternatif di v1).

---

## Keputusan Produk (Final)

| Pertanyaan | Keputusan |
|------------|-----------|
| Verifikasi bukti | **Setting tenant:** `manual` (default) atau `auto_verify`. UI wajib jelaskan: mode auto memakai **vision AI** → **kuota token bulanan habis lebih cepat**. |
| Setelah verified | Order **`draft` → `processing`** (otomatis saat verified, baik manual maupun auto). |
| Sumber rekening valid | **FAQ / Knowledge Base** — bukan `business_profile` atau wallet keuangan. Owner yang ingin AI verifikasi **wajib** isi nomor rekening + atas nama di KB. Copy onboarding wajib menyebut ini. |
| Stok habis | **Tolak + minta kurangi qty** — tidak tawarkan produk alternatif di v1. |

### Alur status order setelah pembayaran

```
                    ┌─────────────────────────────────────┐
                    │  Order aktif (draft / processing)   │
                    └─────────────────┬───────────────────┘
                                      │
                    Pelanggan kirim gambar bukti (+ konteks checkout)
                                      │
                    ┌─────────────────▼───────────────────┐
                    │  payment_status = proof_submitted   │
                    │  (OCR opsional, flag mismatch)      │
                    └─────────────────┬───────────────────┘
                                      │
              ┌───────────────────────┴───────────────────────┐
              │                                               │
    payment_verification_mode = manual              mode = auto_verify
              │                                               │
    Owner klik Verifikasi di UI                   OCR exact match + confidence ≥ threshold
    (Orders / Inbox)                            + rekening cocok KB
              │                                               │
              └───────────────────────┬───────────────────────┘
                                      │
                    ┌─────────────────▼───────────────────┐
                    │  payment_status = verified            │
                    │  order.status = processing            │
                    │  system message di inbox              │
                    └───────────────────────────────────────┘

    Manual reject: payment_status = rejected, order tetap draft/processing (tidak mundur otomatis)
```

**Manual:** hanya owner/staff dengan aksi eksplisit **Verifikasi** / **Tolak** mengubah `payment_status` dan (jika verify) `order.status`.

**Auto:** pipeline AI + vision setelah gambar masuk; jika semua rule lulus → langsung `verified` + `processing` tanpa klik owner. Jika ragu → `proof_submitted` + notifikasi owner (semi-auto fallback).

---

## Temuan Codebase (Baseline)

| Area | File | Perilaku saat ini |
|------|------|-------------------|
| Parse WA image | `api-go/whatsapp/whatsapp.go` | `type=image`, caption di `body`, `image.id` di `raw` |
| Simpan pesan | `api-go/webhook/webhook.go` | Insert ke `message` semua tipe |
| Schema | `api-go/tenant/tenant.go` | `message.type`, `body`, `metadata` JSONB |
| API inbox | `api-go/inbox/inbox.go` `GetMessages` | **Tidak return `metadata`** — FE tidak bisa render gambar |
| UI inbox | `web-frontend/app/(dashboard)/dashboard/inbox/page.tsx` | `(pesan non-text)` untuk non-text |
| AI | `api-go/ai/autoreply.go` | `inbound.Type != "text"` → **skip** |
| Stok AI | `api-go/ai/order_catalog.go`, `catalog_reply.go` | Stok real jika inventory setup; inquiry OK; **order flow tidak cek stok** |
| Order stock guard | `api-go/order/order.go` | `PrecheckOrderStock` hanya status committed |
| Vision existing | `api-go/aivision/vision.go`, `finance/transaction_image.go` | Pola reuse untuk OCR bukti |
| Payment order | `order.payment_transaction_id` | Midtrans QRIS **platform** — bukan bukti buyer |

---

## Fase Pengembangan

Urutan merge disarankan: **Fase 1 → Fase 4 → Fase 2 → Fase 3**

| Fase | Nama | Effort | PR branch contoh |
|------|------|--------|------------------|
| 1 | Media di Inbox | M | `feat/inbox-media` |
| 4 | Stok guard AI | M | `feat/ai-stock-guard` |
| 2 | Bukti transfer + setting | L | `feat/order-payment-proof` |
| 3 | AI image context lanjutan | M–L | `feat/ai-image-context` |

---

## Fase 1 — Media di Inbox (MVP gambar)

### Tujuan

Staff/owner melihat gambar yang dikirim pelanggan di Inbox.

### Backend (`api-go`)

- [ ] `whatsapp.DownloadMedia(ctx, accessToken, mediaID)` via Meta Graph API
- [ ] **MVP:** `GET /api/v1/inbox/messages/:messageId/media` — proxy on-demand + cache Redis TTL
- [ ] **Production (1b):** async download saat webhook → persist blob / object storage; simpan URL di `message.metadata`
- [ ] `GetMessages`: field `media?: { url, mimeType, thumbnailUrl? }` per message
- [ ] `lastMessagePreview` conversation: prefix `📷` untuk image

### Frontend (`web-frontend`)

- [ ] Komponen `InboxMessageBubble` — render `image`, caption, lightbox
- [ ] `document` / `audio` / `video`: placeholder v1 ("belum didukung penuh")
- [ ] Update `lib/api/inbox.ts` — type `InboxMessage` + media

### Test

- [ ] Webhook fixture image → row `message.type=image`
- [ ] GET media endpoint auth + tenant isolation
- [ ] FE render gambar inbound

### PR

- api-go dan web-frontend **terpisah**; merge backend dulu jika kontrak API baru.

---

## Fase 4 — Stok tersedia & item habis

### Tujuan

AI jawab stok akurat **per gudang**; order flow tidak membuat draft jika qty melebihi stok **satu gudang mana pun** (bukan total gabungan).

### Perubahan

- [x] `enrichCatalogStock`: **available** (`on_hand - reserved`) per gudang
- [x] Inquiry stok: breakdown per gudang + total (jika >1 gudang)
- [x] Label gudang ke pembeli: `customer_label` jika diisi, else `name` gudang (bukan `code`, bukan label generik)
- [x] `order_flow` step qty: tolak jika tidak ada gudang tunggal yang cukup; auto-assign gudang default lalu `display_order`
- [x] `persistDraftOrder`: set `items[].warehouseId` + precheck DB
- [x] Kolom `inv_warehouse.customer_label` + form gudang di web-frontend

### Test

- [x] `order_stock_guard_test.go` — multi-gudang, customer_label, breakdown
- [x] `order_flow` sim — qty dalam stok gudang default → `warehouseId` terisi
- [x] Tenant tanpa inventory → perilaku lama (tanpa stok)

### PR

- `api-go`: `ai/`, `inventory/`, migration tenant
- `web-frontend`: field label pelanggan di halaman Gudang

---

## Fase 2 — Bukti transfer + flag order

### Tujuan

Gambar bukti transfer ter-link ke order; `payment_status` + setting manual/auto; setelah verified → `processing`.

### Migration DB

Update **kedua** `api-go/tenant/tenant.go` + `api-go/tenant/schema_patch.go`.

**Kolom baru di `"order"`:**

| Kolom | Tipe | Default | Keterangan |
|-------|------|---------|------------|
| `payment_status` | `VARCHAR(20)` | `unpaid` | `unpaid` \| `proof_submitted` \| `verified` \| `rejected` |
| `payment_proof_message_id` | `UUID` | NULL | FK `message.id` |
| `payment_proof_submitted_at` | `TIMESTAMPTZ` | NULL | |
| `payment_proof_verified_at` | `TIMESTAMPTZ` | NULL | |
| `payment_proof_verified_by` | `UUID` | NULL | NULL jika auto-verify |
| `payment_proof_meta` | `JSONB` | `{}` | OCR: amount, bank, account_name, confidence, flags |

**Kolom baru di `business_profile` atau `inv_setting` — pilih satu (disarankan `business_profile`):**

| Kolom | Tipe | Default | Keterangan |
|-------|------|---------|------------|
| `payment_verification_mode` | `VARCHAR(20)` | `manual` | `manual` \| `auto_verify` |
| `payment_auto_verify_min_confidence` | `NUMERIC(5,2)` | `0.95` | Hanya dipakai jika auto |

Index: `idx_order_payment_status` pada `(payment_status, created_at DESC)` WHERE `deleted_at IS NULL`.

### Sumber rekening valid (KB)

- Parser helper: `loadPaymentAccountsFromKB(kb []Entry)` — cari entri kategori/tag `payment`, `rekening`, `bank`, atau pertanyaan mengandung "rekening"/"transfer".
- OCR hasil harus cocok **nomor rekening** dan/atau **atas nama** dengan salah satu entri KB.
- Jika KB kosong / tidak ada rekening:
  - Mode `manual`: tetima bukti → `proof_submitted`, tampilkan warning "Lengkapi FAQ rekening untuk verifikasi AI"
  - Mode `auto_verify`: **tidak** auto-verify; fallback ke `proof_submitted`

**Copy wajib (AI Settings / KB setup):**

> Agar bukti transfer bisa diverifikasi AI, tambahkan FAQ berisi **nomor rekening** dan **atas nama** penerima pembayaran.

### Setting UI (`web-frontend`)

Halaman: **AI Settings** atau section baru **Pembayaran & Bukti Transfer**

| Opsi | Label | Deskripsi |
|------|-------|-----------|
| `manual` | Verifikasi manual (disarankan) | Owner mengecek bukti di Inbox/Orders lalu klik Verifikasi. |
| `auto_verify` | Verifikasi otomatis (AI) | AI membandingkan bukti dengan total order & rekening di FAQ. **Menggunakan kuota token vision — bisa habis lebih cepat di bulan yang sama.** |

Toggle hanya **owner** (`canPerformOwnerActions`).

### Pipeline gambar bukti

1. Webhook simpan `message` type `image`
2. Pub/Sub job (atau inline jika ringan): cek konteks
   - Ada order aktif (`draft`/`processing`) untuk `contact_id` / `conversation_id`
   - Atau `IsActiveCheckoutFromHistory`
3. Download media → `aivision` prompt bukti transfer (nominal, bank, rekening tujuan, tanggal, atas nama)
4. Match rules:
   - Nominal ≈ `order.total` (toleransi configurable, default Rp 1.000)
   - Rekening/atas nama cocok entri KB
   - Tanggal tidak di masa depan
   - File hash belum dipakai order lain (anti duplikat)
5. Branch:
   - **manual mode:** selalu `proof_submitted` + isi `payment_proof_meta`
   - **auto mode:** jika semua rule + `confidence >= min` → `verified` + `order.status = processing` + `payment_proof_verified_by = NULL` (system)
   - Jika auto gagal/ragu → `proof_submitted`

### API owner

| Method | Path | Aksi |
|--------|------|------|
| `POST` | `/api/v1/orders/:id/payment-proof/verify` | `verified` + `processing` |
| `POST` | `/api/v1/orders/:id/payment-proof/reject` | `rejected` + body `{ reason? }` |
| `PATCH` | `/api/v1/business/profile` | extend dengan `paymentVerificationMode` |

### Frontend Orders / Inbox

- [ ] Badge list: Belum bayar / Bukti masuk / Terverifikasi / Ditolak
- [ ] Detail order: thumbnail bukti, hasil OCR, tombol Verifikasi / Tolak
- [ ] Inbox: gambar bukti → link "Lihat order terkait"
- [ ] System message di thread saat bukti masuk / verified

### Anti-abuse

| Risiko | Mitigasi |
|--------|----------|
| Fake screenshot | Default manual; auto hanya jika setting + confidence tinggi |
| Bukti dipakai ulang | SHA-256 file / perceptual hash → reject duplikat |
| Spam gambar | Rate limit N bukti/jam/contact |
| Order salah | Satu order aktif per contact; multi-order → AI tanya konfirmasi |
| Nominal tidak cocok | Flag `mismatch_amount` di meta; tidak auto-verify |
| Token abuse | Usage meter vision; warning di setting auto |

### Test

- [ ] Migration patch idempotent
- [ ] Manual verify → status processing
- [ ] Auto verify exact match → processing tanpa user id
- [ ] Auto dengan KB kosong → proof_submitted only
- [ ] Duplikat hash → ditolak
- [ ] `encore test ./order/... ./ai/...`

---

## Fase 3 — AI image context lanjutan

### Tujuan

AI tidak diam untuk semua gambar non-bukti.

### Scope bertahap

| Sub | Perilaku |
|-----|----------|
| 3a | Image + caption → proses caption sebagai text |
| 3b | Image tanpa caption + order aktif → pipeline bukti (Fase 2) |
| 3c | Image produk → vision match katalog (opsional, kuota) |
| 3d | Tidak relevan → pesan fallback + opsi handoff |

**Prioritas setelah Fase 1+2 stabil.**

---

## Dampak Teknis per Layer

| Layer | Fase 1 | Fase 4 | Fase 2 | Fase 3 |
|-------|--------|--------|--------|--------|
| `whatsapp/` | Download media | — | — | — |
| `webhook/` | Trigger download job | — | Trigger proof job | — |
| `inbox/` | Media URL API | — | Link proof | — |
| `ai/` | — | Stock guard | Image + OCR | Vision routing |
| `order/` | — | — | payment_status, verify API | — |
| `aivision/` | — | — | Prompt bukti | Prompt produk |
| `tenant/` | — | — | Migration | — |
| `web-frontend` inbox | Render image | — | Link order | — |
| `web-frontend` orders | — | Warning stok | Badge + verify | — |
| `web-frontend` settings | — | — | Mode manual/auto + warning token | — |
| `web-frontend` KB | — | — | Copy rekening wajib | — |

---

## Test Plan (gabungan)

- [ ] `encore check` bersih
- [ ] `encore test ./inbox/... ./ai/... ./order/... ./webhook/...`
- [ ] Manual QA: kirim gambar WA → tampil inbox
- [ ] Manual QA: bukti transfer → flag order → verify manual → processing
- [ ] Manual QA: auto_verify + FAQ rekening → processing tanpa klik
- [ ] Manual QA: stok 2, order 5 → AI tolak
- [ ] `npm run build` / `tsc` web-frontend

---

## Estimasi

| Fase | Effort | Catatan |
|------|--------|---------|
| 1 | S–M (3–5 hari) | Proxy media MVP |
| 4 | M (3–5 hari) | Bisa paralel Fase 1 |
| 2 | L (1–2 minggu) | Migration + vision + UI |
| 3 | M–L | Setelah 2 stabil |

---

## Referensi Kode

```
api-go/whatsapp/whatsapp.go          — ParseWebhook, InboundMessage
api-go/webhook/webhook.go            — insertMessage
api-go/inbox/inbox.go                — GetMessages, SendMessage
api-go/ai/autoreply.go               — AI pipeline entry
api-go/ai/order_flow.go              — persistDraftOrder
api-go/ai/order_catalog.go           — enrichCatalogStock
api-go/ai/catalog_reply.go           — buildCatalogItemReply, formatStockLabel
api-go/order/order.go                — PrecheckOrderStock
api-go/aivision/vision.go            — Vision extract
api-go/finance/transaction_image.go  — Pola staging + OCR
api-go/tenant/tenant.go              — Schema message, order
web-frontend/app/.../inbox/page.tsx
web-frontend/app/.../orders/page.tsx
web-frontend/app/.../ai-settings/page.tsx
web-frontend/app/.../knowledge-base/page.tsx
web-frontend/lib/api/inbox.ts
```

---

## Changelog dokumen

| Tanggal | Perubahan |
|---------|-----------|
| 2026-06 | Draft awal dari ultrathink + keputusan produk: setting manual/auto, verified→processing, rekening dari KB, stok tolak tanpa alternatif |

---

*Dokumen ini menjadi acuan sebelum branch `feat/inbox-media` dan fase berikutnya. Update file ini jika keputusan produk berubah.*
