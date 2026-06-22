# Shipped: Media WhatsApp di Inbox (Fase 1)

**Status:** Siap merge  
**Branch:** `feat/inbox-media`  
**PR:** [#35](https://github.com/vwijaya03/wabantu-api-go/pull/35)  
**Tanggal:** 2026-06  
**Roadmap terkait:** [`docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md`](../docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md) — Fase 1

---

## Apa yang di-ship

Staff/owner dapat **melihat gambar** (dan metadata media lain) yang dikirim pelanggan lewat WhatsApp, langsung di halaman Inbox. Caption tetap ditampilkan di bawah gambar.

**Sengaja belum di Fase 1:**

- AI auto-reply untuk pesan gambar dengan caption — lihat [ai-image-caption.md](./ai-image-caption.md)
- Download async ke object storage (Fase 1b di roadmap)
- Bukti transfer / vision (Fase 2–3)

---

## Perilaku runtime

### Webhook → database

- Semua tipe pesan WA (termasuk `image`) disimpan ke `message` seperti sebelumnya.
- `message.body` = caption (jika ada).
- `message.metadata` = payload JSON Meta (termasuk `image.id` untuk unduh media).

### API Inbox

| Endpoint | Fungsi |
|----------|--------|
| `GET /api/v1/inbox/conversations/:id/messages` | Response per pesan: field opsional `media.url` (path proxy internal) |
| `GET /api/v1/inbox/messages/:messageId/media` | Proxy unduh dari Meta Graph API + **cache Redis 1 jam** |

Tipe yang bisa di-proxy: `image`, `video`, `audio`, `document`, `sticker`.

### Preview daftar percakapan

`lastMessagePreview` untuk gambar: prefix `📷` (+ caption jika ada) via `whatsapp.InboundMessagePreview`.

---

## File kunci

| File | Peran |
|------|--------|
| `whatsapp/media.go` | `DownloadMedia`, `ExtractMediaIDFromRaw`, `InboundMessagePreview` |
| `whatsapp/media_test.go` | Unit test parse & preview |
| `inbox/media.go` | Handler proxy media + cache |
| `inbox/inbox.go` | `GetMessages` mengembalikan `media`, query include `metadata` |
| `webhook/webhook.go` | Preview non-text di update conversation |

---

## Frontend (repo terpisah)

UI: PR [web-frontend #25](https://github.com/vwijaya03/wabantu-web-frontend/pull/25) — lihat [`web-frontend/docs-development-shipped/inbox-media-fase1.md`](../../web-frontend/docs-development-shipped/inbox-media-fase1.md).

---

## Test

```bash
cd api-go
encore test ./whatsapp/... -run Media
encore test ./inbox/...
```

Manual: kirim gambar WA → muncul di Inbox → klik/lightbox → gambar load via endpoint media.

---

## Catatan operasional

- Proxy membutuhkan `access_token` channel Meta masih valid.
- Batas unduh: 10 MiB per file (`maxDownloadMediaBytes`).
- Cache key per `messageId`; TTL 1 jam.
