# Unit economics & rekomendasi harga — WABantu (pemilik platform)

Dokumen ini menjawab: **biaya apa yang WABantu tanggung**, **dari mana angkanya**, dan **berapa harga jual + kuota yang masih masuk akal** (ada margin).  
Sumber kuota produk: `billing/billing.go`, `usage/usage.go`. Harga AI: `usage/costmonitor.go` (selaras [Anthropic Pricing](https://platform.claude.com/docs/en/about-claude/pricing)).

**Skenario operasional (CSW, handoff, follow-up, dashboard kuota):** [META_WHATSAPP_MESSAGING_AND_BILLING.md](./META_WHATSAPP_MESSAGING_AND_BILLING.md).

**Kurs asumsi perhitungan:** USD 1 = **Rp 16.500** (sesuaikan bulanan).

---

## 1. Meta WhatsApp Cloud API — klarifikasi “1.000 gratis”

### Model terbaru (sejak 1 Juli 2025)

Meta memakai **harga per pesan template terkirim**, bukan lagi “per conversation” untuk semua kategori. Ringkasnya:

| Jenis pesan | Kapan dipakai di WABantu | Biaya ke Meta (Indonesia, perkiraan) |
|-------------|--------------------------|--------------------------------------|
| **Balasan teks biasa** (non-template) | AI auto-reply, staff balas manual, dalam **jendela layanan 24 jam** setelah customer chat | **Rp 0** |
| **Utility template** | Notifikasi order, dll. — **di dalam** jendela layanan terbuka | **Rp 0** |
| **Marketing template** | **Broadcast** promosi ke banyak nomor | **~$0,0271 / pesan** ≈ **Rp 447** / pesan terkirim |
| **Authentication template** | OTP / verifikasi | **~$0,0079 / pesan** ≈ **Rp 130** / pesan |

Referensi tarif Indonesia (2026, agregator publik; cek CSV resmi Meta): marketing **$0,0271**, utility **$0,0036**, authentication **$0,0079** — [Meta Pricing](https://developers.facebook.com/docs/whatsapp/pricing/), [ringkasan per negara](https://setsmart.io/blog/whatsapp-business-api-pricing).

### Apa arti “1.000 gratis”?

Bukan “1.000 pesan apa pun”. Yang umum dijelaskan di pasar:

- **~1.000 percakapan layanan (service) gratis per bulan per WABA** — untuk pesan **layanan** dalam pola customer-initiated + balasan di jendela CS.
- **Marketing / utility di luar jendela** tetap kena tarif dari pesan pertama (tidak masuk kuota 1.000 itu).

**Implikasi WABantu:** mayoritas **AI inbox** (customer chat dulu → AI balas teks) = **biaya Meta ≈ 0**.  
Biaya Meta yang berbahaya bagi platform: **broadcast marketing** (template promosi).

---

## 2. Biaya Anthropic (token) — Haiku & Sonnet

Model di produk (`usage/costmonitor.go`, `ai/routing.go`):

| Model | Input / 1M token | Output / 1M token |
|-------|------------------|-------------------|
| **Claude Haiku 4.5** | $1,00 | $5,00 |
| **Claude Sonnet 4.6** | $3,00 | $15,00 |

### Rumus biaya satu panggilan LLM

```
biaya_USD = (input_tokens / 1_000_000 × harga_input) + (output_tokens / 1_000_000 × harga_output)
biaya_IDR = biaya_USD × 16_500
```

### Asumsi beban per “1 percakapan AI” (autoreply)

Dari pola produk (profil + FAQ + 1–2 putaran):

| Skema | Input | Output | Biaya / percakapan |
|-------|-------|--------|-------------------|
| **Haiku only** (Starter) | 1.800 | 450 | **≈ Rp 67** |
| **Hybrid** (Business, ~65% Haiku token) | 2.000 | 550 | **≈ Rp 95** |
| **Hybrid prioritas Sonnet** (Pro, ~55% token Sonnet) | 2.200 | 650 | **≈ Rp 145** |

*(Ini asumsi engineering; kalibrasi dari `usage_event` + `ai_activity` setelah 1–2 bulan produksi.)*

### Biaya maksimum AI jika kuota token **habis penuh** (100% pakai)

Kuota `ai_token` di sistem = **jumlah token input+output** yang dicatat per bulan.

| Paket | Kuota token/bulan | Asumsi mix | Perkiraan biaya Anthropic (penuh) |
|-------|-------------------|------------|----------------------------------|
| Trial | 100.000 | 70% Haiku | **≈ Rp 7.000** |
| Starter | 2.000.000 | 100% Haiku | **≈ Rp 67.000** |
| Business | 8.000.000 | 65% Haiku / 35% Sonnet | **≈ Rp 520.000** |
| Pro | 30.000.000 | 45% Haiku / 55% Sonnet | **≈ Rp 2.100.000** |

**Catatan:** Batas `ai_conversation` juga membatasi (mis. Pro 20.000 vs token 30 juta). Pada pemakaian normal, **token limit yang mengikat lebih dulu** jika rata-rata >1.500 token/percakapan.

---

## 3. Biaya Meta broadcast (marketing template) — skenario penuh

```
biaya_broadcast_IDR = jumlah_pesan_marketing_terkirim × Rp 447
```

| Paket | Kuota `broadcast_contact` | Biaya Meta jika kuota penuh (semua marketing ID) |
|-------|---------------------------|--------------------------------------------------|
| Trial | 20 | **≈ Rp 8.900** |
| Starter | 0 (fitur off) | Rp 0 |
| Business | 500 | **≈ Rp 223.500** |
| Pro | 10.000 | **≈ Rp 4.470.000** |

**Ini yang membuat paket Pro “10.000 kontak broadcast” berbahaya secara ekonomi** jika WABantu menyerap biaya Meta tanpa pass-through.

---

## 4. Biaya tetap platform (per tenant / bulan, perkiraan)

| Komponen | Perkiraan |
|----------|-----------|
| Postgres + Redis + compute (Encore) | Rp 15.000 – 40.000 / tenant aktif |
| Midtrans fee (~2,9% + fixed) | ~3% dari revenue |
| Support & margin operasional | sisanya |

Angka ini kecil dibanding **AI penuh** atau **broadcast penuh**.

---

## 5. Tabel “worst case” — semua kuota kepakai 100%

Harga jual hari ini (`billing/billing.go`):

| Paket | Harga jual/bulan | Meta (broadcast max) | Anthropic (token max) | Infra+payment ~ | **Total biaya variabel** | **Laba kotor variabel** |
|-------|------------------|----------------------|------------------------|-----------------|---------------------------|-------------------------|
| Starter | Rp 299.000 | Rp 0 | Rp 67.000 | Rp 25.000 | **Rp 92.000** | **+ Rp 207.000** (~69%) |
| Business | Rp 799.000 | Rp 223.500 | Rp 520.000 | Rp 35.000 | **Rp 778.500** | **+ Rp 20.500** (~3%) |
| Pro | Rp 1.999.000 | Rp 4.470.000 | Rp 2.100.000 | Rp 50.000 | **Rp 6.620.000** | **− Rp 4.621.000** |

**Kesimpulan keras:** dengan harga & kuota sekarang, **tenant Pro yang memakai broadcast + AI mendekati plafon = WABantu rugi besar**. Business di ambang impas pada skenario ekstrem.

---

## 6. Paket Pro harus punya batas **terdefinisi** (bukan “lebih besar”)

Definisi resmi di kode (sama Business kecuali angka & fitur tambahan):

| Dimensi | Business | **Pro** (angka pasti) |
|---------|----------|------------------------|
| Harga | Rp 799.000/bulan | **Rp 1.999.000/bulan** |
| Channel WA | 2 | **10** |
| Seat (staff) | 3 | **10** |
| Percakapan AI / bulan | 6.000 | **20.000** |
| Token AI / bulan | 8.000.000 | **30.000.000** |
| Kontak broadcast / bulan | **500** | **10.000** |
| Storage | 2 GB | **10 GB** |
| Eksekusi workflow / bulan | 500 | **5.000** |
| Routing AI | Hybrid Haiku/Sonnet | **Hybrid prioritas Sonnet** |
| Multi cabang | ✗ | **✓** |
| API access | ✗ | **✓** |

**Perbandingan Pro vs Business (untuk sales):**  
Pro = **3,3×** harga, **~3,3×** percakapan AI, **~3,75×** token AI, **20×** kuota broadcast, **5×** channel, **~10×** workflow — bukan “tanpa batas”.

---

## 7. Cara menghitung harga jual yang wajar (dengan margin)

### Langkah A — tentukan target margin

Contoh: **margin kotor variabel minimal 40%** setelah Meta + Anthropic (belum gaji tim).

```
harga_jual_min ≥ biaya_variabel / (1 - 0,40)
```

### Langkah B — hitung biaya variabel per paket (realistis, bukan worst case)

Asumsi pemakaian **70% kuota** (tenant rata-rata):

| Paket | AI (70% token) | Broadcast (30% kuota, marketing) | Total variabel |
|-------|----------------|----------------------------------|----------------|
| Starter | Rp 47.000 | 0 | **Rp 47.000** |
| Business | Rp 364.000 | 500×30%×447 = Rp 67.000 | **Rp 431.000** |
| Pro | Rp 1.470.000 | 10.000×20%×447 = Rp 894.000 | **Rp 2.364.000** |

Harga minimum (margin 40%):

| Paket | Biaya variabel (70%/30%) | Harga min (+40% margin) |
|-------|--------------------------|-------------------------|
| Starter | Rp 47.000 | **≥ Rp 78.000** |
| Business | Rp 431.000 | **≥ Rp 718.000** |
| Pro | Rp 2.364.000 | **≥ Rp 3.940.000** |

Harga **sekarang** (299k / 799k / 1.999k): Starter aman; Business pas di bawah target margin pada tenant aktif; **Pro terlalu murah** jika broadcast dipakai agresif.

### Langkah C — kebijakan broadcast (wajib untuk profit)

Pilih salah satu (bisa kombinasi):

1. **Pass-through Meta** — broadcast di atas kuota kecil = tagihan per pesan (Rp 500–600/ pesan marketing ke ID, termasuk margin 15–20%).
2. **Turunkan kuota included Pro** — mis. **1.000** marketing/bulan included, sisanya pay-per-use (bukan 10.000 included).
3. **Paksa utility / CSW** — edukasi tenant: broadcast promosi = kena Meta; follow-up di inbox = gratis.

Tanpa salah satu di atas, kuota **10.000 broadcast** di Pro adalah **liabilitas**, bukan fitur.

---

## 8. Rekomendasi harga jual (contoh, bisa diadopsi)

Asumsi: tenant rata-rata pakai 50–70% kuota; broadcast Pro dibatasi **1.000 included** + overage.

| Paket | Harga disarankan | Kuota broadcast disarankan | Catatan |
|-------|------------------|----------------------------|---------|
| Starter | **Rp 299.000 – 349.000** | 0 | OK hari ini |
| Business | **Rp 899.000 – 999.000** | **500** (pertahankan) | Naik sedikit untuk buffer AI |
| Pro | **Rp 2.499.000 – 2.999.000** | **1.000 included** + overage Rp 550/pesan | Turunkan liabilitas Meta |

**Overage contoh:** +Rp 75.000 per 500.000 token AI tambahan; +Rp 550 per 1.000 pesan marketing broadcast.

**Top-up kecil (implementasi billing):** untuk tenant UMKM yang belum siap upgrade paket, WABantu boleh menjual top-up AI kecil dengan margin ketat:

| Top-up | Harga | Token tambahan | Percakapan AI tambahan | Dasar kalkulasi |
|--------|-------|----------------|-------------------------|-----------------|
| AI Top-up 20rb | Rp 20.000 | 133.000 token | 59 percakapan | Prorata Rp75k/500k token, percakapan dibulatkan turun dari ~2.250 token/chat |
| AI Top-up 30rb | Rp 30.000 | 200.000 token | 88 percakapan | Prorata Rp75k/500k token, percakapan dibulatkan turun dari ~2.250 token/chat |

Aturan produk: top-up berlaku **hanya bulan berjalan**, tidak recurring, dan tidak membuka broadcast/storage/seat tambahan. Jika `ai_token` atau `ai_conversation` habis lagi, tenant harus top-up ulang atau upgrade paket.

---

## 9. Contoh satu tenant Business (bulan normal)

**Asumsi:** 4.000 percakapan AI, 5 juta token tercatat, 150 broadcast marketing.

1. **AI** — 5M token, mix hybrid → ≈ 5 × Rp 95 = **Rp 475.000** (pakai angka per juta token ≈ Rp 95/M blended → 5M ≈ Rp 475k)  
   Lebih tepat: 5.000.000 / 2.500 × Rp 95 = **Rp 190.000** jika 2000 percakapan efektif…  
   Recalc: 5M tokens at $3.96/M equivalent from earlier = 5 * 3.96 * 16500/1000 = Rp 326.700 ≈ **Rp 327.000**

2. **Broadcast** — 150 × Rp 447 = **Rp 67.050**

3. **Total variabel** ≈ **Rp 394.000**

4. **Harga jual** Rp 799.000 → **margin kotor ≈ Rp 405.000 (~51%)** — sehat.

---

## 10. Checklist monitoring (pemilik WABantu)

- [ ] Dashboard biaya: agregat `ai_token` × model dari `ai_activity` (bukan hanya kuota).
- [ ] Tag `broadcast` dengan kategori template Meta (marketing vs utility) untuk alokasi biaya.
- [ ] Alert tenant jika proyeksi biaya > 80% harga paket.
- [ ] Tampilkan di Billing: **semua angka kuota Pro** (bukan “lebih besar”).
- [ ] Halaman `/pricing` selaras dengan `billing.PlanCatalog` (bukan tier fiktif Growth).

---

## 11. Changelog kebijakan

| Tanggal | Catatan |
|---------|---------|
| 2026-05 | Dokumen awal — kalkulasi Meta per-message ID + Anthropic Haiku/Sonnet + skenario worst-case |

*Update angka di `billing.go` / `usage.go` → update tabel bagian 5–6 dan hitung ulang bagian 7.*
