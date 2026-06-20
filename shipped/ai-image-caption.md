# Shipped: AI memproses caption media (Fase 3a — hotfix)

**Status:** Siap merge  
**Branch:** `feat/inbox-media`  
**PR:** [#35](https://github.com/vwijaya03/wabantu-api-go/pull/35)  
**Tanggal:** 2026-06  
**Roadmap terkait:** [`docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md`](../docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md) — Fase 3a

---

## Masalah

Pelanggan mengirim **gambar + caption** (mis. *"kamu punya barang ini gak min?"*). Gambar tampil di Inbox (Fase 1), tetapi AI tidak membalas karena auto-reply hanya menerima `message.type = text`.

Log sebelum fix:

```
WRN AI job: inbound type not supported type=image
INF inbound AI job done sent=false
```

---

## Perilaku setelah fix

| Inbound | Caption (`body`) | AI auto-reply |
|---------|------------------|---------------|
| `text` | isi pesan | ✅ seperti biasa |
| `image` / `video` / `document` | ada teks | ✅ caption diproses sebagai `userText` |
| `image` / `video` / `document` | kosong | ⏭ skip (`media inbound without caption`) |
| `audio`, `sticker`, `location`, dll. | — | ⏭ skip (belum didukung) |

**Belum termasuk:** vision / baca isi pixel gambar (Fase 3c). Hanya teks caption.

---

## Perubahan kode

| File | Perubahan |
|------|-----------|
| `ai/inbound_media.go` | `inboundTextForAutoReply`, `isMediaTypeWithOptionalCaption` |
| `ai/inbound_media_test.go` | Unit test tipe + caption |
| `ai/autoreply.go` | Ganti guard `type != text` → helper di atas |

Webhook dan penyimpanan pesan **tidak berubah** — caption sudah di `body` sejak parse WA.

---

## Test manual

1. Kirim gambar WA + caption bertanya stok → AI membalas (katalog/stok jika inventory aktif).
2. Kirim gambar tanpa caption → tidak ada balasan AI; log `media inbound without caption`.
3. Kirim teks biasa → tidak regresi.

```bash
cd api-go && encore test ./ai/... -run InboundTextForAutoReply
```

---

## Ketergantungan

- **Fase 1 (Inbox media):** tampilan gambar di UI — bagian dari PR yang sama (`feat/inbox-media`).
