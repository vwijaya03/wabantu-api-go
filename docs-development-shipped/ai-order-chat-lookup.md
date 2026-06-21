# Order lookup scoped via chat (Fase 4b)

Catatan rilis: pembeli bisa cek pesanan milik chat mereka; lookup pembeli/pesanan orang lain ditolak deterministik.

**Routing lengkap:** [docs/WHATSAPP_AI_ROUTING.md](../docs/WHATSAPP_AI_ROUTING.md)  
**Intent & ownership:** [docs/ORDER_CUSTOMER_CHAT.md](../docs/ORDER_CUSTOMER_CHAT.md)  
**Roadmap:** [docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md](../docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md) §Fase 4b

---

## Perilaku runtime

| Pesan pembeli | Path metadata | LLM? |
|---------------|---------------|------|
| `saya masih punya pesanan aktif nggak?` | `order_status` | Tidak |
| `pembeli dengan nama Lavana Snack ada?` | `order_lookup_denied` | Tidak |
| `pembeli atas nama saya ada?` | `order_status` | Tidak |
| `pembeli atas nama ini ada? Nama: supriyanto` + order scoped penerima cocok | `order_status` | Tidak |
| Hint nama tanpa order di scope chat | `order_status` (balasan tidak ditemukan) | Tidak |

Query order selalu scoped `conversation_id` + `contact_id` — tidak ada `SELECT` global by nama customer lain.

---

## Resolve order (prioritas)

1. `WB-...` eksplisit di pesan
2. `WB-...` dari history outbound (`pesanan tadi`, `yang barusan`, dll.)
3. Hint penerima `Nama:` / `HP:` → match `shipping_address`
4. Pesanan aktif / terbaru milik chat

---

## File kunci

| File | Peran |
|------|-------|
| `ai/order_customer.go` | `IsThirdPartyBuyerLookup`, `IsSelfBuyerOrderLookup`, `resolvePersistedOrderStatus` |
| `ai/autoreply.go` | Early routing + guard sebelum FAQ |
| `ai/classifier_routing.go` | `tryFAQDirectAnswer` skip order lookup |
| `ai/reply_meta.go` | `PathOrderLookupDenied` |
| `ai/order_buyer_lookup_test.go` | Unit test skenario |

---

## Test

```bash
encore test ./ai/ -run 'BuyerLookup|OrderStatus|OrderOwnership' -count=1
```
