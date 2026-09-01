# Indeks Dokumentasi api-go

Peta navigasi untuk menjawab pertanyaan tentang backend WABantu. Mulai dari sini jika tidak tahu file mana yang harus dibuka.

**Dokumen kanonik routing WhatsApp → AI:** [WHATSAPP_AI_ROUTING.md](./WHATSAPP_AI_ROUTING.md)

**Fitur sudah rilis (bukan roadmap):** [docs-development-shipped/](../docs-development-shipped/)

---

## Kalau ditanya…

| Pertanyaan umum | Buka |
|-----------------|------|
| Alur pesan WA dari webhook sampai AI balas? | [WHATSAPP_AI_ROUTING.md](./WHATSAPP_AI_ROUTING.md) |
| Loop engineering otomatis (AI Triage)? | [AI_TRIAGE_LOOP_NEXT_DEV.md](./AI_TRIAGE_LOOP_NEXT_DEV.md) |
| Kenapa pesan ini dapat path `order_status` / `llm` / `catalog_db`? | [WHATSAPP_AI_ROUTING.md](./WHATSAPP_AI_ROUTING.md) → §Decision tree + §Tabel path |
| Bagaimana AI mendeteksi intent (greeting, order, katalog)? | [WHATSAPP_AI_ROUTING.md](./WHATSAPP_AI_ROUTING.md) → §Tabel deteksi intent |
| Cek / batal pesanan lewat chat pembeli | [ORDER_CUSTOMER_CHAT.md](./ORDER_CUSTOMER_CHAT.md) |
| Lookup pembeli orang lain vs scoped (Lavana Snack, supriyanto) | [ORDER_CUSTOMER_CHAT.md](./ORDER_CUSTOMER_CHAT.md) · shipped: [ai-order-chat-lookup.md](../docs-development-shipped/ai-order-chat-lookup.md) |
| Ownership order — siapa boleh lihat WB-…? | [ORDER_OWNERSHIP_RESEARCH.md](./ORDER_OWNERSHIP_RESEARCH.md) |
| Stok per gudang di order flow AI | [ai-stock-guard-fase4.md](../docs-development-shipped/ai-stock-guard-fase4.md) |
| Media gambar + caption di chat | [ai-image-caption.md](../docs-development-shipped/ai-image-caption.md) |
| Tampilan gambar di inbox (proxy Meta) | [inbox-media-fase1.md](../docs-development-shipped/inbox-media-fase1.md) |
| Persist media inbox ke S3 | [inbox-media-s3.md](../docs-development-shipped/inbox-media-s3.md) |
| Vision match katalog dari gambar (planned) | [ai-image-context.md](../docs-development-shipped/ai-image-context.md) |
| Bagaimana RAG / vector search FAQ & katalog? | [RAG_VECTOR_RETRIEVAL.md](./RAG_VECTOR_RETRIEVAL.md) |
| Kenapa FAQ direct / retrieval fallback? | [RAG_VECTOR_RETRIEVAL.md](./RAG_VECTOR_RETRIEVAL.md) → §Query pipeline |
| Setup Pinecone & embedding? | [RAG_VECTOR_RETRIEVAL.md](./RAG_VECTOR_RETRIEVAL.md) → §Secrets |
| Rollout RAG / webhook cleanup (2026-08-31)? | [20260831_101000_rag-hardening-webhook-cleanup.md](../docs-development-shipped/20260831_101000_rag-hardening-webhook-cleanup.md) |
| Import katalog dari teks (caption WA, deskripsi toko)? | [CATALOG_TEXT_IMPORT.md](./CATALOG_TEXT_IMPORT.md) |
| Revamp UI katalog + fix upsert import (2026-08-31)? | [20260831_161500_catalog-revamp-text-import.md](../docs-development-shipped/20260831_161500_catalog-revamp-text-import.md) |
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
| [inbox-media-fase1.md](../docs-development-shipped/inbox-media-fase1.md) | Media inbox — proxy Meta, field `media` di GetMessages |
| [inbox-media-s3.md](../docs-development-shipped/inbox-media-s3.md) | Persist media inbox ke Amazon S3 |
| [ai-image-context.md](../docs-development-shipped/ai-image-context.md) | Vision match katalog + fallback gambar (planned) |
| [LIMITS_AND_QUOTAS.md](../LIMITS_AND_QUOTAS.md) | Kuota paket, AI token, broadcast |

## RAG / vector retrieval

| File | Isi |
|------|-----|
| [RAG_VECTOR_RETRIEVAL.md](./RAG_VECTOR_RETRIEVAL.md) | Arsitektur Pinecone + OpenAI, indexing outbox, query pipeline, rollout |
| [20260831_101000_rag-hardening-webhook-cleanup.md](../docs-development-shipped/20260831_101000_rag-hardening-webhook-cleanup.md) | Hardening produksi + webhook kanonik |
| [20260830_173800_rag-kb-retrieval.md](../docs-development-shipped/20260830_173800_rag-kb-retrieval.md) | Wire autoreply KB hybrid |
| [20260830_173800_rag-outbox-indexing.md](../docs-development-shipped/20260830_173800_rag-outbox-indexing.md) | Outbox + kolom embedding |

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
| [CATALOG_TEXT_IMPORT.md](./CATALOG_TEXT_IMPORT.md) | Import katalog dari teks (dashboard) |
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
| [DEPLOY_ENCORE_CLOUD.md](./DEPLOY_ENCORE_CLOUD.md) | Deploy Encore Cloud (+ hot-fix `business_profile` dynamic grants) |
| [DEPLOY_REDIS.md](./DEPLOY_REDIS.md) | Redis production |
| [STAGING_ACCESS.md](./STAGING_ACCESS.md) | Akses staging |

## Riset, audit, test

| File | Isi |
|------|-----|
| [WHATSAPP_BUYER_BEHAVIOR_TESTS.md](./WHATSAPP_BUYER_BEHAVIOR_TESTS.md) | Test perilaku buyer |
| [AI_TRIAGE_LOOP_NEXT_DEV.md](./AI_TRIAGE_LOOP_NEXT_DEV.md) | Loop engineering AI Triage (analyze → regression → PR) |
| [CHAOS_BUYER_RESEARCH.md](./CHAOS_BUYER_RESEARCH.md) | Riset chaos buyer |
| [BACKEND_AUDIT_REPORT.md](./BACKEND_AUDIT_REPORT.md) | Laporan audit backend |
