# Shipped: Stok guard AI (Fase 4)

**Status:** Siap merge  
**Branch:** `feat/ai-stock-guard`  
**Tanggal:** 2026-06  
**Roadmap terkait:** [`docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md`](../docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md) — Fase 4

---

## Apa yang di-ship

AI WhatsApp memakai **stok available** (`on_hand - reserved`) dan **menolak qty melebihi stok** saat order flow — tanpa menawarkan produk alternatif (v1).

---

## Perilaku runtime

| Situasi | Perilaku |
|---------|----------|
| Tanya stok produk | `buildCatalogItemReply` pakai `StockAvailable` (available, bukan on_hand mentah) |
| Order qty > stok | Tolak, tetap di step `ask_qty`, minta kurangi |
| Stok habis (≤ 0) | Tolak lanjut pesan untuk produk tersebut |
| Tenant tanpa inventory / item tidak tracked | Perilaku lama (tanpa guard stok) |
| Sebelum `persistDraftOrder` | Precheck DB `lookupCatalogStockAvailable` (defense in depth) |

---

## File kunci

| File | Peran |
|------|--------|
| `ai/order_catalog.go` | `enrichCatalogStock` — `SUM(GREATEST(on_hand - reserved, 0))` |
| `ai/order_stock_guard.go` | Helper validasi qty vs stok, pesan tolak |
| `ai/order_flow_sim.go` | Guard di `AdvanceOrderFlow` (sim + test) |
| `ai/autoreply.go` | Guard di order flow production |
| `ai/order_flow.go` | `persistDraftOrder` precheck stok |

---

## Test

```bash
cd api-go
encore test ./ai/ -run 'Stock|AdvanceOrderFlow_rejectsQty' -count=1
```

---

## Sengaja belum

- Bundle (`is_bundle = true`) belum di-enrich stok — guard tidak berlaku untuk bundle di v1
- Rekomendasi produk alternatif saat habis (keputusan produk: tidak di v1)
