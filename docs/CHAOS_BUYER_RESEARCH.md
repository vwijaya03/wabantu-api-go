# Chaos Buyer — Research & 100 Unit Tests

Dokumentasi analisis thread produksi `convoId=7f3f02c4-6a0a-4e01-912f-0be70dde81ca` (Omah Apparel, Jun 2026) dan perbaikan routing cancel/order.

## Cara menjalankan

```bash
cd api-go
encore test ./ai/ -run TestChaosBuyer100 -count=1
encore test ./ai/ -count=1
```

---

## Ringkasan insiden (dari log message DB)

| Waktu | User | Bot | Masalah |
|-------|------|-----|---------|
| 16:42 | `mau pesen boxer pria mono spot 5 paket ya` | Ringkasan MONO SPOT → minta penerima | OK |
| 01:11 | `ga jadi beli... ada nomor pesanan?` | **Batalkan WB-EAA94534** (order DB lama) | Draft sudah expired; cancel kena order lama same contact |
| 01:13 | `mau pesen boxer mono spot 5 paket` | Ringkasan **Hello Kitty** | Salah produk / history bias |
| 01:13 | `boxer mono spot bukan hello kitty` | Minta penerima lagi | Koreksi produk belum tegas |
| 01:14 | `batal` | **Tetap minta penerima** | `batal` tidak terdeteksi cancel |
| 01:15 | `cancel` | Order dibatalkan | OK |
| 01:15 | `saya mau beli 5 paket boxer mono spot` | Ringkasan **DE WASA** + tanya qty lagi | Salah SKU boxer |

---

## Root cause (5 bug)

### Bug 1 — `"batal"` tidak membatalkan draft

**Gejala:** User ketik `batal` di step `ask_recipient`, bot ulang minta nama/HP.

**Penyebab:** Regex cancel `\bbatalkan?\b` di Go = `batalka(n)?`, **bukan** kata `batal`.

**Fix:** `orderCancelWordRe` → `\b(batal(?:kan)?|cancel)\b` + helper `IsDraftOrderCancelRequest`.

**Sumber:** [RE2 syntax — `?` quantifier](https://github.com/google/re2/wiki/Syntax); analisis kode `order_customer.go`.

---

### Bug 2 — `"ga jadi beli" + tanya nomor pesanan` membatalkan order DB lama

**Gejala:** User belum selesai order baru, tapi order **WB-EAA94534** (sesi kemarin, same `conversation_id`) ikut dibatalkan.

**Penyebab:** `IsOrderCancelRequest` match substring `"ga jadi"` **sebelum** `IsOrderStatusInquiry`. Tanpa draft Redis aktif → `handleCustomerOrderCancel` load **latest order by conversation_id**.

**Fix:** `ShouldCancelPersistedOrder` — soft regret + pertanyaan status (`?`, `nomor pesanan`) → **status inquiry**, bukan cancel DB. Explicit `batalkan pesanan` / `cancel` tetap cancel.

**Sumber:** Meta WA CSW — satu thread = satu conversation ([docs/META_WHATSAPP_MESSAGING_AND_BILLING.md](./META_WHATSAPP_MESSAGING_AND_BILLING.md)); desain `loadLatestOrderForConversation` di `order_customer.go`.

---

### Bug 3 — `"batalkan ya kak"` terdeteksi greeting

**Gejala:** Cancel dengan suffix `kak` tidak reset flow.

**Penyebab:** `IsCasualChatOpener` anggap token `kak` = sapaan → `IsGreetingLike` true **sebelum** cancel check di `autoreply.go`.

**Fix:** Exclude `IsDraftOrderCancelRequest` dari `IsGreetingLike` / `IsCasualChatOpener`.

---

### Bug 4 — Order baru dapat produk Hello Kitty / DE WASA

**Gejala:** User minta **mono spot pria**, bot pilih Hello Kitty atau DE WASA.

**Penyebab:** `matchCatalogItem` scoring token `boxer` generik; tidak boost `mono spot` / `pria`; tidak exclude `bukan hello kitty`.

**Fix:** `catalogPhraseBoost`, gender hint `pria`/`perempuan`, `catalogExcludeHints`, `tryApplyProductRevision`.

**Sumber:** Pola e-commerce disambiguation — [Amazon search relevance (multi-signal ranking)](https://www.amazon.science/publications); praktik chatbot WA — explicit SKU mention harus menang atas fuzzy token overlap.

---

### Bug 5 — `"ganti"` qty revisi vs ganti produk bentrok

**Gejala:** `"ga jadi ganti 3 pcs"` mengubah produk instead of qty.

**Penyebab:** `tryApplyProductRevision` trigger pada kata `ganti` sebelum `tryApplyQtyRevision`.

**Fix:** Urutan qty dulu, product revision hanya pada sinyal eksplisit (`bukan`, `ganti jadi`, `ganti produk`).

---

## Model pembeli "kacau" (untuk test)

```
browse → order → (timeout) → regret+status? → order lagi → salah produk
       → koreksi → batal (gagal) → cancel (OK) → order lagi → salah SKU
```

**State yang harus dijaga:**

| Event | Redis draft | DB order |
|-------|-------------|----------|
| `batal` / `cancel` during checkout | CLEAR | unchanged |
| `ga jadi` + `ada nomor pesanan?` | CLEAR if any | **READ status only** |
| `batalkan pesanan` (no draft) | — | CANCEL latest cancellable |
| New `mau pesen X` after cancel | NEW draft | unchanged |

---

## Suite 100 test — `TestChaosBuyer100`

| Kategori | Count | Fokus |
|----------|-------|-------|
| `cancel_detection` | 8 | `batal`, `cancel`, regex |
| `soft_cancel_status` | 3 | ga jadi + status → no DB cancel |
| `soft_cancel_alone` | 4 | ga jadi standalone → may cancel DB |
| `explicit_persisted_cancel` | 5 | batalkan pesanan |
| `batal_reset` | 20 | batal di ask_variant/qty/recipient |
| `cancel_restart` | 15 | cancel → order baru |
| `product_match` / `product_flow` | 12 | mono spot vs hello kitty vs de wasa |
| `product_correction` | 1 | bukan hello kitty |
| `transcript_replay` | 12 | replay thread produksi |
| `cancel_restart_pad` | 8+ | padding |

File: `chaos_buyer_gen.go`, `chaos_buyer_100_test.go`.

---

## Referensi & sumber research

1. **Thread log produksi** — message JSON `conversation_id=7f3f02c4-6a0a-4e01-912f-0be70dde81ca` (user-provided, Jun 2026).
2. **Meta WhatsApp CSW** — satu conversation window 24 jam; [META_WHATSAPP_MESSAGING_AND_BILLING.md](./META_WHATSAPP_MESSAGING_AND_BILLING.md).
3. **RE2 / Go regexp** — `\bbatalkan?\b` ≠ `batal`; [RE2 wiki](https://github.com/google/re2/wiki/Syntax).
4. **FSM order flow** — `order_flow_sim.go`, `AdvanceOrderFlow` state machine pattern (Martin, *Practical Statecharts*).
5. **Adversarial buyer testing** — suite 2000 cases prior art: [WHATSAPP_BUYER_BEHAVIOR_TESTS.md](./WHATSAPP_BUYER_BEHAVIOR_TESTS.md).
6. **E-commerce catalog disambiguation** — multi-token scoring + negative keywords (industry practice; cf. Amazon Science publication on search relevance).

---

## Hasil test

```
encore test ./ai/ -run TestChaosBuyer100 -count=1  → PASS (100 cases)
encore test ./ai/ -count=1                          → PASS
```
