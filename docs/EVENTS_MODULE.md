# Modul Acara & Terapi (Events) — Dokumentasi Teknis (api-go)

Modul **Event Reservation & Therapy** untuk tenant WABantu: manajemen acara, staf, pasien, penugasan, slot waktu, pendaftaran publik, dan integrasi **Contacts** + **roster staf**.

> Panduan penggunaan (owner/operator): [`web-frontend/docs/EVENTS_MODULE.md`](../../web-frontend/docs/EVENTS_MODULE.md).

---

## Ringkasan fitur

| Area | Keterangan |
|------|------------|
| Acara | CRUD, status (`DRAFT` → `PUBLISHED` → …), duplikat (staf + pasien + pengaturan terapi, tanpa slot) |
| Staf acara | Terapis, Shijie, Daoshi, Fashi, relawan (+ pencatat), multi-terapi per orang |
| Roster staf | Daftar tim tetap tenant; import otomatis ke acara baru |
| Pasien | CRUD admin, filter, **export PDF (async job)**, pendaftaran publik, **opsional dari Contact** |
| Export | Job antrian (`evt_export_job`): PDF pasien & lembar Excel staf (format Google Sheet) |
| Pengaturan terapi | Mode jadwal `AUTO` (rentang jam) atau `MANUAL` (daftar slot per hari) |
| Slot | Generate per terapi per hari acara; kapasitas dari jumlah terapis/shijie/FIXED |
| Penugasan | Tugas × staf × jam/sesi |
| Import gambar | AI mengekstrak draft staf/pasien dari screenshot (Redis staging) |

---

## Package & file utama

| File | Isi |
|------|-----|
| `events/event.go` | CRUD acara, therapy settings, `importStaffFromRoster` saat create |
| `events/staff.go` | CRUD orang acara, penugasan |
| `events/staff_person.go` | Mapping role ↔ `person_type`, sync terapi & relawan |
| `events/staff_roster.go` | Roster tenant, import ke acara, sync dari acara |
| `events/patients.go` | CRUD pasien, status reservasi |
| `events/patient_contact.go` | Resolve pasien dari `contact_id` |
| `events/patients_query.go` | List/filter pasien |
| `events/patients_export.go` | Builder PDF pasien |
| `events/export_job.go` | Job export async (pasien PDF, staf XLSX) |
| `events/staff_export_sheet.go` | Builder lembar Excel staf + penugasan |
| `events/slots.go` | Generate & list slot waktu |
| `events/therapy_schedule.go` | Template slot manual, validasi |
| `events/dashboard.go` | Ringkasan acara |
| `events/event_duplicate.go` | Duplikat acara |
| `events/public.go` | Pendaftaran publik (`/register/{tenant}/{event}`) |
| `events/image_import.go` | Import gambar staf/pasien |
| `events/master.go` | Master terapi, peran relawan, tugas |
| `events/helpers.go` | Schema cache, util tenant |
| `tenant/events_schema.go` | DDL `evt_*` + patch idempotent |

Frontend: `web-frontend/lib/api/events.ts`, komponen di `web-frontend/components/events/`.

---

## Schema database (tenant `t_<slug>`)

### Tabel inti acara

| Tabel | Keterangan |
|-------|------------|
| `evt_event` | Acara (nama, slug, tanggal, jam, status) |
| `evt_event_therapy` | Pengaturan per terapi: durasi, kapasitas, `schedule_mode`, jam (AUTO) |
| `evt_event_therapy_slot_template` | Baris jam mulai/selesai per template (mode `MANUAL`) |
| `evt_event_person` | Staf/orang di acara |
| `evt_person_therapy` | Terapi yang dikuasai staf (+ jam partial) |
| `evt_event_volunteer` | Relawan: peran + flag pencatat |
| `evt_event_assignment` | Penugasan tugas |
| `evt_time_slot` | Slot terjadwal per tanggal |
| `evt_patient` | Pasien (nama/TTL terenkripsi + `contact_id` opsional) |
| `evt_audit_log` | Audit trail |
| `evt_export_job` | Antrian export (status, `download_url` data URL, filter di `params`) |

### Roster staf (tenant-wide)

| Tabel | Keterangan |
|-------|------------|
| `evt_staff_roster` | Nama, `person_type`, catatan |
| `evt_staff_roster_therapy` | Terapi per anggota roster |
| `evt_staff_roster_volunteer` | Peran relawan + `is_pencatat` |

Unique aktif: `(normalized_name, person_type)` where `deleted_at IS NULL`.

### Integrasi Contacts

| Kolom / tabel | Keterangan |
|---------------|------------|
| `contact.birth_date` | Tanggal lahir (opsional) — dipakai saat pasien dari kontak |
| `evt_patient.contact_id` | FK opsional ke `contact.id` |

Patch DDL dijalankan via `tenant.RunEventsSchemaPatches` (setiap koneksi events pertama kali per schema, dengan cache).

---

## Mode jadwal terapi (`schedule_mode`)

