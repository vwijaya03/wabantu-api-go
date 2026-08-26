# Hint super_admin saat acara milik tenant lain

**Tanggal:** 2026-08-26  
**PR:** [#128](https://github.com/vwijaya03/wabantu-api-go/pull/128)  
**Tipe:** fix  
**Status:** Dalam PR (belum merge ke `master`)

## Masalah / Kebutuhan

Super admin dengan tenant efektif `t_stark_services` membuka detail acara milik tenant lain (mis. `t_omah_apparel`) dan mendapat **404 generik** `acara tidak ditemukan`. Ini membingungkan karena acara memang ada — hanya berada di schema tenant berbeda. Super admin perlu petunjuk jelas untuk memakai fitur **Pantau** ke tenant pemilik.

## Perubahan

- Tambah `events/event_lookup.go` dengan `lookupEventOwnerTenant` — cari pemilik acara di schema tenant lain saat tidak ditemukan di schema aktif.
- `GetEvent`, `GetEventSchedule`, dan endpoint terkait:
  - **403** + pesan jelas untuk `super_admin` (nama tenant pemilik + instruksi Pantau) jika acara ada di tenant lain.
  - **404** tetap jika acara benar-benar tidak ada di mana pun.
  - Tenant biasa yang mengakses acara tenant lain → tetap **404** generik (tidak bocorkan informasi cross-tenant).
- File events terkait di-update untuk memakai lookup baru: `dashboard.go`, `event.go`, `event_duplicate.go`, `export_job.go`, `helpers.go`, `image_import.go`, `patients.go`, `public_monitor.go`, `staff_roster.go`.

## File utama

- `events/event_lookup.go` (baru)
- `events/event.go`, `events/dashboard.go`, `events/helpers.go`

## Testing

- [ ] Login super_admin (tenant home ≠ tenant acara), buka URL detail acara tenant lain → 403 dengan pesan tenant pemilik
- [ ] Impersonate tenant pemilik, buka acara yang sama → sukses
- [ ] Owner tenant biasa buka acara tenant lain → tetap 404 generik
- [ ] `encore test ./events/...`

## Catatan deploy

- PR frontend terpisah untuk UI error state di halaman detail acara.
- Tidak mengubah model data; hanya perilaku lookup dan respons HTTP.
