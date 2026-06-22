# Structured multi-line order (checkout AI)

Catatan rilis: pembeli bisa mengirim daftar barang bernomor dalam satu pesan; bot mem-parse qty (termasuk lusin), ukuran, dan lanjut ke `order_flow` — bukan daftar katalog generik.

**Routing lengkap:** [docs/WHATSAPP_AI_ROUTING.md](../docs/WHATSAPP_AI_ROUTING.md)

---

## Perilaku runtime

| Pesan pembeli | Path metadata | LLM? |
|---------------|---------------|------|
| `mau buat pesanan baru` + `1. LOL Best Seller 1 lusin ukuran L` | `order_flow` | Tidak |
| `bisa listkan semua jualan kamu ?` | `catalog_db` | Tidak |
| `minta rekomendasi best seller` (tanpa qty) | `catalog_db` | Tidak |
| Produk bernama "LOL Best Seller" dalam pesan order | `order_flow` (bukan recommendation) | Tidak |

### Format pesan yang dikenali

- Header: `mau buat pesanan baru`, `barang yang dibeli`, atau kombinasi keduanya
- Baris: `1. <nama produk> <qty> [lusin] [ukuran L/XL/...]`
- `1 lusin` → 12 pcs (reuse `parseOrderQty`)

### Alur checkout

1. Parse baris → match `business_catalog_item` per baris
2. Redis `orderState.items[]` — ringkasan multi-item sebelum minta penerima
3. Stock guard per baris (`guardStructuredOrderStock`)
4. `persistDraftOrderMulti` → `tenant.order.items` JSON array

Baris tidak cocok katalog: balasan deterministik baris mana yang tidak dikenali (bukan list katalog penuh).

---

## Perbaikan routing (catalog hijack)

**Akar masalah:** `IsRecommendationRequest` memakai substring `"best seller"` → produk "LOL Best Seller" memicu `buildCatalogListReply` sebelum `order_intent`.

**Mitigasi:**

- `IsRecommendationRequest` — frasa intent saja + guard `IsExplicitNewOrderStart`, `hasPurchaseIntent`, `mentionsOrderQty`, `IsStructuredOrderList`
- `replyFromBusinessCatalog` — early return `handled=false` untuk order terstruktur
- `autoreply.go` — `order_intent` / `IsStructuredOrderList` **sebelum** blok katalog
- `IsCatalogListQuestion` — tambah `listkan`, `semua jualan kamu`

---

## File kunci

| File | Peran |
|------|-------|
| `ai/order_structured.go` | `IsStructuredOrderList`, `parseStructuredOrderLines`, multi-item stock guard |
| `ai/order_flow.go` | `persistDraftOrderMulti`, `normalizeOrderState` sync Items |
| `ai/autoreply.go` | Routing prioritas + entry `handleOrderFlow` |
| `ai/catalog_reply.go` | Guard + `IsCatalogListQuestion` |
| `ai/wa_intent_extended.go` | `IsRecommendationRequest` |
| `ai/sales_format.go` | `formatMultiOrderSummary` |
| `ai/order_structured_test.go` | Unit test parse + routing regresi |

---

## Test

```bash
encore test ./ai/ -run 'StructuredOrder|Recommendation|ParseStructured|Hijacked|listkan' -count=1
encore test ./ai/ -count=1
```

---

## QA manual (inbox)

1. Paste pesan 3 baris LOL Best Seller (L/XL/XXL, 1 lusin) → path `order_flow`, ringkasan 3 item
2. Lanjutkan penerima + alamat → draft order dengan 3 `OrderItem`
3. `bisa listkan semua jualan kamu ?` → path `catalog_db`
