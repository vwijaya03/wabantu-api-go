# Spec: Public Patient Schedule + Safe Public Event Errors

**Tanggal:** 2026-08-01  
**Status:** Draft — menunggu review user  
**Scope:** api-go + web-frontend (PR terpisah)  
**Terkait:** monitor staf publik, tab Jadwal dashboard events

---

## 1. Masalah

1. Halaman publik `/monitor/{tenant}/{event}` kadang menampilkan error teknis Postgres (mis. `relation "evt_event" does not exist`) setelah Safari mobile idle — **tidak boleh** terlihat oleh end user.
2. Butuh halaman publik baru untuk **jadwal pasien** (subset kolom tab Jadwal dashboard), hanya saat acara `PUBLISHED`.
3. Metadata halaman jadwal pasien = **nama acara saja**, tanpa branding WABantu.

**Akar penyebab (monitor):**
- Public handlers memakai `appErrs.Internal(err.Error())` → teks DB bocor ke HTTP `message`.
- UI monitor menampilkan `toApiError(error).message` mentah (halaman register sudah menyembunyikannya).
- Lazy migrate / bad connection setelah idle bisa memicu missing relation; dashboard punya retry, public monitor belum.

---

## 2. Keputusan produk

| Item | Keputusan |
|------|-----------|
| URL halaman | `/jadwal/{tenantSlug}/{eventSlug}` |
| URL API | `GET /api/v1/public/events/{tenantSlug}/patient-schedule/{eventSlug}` |
| Visibility | Hanya `status = PUBLISHED` |
| Filter pasien | Hanya `CONFIRMED` + punya slot |
| Kolom UI | Pasien, Terapi, Jadwal, Jam preferensi |
| Error ke client | Pesan aman + HTTP status + `errorCode` stabil; detail DB hanya di server log |
| Metadata title | Absolut = `eventName` saja (tanpa “WABantu”, tanpa “\| Tenant”) |
| Dashboard | Tombol “Salin link jadwal pasien” saat `PUBLISHED` |

---

## 3. Kontrak API (developer)

### 3.1 Endpoint baru — public patient schedule

```
GET /api/v1/public/events/:tenantSlug/patient-schedule/:eventSlug
Auth: none (public)
```

**Sukses `200`:**

```json
{
  "eventName": "Terapi Energi 2 Agustus 2026",
  "patients": [
    {
      "fullName": "Lilik Supatmi",
      "therapyName": "Terapi 5 Elemen",
      "slotLabel": "2 Agustus 2026 09:00–09:30",
      "preferredTime": "09:00"
    }
  ]
}
```

| Field | Tipe | Catatan |
|-------|------|---------|
| `eventName` | string | Nama acara (juga sumber metadata title) |
| `patients` | array | Kosong `[]` jika belum ada yang CONFIRMED+slot |
| `patients[].fullName` | string | Decrypted/display name |
| `patients[].therapyName` | string | |
| `patients[].slotLabel` | string | Format sama dashboard (`ListTimeSlots` / schedule) |
| `patients[].preferredTime` | string | Boleh `""` |

**Tidak dikembalikan (privacy):** birth date, complaint, reservation status, patient/slot/therapy UUID, tenant internals.

**Urutan:** selaras tab Jadwal dashboard (slot time, lalu nama).

### 3.2 Endpoint existing — public staff monitor (perilaku error diubah)

```
GET /api/v1/public/events/:tenantSlug/monitor/:eventSlug
```

Response shape **tidak berubah**. Yang berubah: sanitasi error (lihat §4). Berlaku juga untuk endpoint public events lain yang masih `Internal(err.Error())` dalam scope PR (minimal: monitor + patient-schedule; ideal: semua `public/events/*`).

### 3.3 Error contract (public events)

Jangan pernah mengembalikan string Postgres/SQL ke client.

| Situasi | HTTP | `message` (user-facing, ID) | `errorCode` |
|---------|------|-----------------------------|-------------|
| Tenant/event tidak ada, atau bukan `PUBLISHED` | 404 | `Acara tidak ditemukan` | `EVT_NOT_FOUND` |
| Transient (bad connection, missing `evt_*` relation saat migrate) | 503 | `Jadwal sementara tidak tersedia. Coba muat ulang sebentar lagi.` | `EVT_PUBLIC_UNAVAILABLE` |
| Error tak terduga lain | 500 | `Terjadi gangguan. Coba lagi nanti.` | `EVT_PUBLIC_INTERNAL` |

**Logging (wajib untuk debugging):**

```
rlog.Error("public event failed",
  "errorCode", "...",
  "tenantSlug", "...",
  "eventSlug", "...",
  "err", err,   // full error — server only
)
```

Developer mencari log dengan `errorCode` + `tenantSlug` + `eventSlug`. Tidak perlu menampilkan `requestId` di UI untuk v1.

**Retry:** satu kali ulang query pada error “bad connection” / missing relation yang transient (pola mirip staff roster dashboard), lalu baru return 503.

