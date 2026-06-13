# Order Status Buyer — Research & 30 Unit Tests

Analisis thread produksi `conversation_id=7f3f02c4-6a0a-4e01-912f-0be70dde81ca` (Jun 2026) — pembeli tanya pesanan aktif/pending tapi bot jawab greeting atau order lama yang sudah dibatalkan.

## Cara menjalankan

```bash
cd api-go
encore test ./ai/ -run TestOrderStatusBuyer30 -count=1
encore test ./ai/ -count=1
```

---

## Ringkasan insiden

| Waktu | User | Bot | Masalah |
|-------|------|-----|---------|
| 03:38 | `siang` | Greeting | OK (sapaan murni) |
| 03:38 | `apa saya masih punya pesanan yang pending ya?` | Status **WB-EAA94534 dibatalkan** (Hello Kitty) | Harus jawab **tidak ada aktif**, bukan order cancelled |
| 03:39 | User protes → LLM hallucinate | LLM bilang tidak pending | Inkonsisten dengan status sebelumnya |
| 03:46–03:47 | `halo min apakah saya punya pesanan aktif?` (×3) | **Greeting** | Status inquiry + greeting prefix |
| 03:47 | `saya punya pesanan nggak?` | Status WB-947FC5C0 aktif | OK (setelah user sudah order baru) |

---

## Root cause

### Bug 1 — Greeting menelan status inquiry

**Gejala:** `halo min apakah saya punya pesanan aktif?` → `Selamat pagi kak! Ada yang bisa aku bantu?`

**Penyebab:** Router `autoreply.go` cek `IsGreetingLike` **sebelum** `IsOrderStatusInquiry`. `isPureGreetingCore` match prefix `halo ` meski ada pertanyaan pesanan.

**Fix:**
1. `IsOrderStatusInquiry` diprioritaskan sebelum greeting di router.
2. `IsGreetingLike`: jika `isCommerceDominant(text)` → bukan greeting (commerce/status menang).

### Bug 2 — Tanya pending/aktif tapi dapat order cancelled

**Gejala:** `apa saya masih punya pesanan pending?` → detail Hello Kitty **dibatalkan**.

**Penyebab:** `resolvePersistedOrderAction` fallback ke `loadLatestOrderForConversation` (termasuk cancelled).

**Fix:** `WantsActiveOrderOnly` + `resolvePersistedOrderStatus` — filter order aktif; jika tidak ada → `orderNoActiveOrdersReply()`.

### Bug 3 — Cancel tanpa pilih nomor pesanan

**Gejala:** `batalkan pesanan` bisa kena order sembarang (latest) di chat yang punya banyak order.

**Penyebab:** Cancel langsung ke latest cancellable order.

**Fix:** `resolvePersistedOrderCancel` — tanpa `WB-XXXX` di chat, tampilkan daftar pesanan cancellable + minta `batalkan WB-XXXXXXXX`. Lookup order **scoped `conversation_id`** (bukan contact lain).

---

## Perilaku pembeli (research)

| Pola chat | Intent | Respons yang benar |
|-----------|--------|-------------------|
| `halo min punya pesanan aktif?` | Status + greeting prefix | Ringkasan aktif / tidak ada aktif |
| `saya punya pesanan nggak?` | Active check | Ya/tidak + nomor jika ada |
| `pending?` / `masih ada order?` | Active only | Jangan tampilkan cancelled |
| `batalkan pesanan` (multi order) | Cancel pick | List WB-XXXX, minta pilih |
| `batalkan WB-947FC5C0` | Cancel spesifik | Cancel hanya order itu (same convo) |
| `siang` / `halo min` | Greeting murni | Sapaan |

---

## Suite 30 test — `TestOrderStatusBuyer30`

| Kategori | Count | Fokus |
|----------|-------|-------|
| `greeting_status` | 10 | halo + tanya pesanan ≠ greeting |
| `active_only` | 8 | WantsActiveOrderOnly |
| `cancel_pick_ref` | 6 | WB ref + pick list |
| `sim_routing` | 4 | ConversationSimulator path |
| `production` | 2 | Replay thread |

File: `order_status_buyer_gen.go`, `order_status_buyer_30_test.go`.

---

## UI dashboard (web-frontend)

Permintaan terpisah dari chat AI:

1. **Tombol refresh** di halaman `/dashboard/orders` — reload daftar dari server.
2. **Nomor/title pesanan clickable** — buka detail/edit pesanan (dialog edit sebagai detail view).

---

## Referensi

- Thread log produksi Jun 2026 (user-provided JSON).
- Prior art: [CHAOS_BUYER_RESEARCH.md](./CHAOS_BUYER_RESEARCH.md), `order_guard_gen.go` (50 case).
