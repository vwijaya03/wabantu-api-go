# Indeks Dokumentasi api-go

Peta navigasi untuk menjawab pertanyaan tentang backend WABantu. Mulai dari sini jika tidak tahu file mana yang harus dibuka.

**Dokumen kanonik routing WhatsApp → AI:** [WHATSAPP_AI_ROUTING.md](./WHATSAPP_AI_ROUTING.md)

**Fitur sudah rilis (bukan roadmap):** [docs-development-shipped/](../docs-development-shipped/)

---

## Kalau ditanya…

| Pertanyaan umum | Buka |
|-----------------|------|
| Alur pesan WA dari webhook sampai AI balas? | [WHATSAPP_AI_ROUTING.md](./WHATSAPP_AI_ROUTING.md) |
| Kenapa pesan ini dapat path `order_status` / `llm` / `catalog_db`? | [WHATSAPP_AI_ROUTING.md](./WHATSAPP_AI_ROUTING.md) → §Decision tree + §Tabel path |
| Bagaimana AI mendeteksi intent (greeting, order, katalog)? | [WHATSAPP_AI_ROUTING.md](./WHATSAPP_AI_ROUTING.md) → §Tabel deteksi intent |
| Cek / batal pesanan lewat chat pembeli | [ORDER_CUSTOMER_CHAT.md](./ORDER_CUSTOMER_CHAT.md) |
| Lookup pembeli orang lain vs scoped (Lavana Snack, supriyanto) | [ORDER_CUSTOMER_CHAT.md](./ORDER_CUSTOMER_CHAT.md) · shipped: [ai-order-chat-lookup.md](../docs-development-shipped/ai-order-chat-lookup.md) |
| Ownership order — siapa boleh lihat WB-…? | [ORDER_OWNERSHIP_RESEARCH.md](./ORDER_OWNERSHIP_RESEARCH.md) |
| Stok per gudang di order flow AI | [ai-stock-guard-fase4.md](../docs-development-shipped/ai-stock-guard-fase4.md) |
| Media gambar + caption di chat | [ai-image-caption.md](../docs-development-shipped/ai-image-caption.md) |
| Tagihan Meta vs kuota WABantu | [META_WHATSAPP_MESSAGING_AND_BILLING.md](./META_WHATSAPP_MESSAGING_AND_BILLING.md) |
| Kuota trial / paket / routing model AI | [LIMITS_AND_QUOTAS.md](../LIMITS_AND_QUOTAS.md) |
| Roadmap WA (media, bukti transfer, stok) | [WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md](./WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md) |
| Bukti transfer, limit 5x penolakan, unblock | [payment-proof-fase2.md](../docs-development-shipped/payment-proof-fase2.md) |
| Onboarding developer / arsitektur Encore | [DEVELOPER_DOCUMENTATION.md](../DEVELOPER_DOCUMENTATION.md) |
| Cheat sheet dev harian | [APP_FLOW_GUIDE.md](../APP_FLOW_GUIDE.md) |

---

## Runtime WhatsApp & AI

| File | Isi |
|------|-----|
| [WHATSAPP_AI_ROUTING.md](./WHATSAPP_AI_ROUTING.md) | Webhook → Pub/Sub → `ProcessAutoReply` — decision tree & debug |
| [ORDER_CUSTOMER_CHAT.md](./ORDER_CUSTOMER_CHAT.md) | Nomor WB-*, status & cancel via chat |
| [META_WHATSAPP_MESSAGING_AND_BILLING.md](./META_WHATSAPP_MESSAGING_AND_BILLING.md) | CSW 24 jam, template berbayar Meta |
| [WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md](./WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md) | Roadmap fase inbox/media/payment/stok |
| [LIMITS_AND_QUOTAS.md](../LIMITS_AND_QUOTAS.md) | Kuota paket, AI token, broadcast |

## Order & pembeli

| File | Isi |
|------|-----|
| [ORDER_CUSTOMER_CHAT.md](./ORDER_CUSTOMER_CHAT.md) | Intent order di chat, metadata `orderAction` |
| [payment-proof-fase2.md](../docs-development-shipped/payment-proof-fase2.md) | Bukti transfer WA, `payment_status`, limit 5x, unblock |
| [ORDER_OWNERSHIP_RESEARCH.md](./ORDER_OWNERSHIP_RESEARCH.md) | Model ownership `conversation_id` + `contact_id` |
| [ORDER_STATUS_BUYER_RESEARCH.md](./ORDER_STATUS_BUYER_RESEARCH.md) | Riset frasa buyer tanya status |

## Katalog, harga, inventory

| File | Isi |
|------|-----|
| [PRICE_TYPES_AND_CATALOG_PRICING.md](./PRICE_TYPES_AND_CATALOG_PRICING.md) | Tipe harga katalog |
| [CATALOG_IMAGE_IMPORT.md](./CATALOG_IMAGE_IMPORT.md) | Import katalog dari gambar (dashboard) |
| [INVENTORY_MODULE.md](./INVENTORY_MODULE.md) | Modul gudang & stok |

## Keuangan & modul lain

| File | Isi |
|------|-----|
| [FINANCE_MODULE.md](./FINANCE_MODULE.md) | Modul keuangan tenant |
| [TRANSACTION_IMAGE_IMPORT.md](./TRANSACTION_IMAGE_IMPORT.md) | Import transaksi dari gambar |
| [EVENTS_MODULE.md](./EVENTS_MODULE.md) | Modul event |
| [UNIT_ECONOMICS_AND_PRICING.md](./UNIT_ECONOMICS_AND_PRICING.md) | Unit economics produk |

## Deploy & staging

| File | Isi |
|------|-----|
| [DEPLOY_ENCORE_CLOUD.md](./DEPLOY_ENCORE_CLOUD.md) | Deploy Encore Cloud |
| [DEPLOY_REDIS.md](./DEPLOY_REDIS.md) | Redis production |
| [STAGING_ACCESS.md](./STAGING_ACCESS.md) | Akses staging |

## Riset, audit, test

| File | Isi |
|------|-----|
| [WHATSAPP_BUYER_BEHAVIOR_TESTS.md](./WHATSAPP_BUYER_BEHAVIOR_TESTS.md) | Test perilaku buyer |
| [CHAOS_BUYER_RESEARCH.md](./CHAOS_BUYER_RESEARCH.md) | Riset chaos buyer |
| [BACKEND_AUDIT_REPORT.md](./BACKEND_AUDIT_REPORT.md) | Laporan audit backend |
