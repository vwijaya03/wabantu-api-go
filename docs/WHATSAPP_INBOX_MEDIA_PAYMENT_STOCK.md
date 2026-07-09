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
| 1 | Media di Inbox (MVP) | M | `feat/inbox-media` |
| 1b | Persist media ke S3 | L | `feat/inbox-media-s3` |
| 4 | Stok guard AI | M | `feat/ai-stock-guard` |
| 2 | Bukti transfer + setting | L | `feat/order-payment-proof` |
| 3 | AI image context lanjutan | M–L | `feat/ai-image-context` |

---

## Fase 1 — Media di Inbox (MVP gambar)

### Tujuan

Staff/owner melihat gambar yang dikirim pelanggan di Inbox.

### Backend (`api-go`)

- [x] `whatsapp.DownloadMedia(ctx, accessToken, mediaID)` via Meta Graph API
- [x] **MVP:** `GET /api/v1/inbox/messages/:messageId/media` — proxy on-demand + cache Redis TTL
- [ ] **Production (1b):** async download saat webhook → persist ke S3; lihat [Fase 1b](#fase-1b--persist-media-ke-amazon-s3) + [`docs-development-shipped/inbox-media-s3.md`](../docs-development-shipped/inbox-media-s3.md)
- [x] `GetMessages`: field `media?: { url, mimeType, thumbnailUrl? }` per message
- [x] `lastMessagePreview` conversation: prefix `📷` untuk image

**Shipped detail:** [`docs-development-shipped/inbox-media-fase1.md`](../docs-development-shipped/inbox-media-fase1.md)

### Frontend (`web-frontend`)

- [x] Komponen `InboxMessageBubble` — render `image`, caption
- [ ] Lightbox fullscreen (klik gambar → Dialog) — branch `feat/inbox-media-lightbox` (web-frontend)
- [x] `document` / `audio` / `video`: placeholder v1 ("belum didukung penuh")
- [x] Update `lib/api/inbox.ts` — type `InboxMessage` + media

### Test

- [x] Webhook fixture image → row `message.type=image`
- [x] GET media endpoint auth + tenant isolation
- [x] FE render gambar inbound

### PR

- api-go [#35](https://github.com/vwijaya03/wabantu-api-go/pull/35) + web-frontend [#25](https://github.com/vwijaya03/wabantu-web-frontend/pull/25) — merged.

---

## Fase 1b — Persist media ke Amazon S3

### Tujuan

Media inbox tidak bergantung pada `access_token` Meta jangka panjang; object tersimpan di bucket tenant-scoped dengan tracking `storage_byte`.

### Status

- [ ] Pub/Sub `inbox-media-persist` — download async saat webhook
- [ ] Package `shared/mediastorage` — Put/Get/Delete S3
- [ ] Patch `message.metadata` — `persisted`, `s3Key`, `mimeType`, `bytes`
- [ ] `GetMessageMedia` — stream S3 jika `s3Key` ada, else fallback proxy Meta
- [ ] Encore secrets `AWSS3*` + graceful degrade jika kosong (lokal)
- [ ] Increment `usage.storage_byte`; skip persist jika over quota

**Spesifikasi implementasi:** [`docs-development-shipped/inbox-media-s3.md`](../docs-development-shipped/inbox-media-s3.md)  
**Branch:** `feat/inbox-media-s3` (api-go only)

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

## Fase 4b — Order status dari history chat (ownership)

### Tujuan

Pembeli bisa cek pesanan milik chat mereka (termasuk dari history outbound & hint penerima), sambil memblokir lookup pembeli/pesanan orang lain.

### Perubahan

- [x] Early routing `pembeli` / third-party deny → `path: order_lookup_denied`
- [x] Scoped recipient hint (nama/HP penerima di `shipping_address`)
- [x] Parse `WB-` dari history outbound (6–8 pesan terakhir)
- [x] FAQ/LLM bypass untuk intent order lookup (`tryFAQDirectAnswer` skip)
- [x] Ownership filter tetap lewat `orderAccessScope` + `loadOrderByRefForContact`

### Test

- [x] `order_buyer_lookup_test.go` — Lavana Snack deny, supriyanto hint, pesanan aktif, FAQ skip
- [x] Regression: `order_ownership_100_test.go`, `order_customer_test.go`

### Test plan manual

- [ ] `saya masih punya pesanan aktif nggak ?` → `order_status`
- [ ] `pembeli dengan nama Lavana Snack ada ?` → `order_lookup_denied`, tanpa LLM
- [ ] `pembeli atas nama saya ada ?` → `order_status`
- [ ] `pembeli atas nama ini ada? Nama: supriyanto` + order scoped penerima supriyanto → `order_status` WB-...
- [ ] Nama supriyanto tanpa order di scope → tidak ada pesanan di chat ini (tanpa bocor data orang lain)

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
| `payment_proof_meta` | `JSONB` | `{}` | OCR: amount, bank, account_name, confidence, flags; plus `rejectionCount`, `proofBlocked`, `blockedNotified` (limit 5x penolakan) |

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
2. Pub/Sub job `payment-proof-jobs`: cek konteks
   - Ada order aktif (`draft`/`processing`) untuk `contact_id` / `conversation_id`
   - Atau `IsActiveCheckoutFromHistory`
   - Caption eksplisit `WB-xxxxxxxx` resolve order meski blocked (untuk pesan batas)
3. **Early exit jika blocked** (`rejectionCount >= 5` / `proofBlocked`): skip download & OCR; WA batas sekali (`blockedNotified`), lalu silent ignore
4. Download media → `aivision` prompt bukti transfer (nominal, bank, rekening tujuan, tanggal, atas nama)
5. Match rules:
   - Nominal ≈ `order.total` (toleransi configurable, default Rp 1.000)
   - Rekening/atas nama cocok entri KB
   - Tanggal tidak di masa depan
   - File hash belum dipakai order lain (anti duplikat)
6. Branch:
   - **manual mode:** selalu `proof_submitted` + isi `payment_proof_meta`
   - **auto mode:** jika semua rule + `confidence >= min` → `verified` + `order.status = processing` + `payment_proof_verified_by = NULL` (system)
   - Jika auto gagal/ragu → `proof_submitted`
7. Setiap **penolakan** (`rejected`): `rejectionCount++`; jika `>= 5` → `proofBlocked = true`
8. **Outbound WA** ke pembeli untuk diterima/ditolak/terverifikasi/batas (bukan hanya insert `message` DB)

**Shipped detail:** [`docs-development-shipped/payment-proof-fase2.md`](../docs-development-shipped/payment-proof-fase2.md)

### API owner

| Method | Path | Aksi |
|--------|------|------|
| `POST` | `/api/v1/orders/:id/payment-proof/verify` | `verified` + `processing` |
| `POST` | `/api/v1/orders/:id/payment-proof/reject` | `rejected` + body `{ reason? }` + increment `rejectionCount` |
| `POST` | `/api/v1/orders/:id/payment-proof/unblock` | Reset counter block; WA ke pembeli; owner only |
| `PATCH` | `/api/v1/business/profile` | extend dengan `paymentVerificationMode` |

### Frontend Orders / Inbox

- [x] Badge list: Belum bayar / Bukti masuk / Terverifikasi / Ditolak
- [x] Detail order: thumbnail bukti, hasil OCR, tombol Verifikasi / Tolak / Buka batas bukti
- [x] Inbox: gambar bukti → link "Lihat order terkait" (`linkedOrderId` di `GetMessages`)
- [x] System message + WA outbound saat bukti masuk / verified / ditolak / batas 5x

**UI shipped:** [`web-frontend/docs-development-shipped/payment-proof-fase2.md`](../../web-frontend/docs-development-shipped/payment-proof-fase2.md)

### Anti-abuse

| Risiko | Mitigasi |
|--------|----------|
| Fake screenshot | Default manual; auto hanya jika setting + confidence tinggi |
| Bukti dipakai ulang | SHA-256 file / perceptual hash → reject duplikat |
| Spam gambar | Rate limit 10 bukti/jam/contact (Redis) |
| Spam retry pesanan sama | Max **5 penolakan per order** → `proofBlocked`; upload diabaikan; owner **Buka batas bukti** reset counter |
| Order salah | Satu order aktif per contact; multi-order → AI tanya konfirmasi |
| Nominal tidak cocok | Flag `mismatch_amount` di meta; tidak auto-verify |
| Token abuse | Usage meter vision; warning di setting auto |

### Test

- [x] Migration patch idempotent
- [x] Manual verify → status processing
- [x] Auto verify exact match → processing tanpa user id
- [x] Auto dengan KB kosong → proof_submitted only
- [x] Duplikat hash → ditolak
- [x] Limit 5x penolakan → upload diabaikan + WA batas sekali
- [x] Unblock owner → counter reset, pipeline terima lagi
- [x] `encore test ./order/... ./ai/...`

---

## Fase 3 — AI image context lanjutan

### Tujuan

AI tidak diam untuk semua gambar non-bukti.

### Scope bertahap

| Sub | Perilaku | Status |
|-----|----------|--------|
| 3a | Image + caption → proses caption sebagai text | ✅ Shipped |
| 3b | Image tanpa caption + order aktif → pipeline bukti (Fase 2) | ✅ Shipped |
| 3c | Image produk → vision match katalog (opsional, kuota) | 🔲 Planned — `feat/ai-image-context` |
| 3d | Tidak relevan → pesan fallback + opsi handoff | 🔲 Planned — `feat/ai-image-context` |

### Fase 3a — Caption sebagai teks (shipped)

- [x] `inboundTextForAutoReply` — image/video/document + caption → proses sebagai `userText`
- [x] Tanpa caption → skip auto-reply (`media inbound without caption`)

**Shipped detail:** [`docs-development-shipped/ai-image-caption.md`](../docs-development-shipped/ai-image-caption.md)

### Fase 3b — Gambar tanpa caption → bukti (shipped)

- [x] `IsPaymentProofInbound` di `autoreply.go` — skip AI untuk gambar bukti
- [x] `processPaymentProofJob` — image tanpa caption + order aktif masuk pipeline Fase 2
- [x] No target order → silent skip *(akan diganti 3d di `feat/ai-image-context`)*

**Shipped detail:** [`docs-development-shipped/payment-proof-fase2.md`](../docs-development-shipped/payment-proof-fase2.md)

### Fase 3c — Vision match katalog (planned)

- [ ] `aivision.ExtractProductMatchFromImage` — prompt produk + confidence
- [ ] Fuzzy match ke katalog/inventory aktif (threshold ≥ 0.85)
- [ ] Balas stok/harga via `buildCatalogItemReply` + `formatStockLabel`
- [ ] Rate limit 5 vision image/contact/jam; purpose `product_image_match`
- [ ] Tidak match → fallback 3d

### Fase 3d — Fallback gambar (planned)

- [ ] Trigger saat payment-proof skip (no order) + autoreply skip (no caption)
- [ ] Outbound WA: minta ketik pertanyaan atau *bantuan*
- [ ] Record AI activity path `image_fallback`
- [ ] Opsional handoff (`image_unhandled`) — default off v1

**Spesifikasi implementasi 3c/3d:** [`docs-development-shipped/ai-image-context.md`](../docs-development-shipped/ai-image-context.md)  
**Branch:** `feat/ai-image-context` (api-go only)

---

## Dampak Teknis per Layer

| Layer | Fase 1 | Fase 1b | Fase 4 | Fase 2 | Fase 3 |
|-------|--------|---------|--------|--------|--------|
| `whatsapp/` | Download media | — | — | — | — |
| `webhook/` | Insert message | Trigger persist job | — | Trigger proof job | — |
| `inbox/` | Media URL API | S3 GET fallback | — | Link proof | Fetch bytes |
| `shared/mediastorage/` | — | S3 client | — | — | — |
| `ai/` | — | — | Stock guard | Image + OCR | Image context 3c/3d |
| `order/` | — | — | — | payment_status, verify API | — |
| `aivision/` | — | — | — | Prompt bukti | Prompt produk |
| `tenant/` | — | — | Migration | Migration | — |
| `web-frontend` inbox | Render image | — | — | Link order | — |
| `web-frontend` orders | — | — | Warning stok | Badge + verify | — |
| `web-frontend` settings | — | — | — | Mode manual/auto + warning token | — |
| `web-frontend` KB | — | — | — | Copy rekening wajib | — |

---

## Test Plan (gabungan)

- [ ] `encore check` bersih
- [ ] `encore test ./inbox/... ./ai/... ./order/... ./webhook/...`
- [ ] Manual QA: kirim gambar WA → tampil inbox
- [ ] Manual QA: bukti transfer → flag order → verify manual → processing
- [ ] Manual QA: auto_verify + FAQ rekening → processing tanpa klik
- [ ] Manual QA (Fase 4): 2 gudang (stok 2 + 3) → tanya stok → breakdown + total 5
- [ ] Manual QA (Fase 4): order qty 5 ditolak (tidak ada gudang tunggal cukup); qty 2 lolos + `warehouseId`
- [ ] Manual QA (Fase 4): `customer_label` tampil di chat; kosong → pakai `name` gudang
- [ ] Manual QA (Fase 4): halaman Gudang — tambah/edit label pelanggan tersimpan
- [ ] `npm run build` / `tsc` web-frontend

---

## Estimasi

| Fase | Effort | Catatan |
|------|--------|---------|
| 1 | S–M (3–5 hari) | Proxy media MVP — **shipped** |
| 1b | L (4–6 hari) | S3 persist + secrets + quota |
| 4 | M (3–5 hari) | Bisa paralel Fase 1 — **shipped** |
| 2 | L (1–2 minggu) | Migration + vision + UI — **shipped** |
| 3a/3b | S | Caption + bukti routing — **shipped** |
| 3c/3d | M | Vision katalog + fallback — planned |

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
| 2026-07 | Sync status shipped: Fase 1 MVP, 2 (incl. inbox link), 4, 4b, 3a, 3b. Tambah Fase 1b S3 + Fase 3c/3d planned; shipped docs `inbox-media-s3.md`, `ai-image-context.md` |

---

*Dokumen ini menjadi acuan sebelum branch `feat/inbox-media` dan fase berikutnya. Update file ini jika keputusan produk berubah.*
