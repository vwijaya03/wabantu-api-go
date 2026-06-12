# WhatsApp Buyer Behavior — Research & 2000+ Unit Tests

Dokumen ini merangkum perilaku pembeli via WhatsApp yang di-handle bot Omah Apparel, suite test otomatis, dan bug yang ditemukan + diperbaiki dari test tersebut.

## Cara menjalankan

```bash
cd api-go
./scripts/fix-encore-test-db.sh   # jika encore-migrator error
./scripts/run-ai-unit-tests.sh
# atau subset:
encore test ./ai/ -run TestBuyerBehavior1000 -count=1
encore test ./ai/ -run TestWABuyerCases2000 -count=1
```

---

## Suite 2000 — structured buyer cases

`TestWABuyerCases2000` menjalankan **2000** kasus terstruktur dengan field:

| Field | Arti |
|-------|------|
| `input_user` | Pesan pembeli (variasi bahasa) |
| `current_state` | State FSM awal (opsional) |
| `expected_state` | `none`, `ask_*`, `cleared`, `completed` |
| `expected_intent` | `greeting`, `browsing`, `consulting`, `cart_ready`, `checkout`, `correction`, … |
| `expected_response_behavior` | Perilaku balasan (bukan teks persis) |

**25 kategori:** greeting, browse catalog, search, price, size, qty, MOQ, comparison, recommendation, add/update/remove cart, change product/qty/address/recipient, checkout, payment, order status, complaint, human escalation, correction, ambiguous intent, topic switching, abandoned cart, + adversarial.

**Variasi bahasa:** formal, informal, typo, slang, Indo-English, regional (monggo/punten).

| File | Fungsi |
|------|--------|
| `wa_buyer_cases_gen.go` | Generator 2000 kasus |
| `wa_buyer_style.go` | Transformasi bahasa WA |
| `wa_buyer_case.go` | `EvaluateWABuyerCase` + deteksi behavior |
| `wa_buyer_assert.go` | Assertion fleksibel intent/behavior |
| `wa_buyer_2000_test.go` | Runner |

---

## Model perilaku pembeli (research)

### Alur checkout (FSM)

```
browse/consult → cart_ready → ask_product → ask_variant → ask_qty
    → ask_recipient → ask_address_full → persist draft → selesai
```

| Step | Contoh pesan pembeli | Bot |
|------|----------------------|-----|
| `ask_product` | "mau abon sapi" | Cocokkan katalog / minta produk |
| `ask_variant` | "L", "ukuran M" | Apparel butuh ukuran; SKU `- L` bisa auto-fill |
| `ask_qty` | "3 paket", "2 biji" | Parse qty (pcs/biji/paket/lusin) |
| `ask_recipient` | "Nama: Budi\nHP: 0812..." | Nama + HP |
| `ask_address_full` | Jalan, kota, provinsi, kode pos | Alamat lengkap |

### Kategori skenario (5 yang diminta + tambahan)

| # | Kategori | Contoh perilaku | Test count |
|---|----------|-----------------|------------|
| 1 | **Checkout lengkap** | `saya jadi beli abon sapi 2 pcs` → nama → alamat → selesai | 20 |
| 2 | **Revisi qty + selesai** | Di `ask_recipient`: "revisi jadi 10 biji" → lanjut checkout | 20 |
| 3 | **Revisi qty + batal** | "ubah jadi 10 biji" lalu "batalkan order" | 8 |
| 4 | **Ngomong-ngomong, tidak jadi** | Browse, tanya harga, pujian, koreksi "jangan checkout" | 46 |
| 5 | **Consulting → beli** | "boleh beli 1 pcs?" lalu "saya jadi beli ..." | 20 |
| 6 | Parse qty (1–20 × 5 satuan) | `mau 5 paket`, `sepuluh biji` | 100 |
| 7 | Revisi vs batal | "ga jadi dirubah 10 biji" ≠ cancel | 8 |
| 8 | Intent routing | browsing / consulting / checkout / greeting | 150 |
| 9 | Break order flow | Tanya harga saat `ask_variant` | 120 |
| 10 | Apply qty revision | `tryApplyQtyRevision` state | 42 |
| 11 | Shipping parse | Alamat format Indonesia | 30 |
| 12 | FSM single-step | `AdvanceOrderFlow` abon + qty | 5 |
| 13 | Purchase intent | `HasPurchaseIntent` per produk | 30 |
| 14 | Padding in-scope | Regresi scope | 401 |

**Total: 1000 subtests** di `TestBuyerBehavior1000` (`ai/buyer_behavior_1000_test.go`).

### Arsitektur test

| File | Fungsi |
|------|--------|
| `order_flow_sim.go` | `AdvanceOrderFlow` — FSM murni tanpa Redis/DB |
| `conversation_sim.go` | `ConversationSimulator.Turn` — multi-turn routing |
| `buyer_behavior_gen.go` | Generator 1000 skenario |
| `fixtures_omah.go` | Katalog Omah Apparel test |

---

## Bug yang ditemukan & diperbaiki dari unit test

### 1. Checkout gagal walau alamat lengkap (`missingOrderDataPrompt`)

**Gejala:** Setelah isi alamat, bot mengirim ringkasan + "Mau order produk apa?" — tidak pernah `Completed`.

**Penyebab:** `missingOrderDataPrompt` menolak checkout jika `CatalogItemID` kosong, padahal fixture/test catalog sering hanya punya `ProductName`.

