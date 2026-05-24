# WhatsApp (Meta) — kapan bayar, kapan gratis, & beda dengan kuota WABantu

Panduan untuk **tim produk**, **CS/sales**, dan **client (owner)**.  
Kuota angka paket WABantu: [LIMITS_AND_QUOTAS.md](../LIMITS_AND_QUOTAS.md). Unit economics: [UNIT_ECONOMICS_AND_PRICING.md](./UNIT_ECONOMICS_AND_PRICING.md).

**Dashboard client:** `GET /api/v1/usage/summary` + halaman `/dashboard/billing` (panel kuota WABantu).

---

## Dua tagihan terpisah (wajib dipahami)

| Yang dibayar ke | Untuk apa | Contoh |
|-----------------|-----------|--------|
| **WABantu** (Midtrans) | Langganan software: inbox, AI, workflow, kuota platform | Rp 299k / 799k / 1,99 juta per bulan |
| **Meta** (kartu di Business Manager client) | Pesan **template** WhatsApp terkirim (marketing/utility/auth di luar aturan gratis) | ~Rp 59–447 per pesan ke Indonesia (2026) |

**Langganan WABantu tidak mengganti tagihan Meta.** Kuota `broadcast_contact` di WABantu = **batas platform**, bukan “Meta sudah dibayar”.

---

## Jendela layanan 24 jam (Customer Service Window / CSW)

- Dibuka saat **pelanggan mengirim pesan ke bisnis** (bukan saat bisnis mengirim dulu).
- Selama **24 jam sejak pesan terakhir pelanggan**, bisnis boleh kirim **pesan teks/gambar biasa** (sesi) lewat inbox WABantu (`SendText`).
- Pesan sesi dalam CSW → **Meta: gratis** (Indonesia, aturan umum 2025+).

Setelah 24 jam **tanpa** pesan baru dari pelanggan → CSW **tutup**.

---

## Skenario umum (FAQ)

### 1. Pelanggan A chat siang, staff handoff / balas manual (tanpa AI)

| | |
|--|--|
| Meta | **Tidak bayar** (balasan teks dalam CSW) |
| WABantu | Kuota `ai_conversation` / `ai_token` **tidak** terpakai untuk balasan manual |

### 2. Bisnis memulai chat dulu ke A (A belum pernah chat / CSW tutup)

| Cara kirim | Sampai ke A? | Meta |
|------------|--------------|------|
| Inbox teks biasa | **Biasanya tidak** (API menolak) | Rp 0 (tidak terkirim) |
| Template marketing terkirim | Ya | **~Rp 447/pesan** (ID) |
| Template utility terkirim (di luar CSW) | Ya | **~Rp 59/pesan** (ID) |

“Chat personal” di UI **≠** boleh gratis kalau CSW sudah tutup. Yang gratis = **sesi dalam CSW**, bukan nada ramahnya.

### 3. A pernah chat, lalu diam 1 minggu, bisnis follow-up dari inbox

Sama seperti skenario 2: teks inbox **gagal**; yang sampai = **template berbayar** ke Meta.

### 4. AI auto-reply setelah A chat

| | |
|--|--|
| Meta | **Tidak bayar** (teks dalam CSW) |
| WABantu | Kena kuota **`ai_token`** + **`ai_conversation`** |

### 5. Broadcast di menu WABantu

Implementasi saat ini: **`SendText` massal**, bukan template marketing resmi.

- Ke nomor **tanpa CSW terbuka** → banyak yang **gagal**; Meta sering **tidak tagih**.
- Kuota WABantu `broadcast_contact` tetap terhitung saat kampanye dibuat — ini **batas jumlah penerima di platform**, bukan invoice Meta.

Untuk promosi resmi yang sampai → butuh **template marketing** + billing Meta aktif di [Business Manager](https://business.facebook.com/).

---

## “1.000 pesan gratis” — apa maksudnya?

Bukan “1.000 pesan apa pun dari paket WABantu”.

| Pesan | Gratis di Meta? |
|-------|-----------------|
| Balasan layanan (teks dalam CSW setelah customer chat) | **Ya** (service; Meta menyatakan service conversations gratis sejak Nov 2024) |
| Template **marketing** | **Tidak** — kena dari pesan pertama terkirim |
| Template utility **di luar** CSW | **Tidak** — per pesan (+ tier volume) |

Model penagihan template: **per pesan terkirim** sejak 1 Juli 2025 ([Meta Pricing](https://developers.facebook.com/docs/whatsapp/pricing/)).

### Rumus template (Indonesia, perkiraan)

```
biaya_meta ≈ jumlah_template_terkirim × tarif[kategori][negara]
```

| Kategori | IDR / pesan (≈) |
|----------|-----------------|
| Marketing | Rp 447 |
| Utility | Rp 59 |
| Authentication | Rp 130 |

Tarif resmi: rate card CSV/PDF di portal Meta (bisa berubah tiap kuartal).

---

## Messaging limit Meta (bukan kuota WABantu)

Batas **berapa nomor unik** yang bisa dihubungi **di luar CSW** per periode rolling (~24 jam), level **business portfolio**:

- Awal sering **~250** unique users → bisa naik 2.000 → 10.000 → … dengan verifikasi & kualitas pengiriman.
- Terpisah dari kuota `broadcast_contact` di paket Pro (10.000/bulan).

Cek di WhatsApp Manager / field API `whatsapp_business_manager_messaging_limit`.

---

## Siapa pasang kartu kredit Meta?

| Model | Praktik |
|-------|---------|
| **Disarankan (SaaS)** | Client login OAuth → **kartu client** di Meta Business Manager mereka |
| **BSP / pass-through** | Satu credit line WABantu → tagih client (butuh partnership Meta) |
| **Jangan** | Kartu pribadi WABantu di setiap akun FB client |

---

## Kuota WABantu yang tampil di dashboard

| `event_type` | Arti untuk client |
|--------------|-------------------|
| `ai_conversation` | Percakapan yang memakai AI (bulan ini) |
| `ai_token` | Token AI terpakai (import gambar, autoreply, dll.) |
| `broadcast_contact` | Penerima yang dicatat di kampanye broadcast platform |
| `workflow_exec` | Rule kata kunci terpicu |
| `admin_seat` | Kursi staff |
| `storage_byte` | Penyimpanan file |

Lihat pemakaian: **`GET /api/v1/usage/summary`** — reset periode kalender `YYYY-MM`.

---

## Copy untuk sales / onboarding (singkat)

> WhatsApp mengizinkan chat gratis dari inbox **hanya saat pelanggan baru menghubungi Anda** (dalam 24 jam). Follow-up setelah lama diam atau promosi ke banyak nomor memakai **pesan template resmi Meta** yang **ditagih terpisah** ke akun bisnis Anda — bukan termasuk langganan WABantu.

---

## Changelog

| Tanggal | Catatan |
|---------|---------|
| 2026-05 | Dokumen awal — CSW, skenario inbox, beda Meta vs WABantu |
