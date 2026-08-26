# Fitur yang Sudah Rilis (Development Shipped)

Folder ini berisi **catatan implementasi** fitur yang sudah (atau sedang) di-merge — bukan spesifikasi/roadmap.

| Folder / file | Isi |
|---------------|-----|
| [`docs/`](../docs/) | Spesifikasi, riset, roadmap (belum tentu sudah di-build) |
| **`docs-development-shipped/`** (di sini) | Apa yang benar-benar di-ship: perilaku runtime, endpoint, file kunci, PR |

## Konvensi penamaan file

Entri baru memakai format:

```
YYYY-MM-DD_HHMMSS_slug.md
```

| Bagian | Keterangan |
|--------|------------|
| `YYYY-MM-DD` | Tanggal ship / merge (atau tanggal PR jika belum merge) |
| `HHMMSS` | Waktu 24 jam (UTC+7) — untuk sort **terbaru di atas** saat diurutkan descending |
| `slug` | Ringkasan tema, kebab-case |

Contoh: `2026-08-25_233200_tenant-schema-qualified-pool-retry.md`

Setiap file `*.md` = satu batch rilis (bisa mencakup beberapa PR bertema sama). Struktur isi:

- **Masalah / Kebutuhan**
- **Perubahan**
- **File utama**
- **Testing**
- **Catatan deploy**

Entri lama tanpa prefix datetime (mis. `inbox-media-fase1.md`) tetap valid; entri baru mengikuti konvensi di atas.

**Roadmap WhatsApp (media, bukti transfer, stok):** [`docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md`](../docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md).

---

## Entri terbaru (2026-08)

| File | Isi | Status |
|------|-----|--------|
| [2026-08-26_142400_tenant-access-consent.md](./2026-08-26_142400_tenant-access-consent.md) | Consent owner sebelum super admin Pantau tenant | PR [#130](https://github.com/vwijaya03/wabantu-api-go/pull/130) |
| [2026-08-26_142000_finance-audit-perf-quick-wins.md](./2026-08-26_142000_finance-audit-perf-quick-wins.md) | Test HPP wallet, timeout HTTP, index inbox, lazy migrate `sync.Once` | PR [#129](https://github.com/vwijaya03/wabantu-api-go/pull/129) |
| [2026-08-26_090100_events-super-admin-cross-tenant.md](./2026-08-26_090100_events-super-admin-cross-tenant.md) | Hint 403 super_admin saat acara milik tenant lain | PR [#128](https://github.com/vwijaya03/wabantu-api-go/pull/128) |
| [2026-08-25_233200_tenant-schema-qualified-pool-retry.md](./2026-08-25_233200_tenant-schema-qualified-pool-retry.md) | Schema-qualified SQL, pool retry 08P01, readiness gate | PR [#121](https://github.com/vwijaya03/wabantu-api-go/pull/121)–[#127](https://github.com/vwijaya03/wabantu-api-go/pull/127) |

---

## Entri sebelumnya (WhatsApp / AI)

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
| [ai-triage-catalog-snapshot.md](./ai-triage-catalog-snapshot.md) | Loop AI Triage: embed katalog tenant di auto-gen regression test |
