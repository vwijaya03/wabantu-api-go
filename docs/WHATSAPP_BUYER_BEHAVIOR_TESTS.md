# WhatsApp Buyer Behavior — Research & 1000 Unit Tests

Dokumen ini merangkum perilaku pembeli via WhatsApp yang di-handle bot Omah Apparel, suite test otomatis, dan bug yang ditemukan + diperbaiki dari test tersebut.

## Cara menjalankan

```bash
cd api-go
./scripts/fix-encore-test-db.sh   # jika encore-migrator error
./scripts/run-ai-unit-tests.sh
# atau subset:
encore test ./ai/ -run TestBuyerBehavior1000 -count=1
```

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

## Ringkasan hasil test (terakhir)

```
encore test ./ai/ -run TestBuyerBehavior -count=1  → PASS (1000 cases)
encore test ./ai/ -count=1                         → PASS (semua paket ai)
```
