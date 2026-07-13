# Fitur yang Sudah Rilis (Development Shipped)

Folder ini berisi **catatan implementasi** fitur yang sudah (atau sedang) di-merge — bukan spesifikasi/roadmap.

| Folder / file | Isi |
|---------------|-----|
| [`docs/`](../docs/) | Spesifikasi, riset, roadmap (belum tentu sudah di-build) |
| **`docs-development-shipped/`** (di sini) | Apa yang benar-benar di-ship: perilaku runtime, endpoint, file kunci, PR |

Setiap file `*.md` di folder ini = satu entri rilis.

**Roadmap WhatsApp (media, bukti transfer, stok):** [`docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md`](../docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md).

| Entri | Isi |
|-------|-----|
| [inbox-media-fase1.md](./inbox-media-fase1.md) | Media di Inbox + proxy Meta |
| [ai-image-caption.md](./ai-image-caption.md) | AI memproses caption gambar/video/dokumen |
| [ai-stock-guard-fase4.md](./ai-stock-guard-fase4.md) | Stok tersedia & penjagaan qty di order flow AI |
| [ai-order-chat-lookup.md](./ai-order-chat-lookup.md) | Order lookup scoped via chat + deny third-party |
| [ai-recipient-policy.md](./ai-recipient-policy.md) | Jawaban kebijakan pesan atas nama orang lain |
| [ai-structured-order.md](./ai-structured-order.md) | Pesanan multi-baris + guard catalog hijack |
| [payment-proof-fase2.md](./payment-proof-fase2.md) | Bukti transfer, limit 5x penolakan, unblock owner |
| [inbox-media-s3.md](./inbox-media-s3.md) | Persist media inbox ke Amazon S3 (Fase 1b) |
| [ai-image-context.md](./ai-image-context.md) | **Planned** — vision match katalog (3c) + fallback gambar (3d) |