| Mode | Perilaku generate slot |
|------|------------------------|
| `AUTO` | Bagi rentang `schedule_start_time`–`schedule_end_time` (atau jam acara) dengan `slot_duration_minutes` |
| `MANUAL` | Pakai baris `evt_event_therapy_slot_template` (diulang **setiap hari** dalam rentang tanggal acara) |

Contoh MANUAL (Terapi 5 Elemen): 09:00–09:30, 09:31–10:00, 10:01–10:30, 13:00–13:30, 13:31–14:00 (jeda istirahat tidak perlu diisi).

**Kapasitas slot** (`capacity_mode`):

- `THERAPIST_COUNT` — jumlah staf terapis (dll.) yang terhubung ke terapi tersebut
- `SHIJIE_COUNT` — jumlah Shijie hadir
- `FIXED` — `max_capacity` tetap

---

## Roster staf — alur API

1. **Upsert roster** — otomatis saat `POST/PUT .../people` jika `saveToRoster` tidak `false` (default: simpan).
2. **`GET /api/v1/events/staff-roster`** — daftar roster.
3. **`POST /api/v1/events/detail/:eventId/people/import-roster`** — salin semua roster ke acara (skip jika nama+peran sudah ada).
4. **`POST /api/v1/events/staff-roster/sync-from-event/:eventId`** — bangun/update roster dari semua staf acara tersebut.
5. **`POST /api/v1/events`** — body `importStaffFromRoster: true` (default jika tidak dikirim: import) memanggil import roster setelah create.

Tambah staf dari roster: `POST .../people` dengan `rosterId` — field lain bisa dikosongkan (diisi dari roster).

---

## Pasien dari kontak

**`POST /api/v1/events/detail/:eventId/patients`** body:

```json
{
  "contactId": "uuid-kontak",
  "therapyId": "uuid-terapi",
  "fullName": "",
  "birthDate": "",
  "complaint": "",
  "preferredTime": ""
}
```

- Jika `contactId` diisi: `fullName` / `birthDate` boleh kosong — diambil dari `contact.display_name` / `contact.birth_date`.
- Jika tanggal lahir kontak kosong: error `400` dengan pesan agar dilengkapi di Contacts.
- `complaint` kosong bisa diisi dari `contact.notes`.

Kontak: package `inbox` — kolom `birth_date` pada create/update contact.

---

## Endpoint ringkas

### Acara

| Method | Path | Role | Catatan |
|--------|------|------|---------|
| GET | `/api/v1/events` | auth | List + filter |
| GET | `/api/v1/events/detail/:eventId` | auth | Detail |
| POST | `/api/v1/events` | owner | `importStaffFromRoster` opsional |
| PUT | `/api/v1/events/detail/:eventId` | owner | |
| DELETE | `/api/v1/events/detail/:eventId` | owner | Soft delete |
| POST | `/api/v1/events/detail/:eventId/duplicate` | owner | Salin staf, pasien, pengaturan terapi |
| GET | `/api/v1/events/detail/:eventId/dashboard` | auth | Ringkasan |
| GET | `/api/v1/events/detail/:eventId/schedule` | auth | Slot + pasien terjadwal |

### Staf & roster

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/events/detail/:eventId/people` | auth | `q`, `personType`, `page`, `pageSize` |
| POST | `/api/v1/events/detail/:eventId/people` | owner | `rosterId`, `saveToRoster`, `role`, `therapyIds`, … |
| PUT | `/api/v1/events/detail/:eventId/people/:personId` | owner |
| DELETE | `/api/v1/events/detail/:eventId/people/:personId` | owner |
| GET | `/api/v1/events/staff-roster` | auth |
| POST | `/api/v1/events/detail/:eventId/people/import-roster` | owner |
| POST | `/api/v1/events/staff-roster/sync-from-event/:eventId` | owner |

**Role slug (body):** `terapis`, `relawan`, `shijie`, `daoshi`, `fashi` → disimpan sebagai `person_type` DB.

### Pasien

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/events/detail/:eventId/patients` | auth | Filter: `q`, `therapyId`, `status`, `slotDate`, `hasSlot`, pagination |
| POST | `/api/v1/events/detail/:eventId/patients` | owner | `contactId` opsional |
| PUT | `/api/v1/events/detail/:eventId/patients/:patientId` | owner |
| PATCH | `/api/v1/events/detail/:eventId/patients/:patientId` | owner | Status saja |
| DELETE | `/api/v1/events/detail/:eventId/patients/:patientId` | owner |
### Export (async, pola sama dengan Finance `fin_report_job`)

| Method | Path | Role | Catatan |
|--------|------|------|---------|
| POST | `/api/v1/events/detail/:eventId/export-jobs` | auth | Body: `kind`, `filters` (pasien), `format` |
| GET | `/api/v1/events/detail/:eventId/export-jobs` | auth | 20 job terakhir per acara |
| GET | `/api/v1/events/detail/:eventId/export-jobs/:jobId` | auth | Poll status + `downloadUrl` saat `done` |

**`kind`:**