**Helper yang disarankan (api-go):**  
`publicEventError(ctx, errorCode, userMessage, httpKind, err, tenantSlug, eventSlug)` di package `events` agar monitor & patient-schedule konsisten.

### 3.4 Implementasi Encore / errs

- Pakai `shared/errs`: `NotFound(...)`, `Unavailable(...)` (HTTP 503), `Internal(...)` — **semua message user-safe**, tidak pernah `err.Error()`.
- `errorCode` **wajib** di structured log (`rlog` fields).
- Response body: Encore `errs.Error` sudah punya `Code` + `Message`. Untuk debugging client opsional, set `Details` map jika pola project mengizinkan, mis. `Details: map[string]string{"errorCode": "EVT_PUBLIC_UNAVAILABLE"}` — **bukan** raw SQL.
- Frontend public pages **mengabaikan** Details untuk tampilan user; cukup status + pesan aman hardcoded/fallback.
- Dokumentasikan di `EVENTS_MODULE.md`: cara filter log Cloud/local dengan `errorCode`.

---

## 4. Frontend

### 4.1 Halaman `/jadwal/[tenantSlug]/[eventSlug]`

- Client fetch `eventsApi.getPublicPatientSchedule(tenant, event)`.
- Tabel: Pasien | Terapi | Jadwal | Jam preferensi (UI clean, mirip list dashboard; tanpa kartu berlebih di hero).
- Loading / empty / error states ramah.
- Error UI: **jangan** render `toApiError(...).message` mentah — copy tetap seperti §3.3.
- `retry: false` atau retry terbatas; tombol “Muat ulang” opsional.

### 4.2 Metadata

`generateMetadata`:
- Sukses: `title: { absolute: eventName }` — **tanpa** suffix WABantu / tenant.
- Description: singkat berbasis nama acara / tanggal jika ada di response (atau generik “Jadwal pasien terjadwal”).
- Gagal fetch: `title: { absolute: "Jadwal Pasien" }` tanpa brand WABantu.

### 4.3 Fix monitor page

- Samakan error surface dengan register / jadwal: pesan aman, tidak leak DB.
- Opsional: label “Muat ulang” tetap.

### 4.4 Dashboard event detail

- Tombol **Salin link jadwal pasien** → `{SITE}/jadwal/{tenantSlug}/{eventSlug}` hanya jika `PUBLISHED` (sejajar “Salin link pantau”).

### 4.5 Verifikasi frontend

```bash
nvm use 25.9.0
npm run lint
npm run build
```

---

## 5. Dokumentasi yang wajib di-update (developer-clear)

Saat implementasi, **update** (bukan hanya spec ini):

### api-go/docs/EVENTS_MODULE.md

Tambah/ubah bagian **Publik**:

1. Tabel endpoint lengkap termasuk:
   - `GET .../monitor/:eventSlug` (staff monitor)
   - `GET .../patient-schedule/:eventSlug` (jadwal pasien publik) ← baru
   - register/staff yang sudah ada
2. Subsection **Public error contract** (§3.3) — tabel HTTP / message / errorCode + aturan “no raw DB”.
3. Subsection **Patient schedule response** — field list + filter CONFIRMED.
4. Troubleshooting: “User melihat error `evt_* does not exist`” → seharusnya tidak lagi; cek log `EVT_PUBLIC_UNAVAILABLE` + jalankan schema patch tenant.

### web-frontend/docs/EVENTS_MODULE.md

1. Tabel “Di mana fitur ini?” + baris **Jadwal pasien publik** → `/jadwal/{slug-toko}/{slug-acara}`.
2. Cara salin link dari dashboard.
3. Kolom yang tampil vs yang disembunyikan (privacy).
4. Catatan metadata title = nama acara saja.

### Spec ini

Tetap sebagai sumber keputusan desain di `api-go/docs/superpowers/specs/`.

---

## 6. Testing

**api-go (standard `testing`, no testify):**
- Helper error: missing relation / bad connection → mapped ke unavailable, message tidak mengandung `evt_` / `relation`.
- Patient schedule: non-PUBLISHED → NotFound; PUBLISHED + hanya CONFIRMED masuk list (unit/query helper bila feasible).

**Manual:**
- Safari: buka monitor, idle lama, kembali — tidak ada teks Postgres.
- Buka `/jadwal/...` untuk event published — 4 kolom benar.
- Draft event — 404 / “Acara tidak ditemukan”.
- View source / OG: title = nama acara saja.

**encore:** `encore test ./events/...` (permission `all` di agent).

---

## 7. PR plan

1. **api-go** `fix/public-event-safe-errors` atau `feat/public-patient-schedule` — sanitasi error + endpoint patient-schedule + docs api-go.
2. **web-frontend** `feat/public-patient-schedule` — halaman `/jadwal`, metadata, fix monitor error UI, tombol salin link, docs frontend.

Urutan merge: api-go dulu, lalu web-frontend.

---

## 8. Out of scope

- Virtualisasi tabel panjang
- Filter/sort interaktif di halaman publik
- SSE/realtime untuk jadwal pasien
- Menampilkan CANCELLED/COMPLETED
- Break-glass / auth pada halaman jadwal
