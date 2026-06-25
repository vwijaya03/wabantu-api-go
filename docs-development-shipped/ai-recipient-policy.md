# Shipped: Kebijakan pesan atas nama orang lain

**Status:** Siap merge  
**Roadmap:** [`docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md`](../docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md) — konsultasi checkout (bukan Fase 2 payment proof)

---

## Masalah

Pertanyaan *"mau beli atas nama orang lain bisa ya?"* dijawab `catalog_db` dengan harga Abon Sapi (eceran per pcs) karena `IsConsultingPurchaseQuestion` + history hijack katalog.

## Perilaku setelah fix

| Pesan | Path | Jawaban |
|-------|------|---------|
| `mau beli atas nama orang lain bisa ya?` | `recipient_policy` | Boleh + minta nama/HP penerima saat checkout |
| `pembeli dengan nama Lavana Snack ada?` | `order_lookup_denied` | (regresi 4b, tidak berubah) |

FAQ Knowledge Base diutamakan jika skor match cukup.

## File kunci

| File | Peran |
|------|-------|
| `ai/recipient_policy.go` | `IsRecipientPolicyQuestion`, `buildRecipientPolicyReply` |
| `ai/autoreply.go` | Routing sebelum `replyFromBusinessCatalog` |
| `ai/sales_state.go` | Exclude dari `IsConsultingPurchaseQuestion` |
| `ai/catalog_reply.go` | `resolveCatalogMatch` skip recipient policy |
| `ai/sales_intent.go` | `SalesTopicRecipient` |

## Test

```bash
encore test ./ai/ -run RecipientPolicy -count=1
```
