# RAG Conversation Acceptance — Omah Apparel Fixture

Skrip penerimaan (acceptance) untuk percakapan smooth catalog-first + RAG. Fixture: `internal/buyerflow/fixtures_omah.go` (`omahCatalog()` — 6 SKU, 3 keluarga produk).

**Kode test:** `internal/buyerflow/regression_cases.go` · **Simulator:** `internal/buyerflow/simulator.go`

---

## Cara menjalankan

```bash
cd api-go

# Gate CI (buyerflow + retrieval + apiregistry)
./scripts/run-ai-regression-tests.sh

# Hanya golden regression
go test ./internal/buyerflow/ -run 'TestRegression|TestRegressionShippingScript|TestRegressionOrderRevisionScript' -v -count=1

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

---

## Multi-turn scripts

### Shipping (`TestRegressionShippingScript`)

1. `mau tanya produk dulu` → `catalog_db`
2. `berapa ongkir ke bandung?` → `shipping_faq`
3. `berapa lama pengiriman?` → `shipping_faq` (KB ETA)

### Order revision (`TestRegressionOrderRevisionScript`)

1. `mau beli abon sapi 2 pcs` → `order_flow`
2. `revisi jadi 5 pcs` → `order_flow`

---

## Menambah skenario baru

1. Tambah entry di `internal/buyerflow/regression_cases.go`
2. Jalankan `go test ./internal/buyerflow/ -run TestRegression -v`
3. PR wajib hijau: workflow **AI Regression** (`regression-fast`)

---

## Lihat juga

- [WHATSAPP_AI_ROUTING.md](./WHATSAPP_AI_ROUTING.md) — urutan routing autoreply
- [RAG_VECTOR_RETRIEVAL.md](./RAG_VECTOR_RETRIEVAL.md) — vector mode, threshold, fallback
- [AI_SECURITY_PRIVACY.md](./AI_SECURITY_PRIVACY.md) — isolasi tenant, PII, quota