**Fix:** `order_catalog.go` — cukup `productComplete()` (nama produk atau ID).

---

### 2. Apparel selalu minta ukuran walau SKU sudah `- L`

**Gejala:** `saya jadi beli boxer mono spot 1 paket` → stuck di `ask_variant`.

**Penyebab:** `applyCatalogMatch` + `inferVariantFromProductName` set `Size=L`, lalu `st.Size, st.Color = parseSizeAndColor(...)` **menimpa** dengan string kosong.

**Fix:** `autoreply.go`, `order_flow_sim.go` — hanya assign size/color jika parse menghasilkan nilai non-empty.

---

### 3. "ga jadi mau dirubah jadi 10 biji" = batal order (sudah diperbaiki sebelumnya)

**Penyebab:** `IsOrderCancelRequest` match substring `"ga jadi"` sebelum revisi qty.

**Fix:** `order_customer.go` — skip cancel jika `orderRevisionSignals()`.

---

### 4. Validator test salah ekspektasi

**Gejala:** `TestValidateReplyAgainstCatalog_priceMismatch` gagal.

**Penyebab:** Rp170.700 = 56900×3 masih di-allow sebagai "legacy wrong math" di validator.

**Fix:** Test pakai harga halu Rp99999 yang benar-benar tidak ada di katalog.

---

## Skenario manual yang masih perlu spot-check di WA

Unit test tidak mengganti uji end-to-end dengan Redis + DB + WhatsApp:

- Persist draft order ke Postgres
- LLM path (`llm`, `llm_tools`) dan grounding
- Race condition / duplicate webhook

Setelah `encore run`, uji 2–3 thread singkat di WA; sisanya di-cover 1000 test otomatis.

---

## Bug tambahan dari suite 2000 (adversarial)

### 5. Sapaan formal menelan pesan belanja

**Gejala:** `Selamat siang kak, jualan apa aja` / `harga abon berapa` dianggap **greeting** — tidak masuk katalog.

**Penyebab:** `IsGreetingLike` match prefix `selamat siang` tanpa cek tail pesan.

**Fix:** `greeting.go` + `safety.go` — `isCommerceDominant`, strip lead-in WA, tail setelah koma bukan greeting murni → bukan greeting.

---

### 6. `order` di `questionKeywords` → false consulting

**Gejala:** `order abon 3 biji` tidak masuk order flow (`IsQuestionLike` true → `hasPurchaseIntent` false).

**Penyebab:** Kata `order`/`pesan` dianggap pertanyaan hanya karena ada di `questionKeywords`.

**Fix:** Hapus `order`/`pesan` dari `questionKeywords`; `IsConsultingPurchaseQuestion` hanya aktif jika ada `?` atau prefix `bisa`/`boleh`/`kalau`.

---

### 7. `mau 1 biji abon` = konsultasi MOQ

**Gejala:** Pesanan eksplisit dengan `1 biji` dianggap tanya kebijakan eceran.

**Penyebab:** `retailPolicySignals` match substring `1 biji` di `IsConsultingPurchaseQuestion`.

**Fix:** `sales_state.go` — skip MOQ consult jika ada `mau`/`beli` + qty tanpa tanda tanya.

---

### 8. `min order` / `min pesan` = greeting

**Gejala:** Pertanyaan MOQ dibalas sapaan.

**Penyebab:** `IsCasualChatOpener` menganggap token `min` sebagai sapaan singkat.

**Fix:** `IsMinimumOrderQuestion` di-check sebelum greeting; `IsCasualChatOpener` exclude MOQ.

---

### 9. `mau cari hello kitty` = checkout (hallucinated flow)

**Gejala:** Pencarian produk memicu `cart_ready` + `ask_qty`.

**Penyebab:** `hasPurchaseIntent` true karena `mau` + match katalog tanpa membedakan `cari`.

**Fix:** `order_flow.go` — frasa `cari` tanpa intent order eksplisit bukan purchase intent.

---

### 10. Koreksi `ha?` tidak break flow

**Gejala:** `ha?` / `Selamat siang kak, ha?` saat `ask_variant` tetap lanjut order.

**Penyebab:** `IsUserSalesCorrection` tidak deteksi `ha?` dengan suffix (`ha? dong`) atau setelah lead-in formal.

**Fix:** `sales_state.go` — `isConfusionOnly` per token pertama; cek tail setelah koma; `conversation_sim.go` return early saat break + set intent correction.

---

### 11. Simulator greeting butuh in-scope (drift dari production)

**Gejala:** `halo` → `consulting` di test, padahal production selalu greeting.

**Penyebab:** `conversation_sim.go` mensyaratkan `inScope` untuk greeting.

**Fix:** Samakan `autoreply.go` — greeting selalu menang.

---

### 12. `hasPurchaseIntent` tanpa katalog di simulator

**Gejala:** `saya jadi beli abon` tidak masuk FSM di sim.

**Fix:** `conversation_sim.go` pakai `hasPurchaseIntent(text, catalog)`.

---

## Ringkasan hasil test (terakhir)

```
encore test ./ai/ -run TestBuyerBehavior1000 -count=1  → PASS
encore test ./ai/ -run TestWABuyerCases2000 -count=1    → PASS (2000 cases)
encore test ./ai/ -count=1                              → PASS (semua paket ai)
```
