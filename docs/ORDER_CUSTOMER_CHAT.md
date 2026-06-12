# Order via Chat — Nomor Pesanan, Status & Pembatalan (api-go)

Fitur AI WhatsApp untuk **nomor pesanan pembeli**, **cek status pesanan**, dan **pembatalan lewat chat** — tanpa kolom DB baru.

> Panduan produk (CS/owner): `web-frontend/docs/ORDER_CUSTOMER_CHAT.md`

---

## Nomor pesanan (`WB-XXXXXXXX`)

Format singkat dari UUID order (8 karakter hex pertama, tanpa strip):

| UUID internal | Nomor untuk pembeli |
|---------------|---------------------|
| `eb76635c-8439-42f1-9a45-dfa31bc0bbf4` | `WB-EB76635C` |

Implementasi: `ai/order_customer.go` → `FormatOrderNumber()`.

Nomor ini:

- Dikirim saat order flow selesai (`orderCompleteMessageWithRef` di `ai/sales_format.go`)
- Ditampilkan saat pembeli cek status atau batalkan pesanan
- **Bukan** nomor urut global — tetap unik per tenant karena berasal dari UUID

---

## Intent sebelum classifier / LLM

Handler di `ai/autoreply.go` (setelah load history, **sebelum** order flow Redis & classifier):

| Intent | Deteksi | Path metadata | `orderAction` |
|--------|---------|---------------|---------------|
| Batalkan pesanan | `IsOrderCancelRequest()` | `order_cancel` | `cancel` atau `cancel_draft` |
| Cek status pesanan | `IsOrderStatusInquiry()` | `order_status` | `status` |

### Frasa pembatalan (contoh)

`batalkan pesanan`, `mau saya batalkan`, `batalin`, `tidak jadi`, `cancel order`, kata `batalkan` / `cancel` sebagai kata utuh.

### Frasa cek status (contoh)

`pesanan saya`, `pesanan atas nama`, `ada pesanan`, `status pesanan`, `nomor pesanan`, kombinasi `pesanan`/`order` + `?` / `ada` / `cek` / `status`.

---

## Alur pembatalan

1. **Checkout aktif (Redis order state, belum persist DB)**  
   → clear Redis → balas `orderFlowCancelReply` → metadata `orderAction: cancel_draft`

2. **Order tersimpan di `tenant.order`**  
   → load order terbaru per `conversation_id`  
   → jika status `draft` / `processing` / `confirmed` / `paid`: `UPDATE status = cancelled`  
   → `finance.RemoveOrderIncomeTransaction` (idempoten)  
   → balas dengan nomor `WB-...` → log `rlog.Info("order cancelled by customer via chat", ...)`

3. **Sudah `shipped` / `completed`**  
   → tolak pembatalan lewat chat (arahkan ke CS)

4. **Sudah `cancelled`**  
   → balas bahwa pesanan sudah dibatalkan

---

## Alur cek status

`loadLatestOrderForConversation` → `formatPersistedOrderSummary` (produk, penerima, total, status label ID).

Tidak memakai LLM — jawaban deterministik dari DB.

---

## Metadata pesan (audit / inbox)

Struct `AiReplyMeta` (`ai/reply_meta.go`):

```json
{
  "path": "order_cancel",
  "reason": "non_question",
  "llmUsed": false,
  "orderId": "eb76635c-8439-42f1-9a45-dfa31bc0bbf4",
  "orderAction": "cancel"
}
```

Field `orderId` dan `orderAction` juga dicatat di `rlog` pada `LogAndRecord`.

---

## File terkait

| File | Peran |
|------|-------|
| `ai/order_customer.go` | Format nomor, deteksi intent, load/cancel order, template balasan |
| `ai/order_customer_test.go` | Unit test frasa & format |
| `ai/autoreply.go` | Hook handler sebelum classifier |
| `ai/order_flow.go` | `persistDraftOrder` mengembalikan `orderID` |
| `ai/sales_format.go` | Pesan complete + nomor pesanan |
| `ai/reply_meta.go` | Path `order_cancel`, `order_status`, field audit |
| `finance/order_income.go` | Cleanup income saat cancel |

---

## Changelog

| Tanggal | Perubahan |
|---------|-----------|
| 2026-06-08 | Nomor pesanan WB-*, cancel & status inquiry via chat, metadata audit |
