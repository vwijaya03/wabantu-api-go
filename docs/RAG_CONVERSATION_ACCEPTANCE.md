# RAG Conversation Acceptance — Checkout Continuity (All Tenants)

Skrip penerimaan (acceptance) untuk percakapan smooth catalog-first + checkout multi-item. Berlaku **semua tenant** (`t_<slug>`); fixture archetype di `internal/buyerflow/`:

| Fixture | File | Archetype |
|---------|------|-----------|
| Omah (mixed) | `fixtures_omah.go` | apparel + food |
| Apparel-only | `fixtures_apparel.go` | fashion |
| Food-only | `fixtures_food.go` | FMCG |

**Kode test:** `internal/buyerflow/regression_cases.go` · `checkout_continuity_test.go` · **Simulator:** `internal/buyerflow/simulator.go`

---

## Cara menjalankan

```bash
cd api-go

# Gate CI (buyerflow + retrieval + apiregistry)
./scripts/run-ai-regression-tests.sh

# Golden regression + checkout continuity matrix
go test ./internal/buyerflow/ -run 'TestRegression|CheckoutContinuity|TestRegressionShippingScript|TestRegressionOrderRevisionScript' -v -count=1

# Encore smoke (master push saja)
encore test ./ai/ -run 'TestConversationRegression' -count=1
```

---

## Kriteria penerimaan

| # | Pesan user | Path metadata | Konten yang diharapkan |
|---|------------|---------------|------------------------|
| 1 | `sore bang` | `greeting` | Sapaan ramah |
| 2 | `toko ini jual apa aja?` | `catalog_db` | 3 keluarga: Pria Dewasa, Anak Perempuan, Makanan |
| 3 | `selain abon sapi ada apa aja?` | `catalog_db` | Produk non-abon (boxer, dll.) |
| 4 | `boxer mono spot ukuran L berapa?` | `catalog_db` | Harga SKU **L**, bukan M |
| 5 | `boxer mono spot ada ukuran apa aja?` | `catalog_db` | L dan M terdaftar terpisah |
| 6 | `berapa lama pengiriman?` | `shipping_faq` | Estimasi dari KB (mis. 2–3 hari) |
| 7 | `bisa kirim ke luar kota?` | `shipping_faq` | Ya + area kirim |
| 8 | `berapa ongkir ke surabaya?` | `shipping_faq` | Template minta alamat lengkap |
| 9 | `mau beli abon sapi 2 pcs` | `order_flow` | FSM order |
| 10 | `revisi jadi 5 pcs` | `order_flow` | Update qty (setelah setup order) |
| 11 | `WB-XXXX` (ref valid) | `order_status` | Status pesanan |
| 12 | `resep nasi goreng enak gimana?` | **bukan** `faq_direct` | Off-topic → consulting/LLM, bukan FAQ sembarangan |
| 13 | Pelanggan B tanya sama seperti A | **tidak** mengandung PII A | Regresi cache FAQ (lihat `AI_SECURITY_PRIVACY.md`) |
| 14 | Checkout Maggi + `1 pcs, lalu abon sapi 250g 1pcs` | `order_flow` | Ringkasan 2 item (Maggi + Abon) |
| 15 | Setelah checkout: `jadikan 1 dengan pesanan sebelumnya` / `abon nutela ga masuk` | `order_flow` (amend) | Draft diperbarui, bukan generic not-found katalog |
| 16 | `selain abon sapi ada list lainnya?` | `catalog_db` | Daftar produk tanpa Abon |
| 17 | Abon @ `ask_recipient` + `cadbury mini 1 pcs` (tanpa `lalu`) | `order_flow` | 2 item di keranjang, flow tidak break |
| 18 | `pesanan saya ada 2 loh ya` (cart aktif) | `order_flow` | Recap keranjang, **bukan** `order_status` |
| 19 | `masih mau order item yang lain?` | `consulting` | Policy cara tambah item, **bukan** full `catalog_db` |
| 20 | `durian musang king 1` (food tenant) | `order_flow` | Langsung `ask_recipient`, tanpa `ask_variant` ukuran |

---

## Canary: Omah Apparel (thread `b72e2bee-…`)

Replay manual di staging setelah merge PR checkout continuity:

1. `mau coba maggi percik 1` → order flow Maggi
2. `1 pcs, lalu abon sapi yang 250 gram 1pcs` → kedua item di ringkasan
3. `cadbury mini 1 pcs` @ ask_recipient → item ketiga tanpa clear cart
4. `loh abon nutela ga masuk` → amend/recap draft, bukan LLM Abon 125g salah
5. `pesanan saya ada 2 loh ya` → recap keranjang, bukan "belum ada pesanan"
6. `masih mau order item yang lain?` → policy reply, bukan full katalog
7. `selain abon sapi ada list lainnya?` → `catalog_db` dengan filter exclusion

## Multi-turn scripts

### Shipping (`TestRegressionShippingScript`)

1. `mau tanya produk dulu` → `catalog_db`
2. `berapa ongkir ke bandung?` → `shipping_faq`
3. `berapa lama pengiriman?` → `shipping_faq` (KB ETA)

### Order revision (`TestRegressionOrderRevisionScript`)

1. `mau beli abon sapi 2 pcs` → `order_flow`
2. `revisi jadi 5 pcs` → `order_flow`

### Checkout continuity matrix (`TestRegressionCheckoutContinuityMatrix`)

Archetype mixed, apparel-only, food-only — lihat `checkout_continuity_test.go`.

---

## Menambah skenario baru

1. Tambah entry di `internal/buyerflow/regression_cases.go` atau `checkout_continuity_test.go`
2. Jalankan `go test ./internal/buyerflow/ -run TestRegression -v`
3. PR wajib hijau: workflow **AI Regression** (`regression-fast`)

---

## Lihat juga

- [WHATSAPP_AI_ROUTING.md](./WHATSAPP_AI_ROUTING.md) — urutan routing autoreply
- [RAG_VECTOR_RETRIEVAL.md](./RAG_VECTOR_RETRIEVAL.md) — vector mode, threshold, fallback
- [AI_SECURITY_PRIVACY.md](./AI_SECURITY_PRIVACY.md) — isolasi tenant, PII, quota