| Nilai | Hasil |
|-------|--------|
| `patients_pdf` | PDF daftar pasien (filter sama dengan list pasien, maks. 2500 baris) |
| `staff_sheet` | XLSX format operasional: tabel terapis, relawan, rotasi Medang per jam, tugas per sesi |

Worker: `go processExportJobAsync` — progress di `error_msg` saat `status=processing`, hasil di `download_url` (`data:application/pdf` atau `data:application/vnd...sheet`).

### Pengaturan terapi & slot

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/events/detail/:eventId/therapy-settings` | auth |
| PUT | `/api/v1/events/detail/:eventId/therapy-settings` | owner | `scheduleMode`, `slotTemplates[]`, … |
| POST | `/api/v1/events/detail/:eventId/therapies/:therapyId/generate-slots` | owner |
| GET | `/api/v1/events/detail/:eventId/slots` | auth |

**Body upsert therapy settings (contoh MANUAL):**

```json
{
  "therapyId": "uuid",
  "slotDurationMinutes": 30,
  "capacityMode": "THERAPIST_COUNT",
  "scheduleMode": "MANUAL",
  "slotTemplates": [
    { "startTime": "09:00", "endTime": "09:30", "sortOrder": 0 },
    { "startTime": "09:31", "endTime": "10:00", "sortOrder": 1 }
  ]
}
```

### Penugasan

| Method | Path | Role |
|--------|------|------|
| GET | `/api/v1/events/detail/:eventId/assignments` | auth | `q`, pagination |
| POST | `/api/v1/events/detail/:eventId/assignments` | owner |
| PUT | `/api/v1/events/detail/:eventId/assignments/:id` | owner |
| DELETE | `/api/v1/events/detail/:eventId/assignments/:id` | owner |

### Master data

| Method | Path |
|--------|------|
| CRUD | `/api/v1/events/masters/therapies` |
| CRUD | `/api/v1/events/masters/volunteer-roles` |
| CRUD | `/api/v1/events/masters/tasks` |

Seed default terapi (jika kosong): *Terapi 5 Elemen*, *Terapi Shijie*, *Terapi Energi Dewa*.

### Publik

| Method | Path |
|--------|------|
| GET | `/api/v1/public/events/:tenantSlug/register/:eventSlug` |
| POST | `/api/v1/public/events/:tenantSlug/register/:eventSlug` |

Acara harus `PUBLISHED` dan dalam jendela registrasi.

### Import gambar (AI)

| Method | Path |
|--------|------|
| POST | `.../people/import-image/preview` (multipart) |
| GET | `.../people/import-image/draft/:jobId` |
| POST | `.../people/import-image/draft/:jobId/commit` |
| (idem) | `.../patients/import-image/...` |

Membutuhkan secret **`AnthropicAPIKey`** pada struct `secrets` di package `events` (sama dengan finance/catalog). Set via `encore secret set --type local AnthropicAPIKey` lalu **restart** `encore run`.

**Staf dari screenshot Google Sheet:** kolom umum `Nama Terapis`, `Apakah Bisa Datang?`, `Terapi Yang Anda Pilih`. Parser menerima `therapyNames` sebagai array atau string dipisah koma; kehadiran `"Bisa"` → hadir, teks lain (mis. `"Pagi sampai after lunch"`) → partial + catatan.

---

## Status acara & mutability

- `ARCHIVED` — hanya baca.
- Ubah data staf/pasien/slot ditolak jika acara tidak mutable (helper `assertEventMutable`).

---

## Keamanan data pasien

- `full_name_enc`, `birth_date_enc` — terenkripsi (tenant `DataEncryptionKey`).
- `normalized_name`, `normalized_birthdate` — untuk deduplikasi index.
- Duplikat pasien aktif per acara ditolak.

---

## Pengembangan lokal

```bash
cd api-go
encore check
encore run
```

Setelah deploy schema patch, buka halaman acara sekali per tenant agar `RunEventsSchemaPatches` menambah kolom/tabel baru.

**Verifikasi roster:**

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:4000/api/v1/events/staff-roster
```

---

## Troubleshooting

| Gejala | Penyebab umum |
|-------|----------------|
| Staf kosong / lambat | Patch schema; hindari query nested pada `rows` terbuka (sudah diperbaiki di `ListEventPeople`) |
| Generate slot gagal (MANUAL) | Belum simpan template slot di pengaturan terapi |
| Pasien dari kontak gagal | `birth_date` belum diisi di Contacts |
| Import roster 0 added | Roster kosong — sync dari acara lama atau tambah staf dengan checkbox roster |
| Export pasien ditolak | Terlalu banyak baris filter (> 2500) — persempit filter |
| Export job `failed` setelah 15 menit | Job `processing` kedaluwarsa — ulangi export |

---

## Changelog referensi (fitur UI terbaru)

- Tabel Pasien / Staf / Penugasan dengan search & pagination (frontend).
- `schedule_mode` AUTO / MANUAL + template slot.
- Roster staf + import ke acara baru.
- Pasien dari `contact_id` + `contact.birth_date`.
