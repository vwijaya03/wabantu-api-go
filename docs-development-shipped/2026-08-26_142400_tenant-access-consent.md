# Persetujuan akses tenant (consent) sebelum Pantau

**Tanggal:** 2026-08-26  
**PR:** [#130](https://github.com/vwijaya03/wabantu-api-go/pull/130)  
**Tipe:** feat  
**Status:** Dalam PR (belum merge ke `master`)

## Masalah / Kebutuhan

Super admin dapat langsung impersonate / Pantau tenant tanpa persetujuan owner. Ini tidak sesuai keputusan produk: akses lintas-tenant harus **explicit consent** dari owner tenant, dengan scope modul dan durasi yang terkontrol.

## Perubahan

### Skema & migrasi

- Tabel system baru: `tenant_access_request`, `app_notification` (migration `14_tenant_access_consent.up.sql` + cloud patch di `shared/systemschema/cloud_patch_sql.go`).

### Alur consent

- Super admin mengajukan akses via API admin.
- Owner menyetujui / menolak / mencabut dengan:
  - **Scope:** `full` atau `limited`
  - **Modul:** `main`, `sales`, `inventory`, `finance`, `ai`, `org`, `advanced` (selaras `nav-config.ts`)
  - **Durasi:** 24 jam / 7 hari / 30 hari / permanen
- Grant `full` = `ImpersonationModules` kosong (akses penuh semua modul).

### Gate impersonasi

- `StartImpersonation` / `admin.Impersonate`: tanpa grant aktif → error *"minta persetujuan owner"*.
- Validasi grant tiap request via `reconcileImpersonationGrant` + `RequireModule` pada modul utama (order, finance, inbox, inventory, kb, business, events, broadcast, leads).
- **Tidak ada break-glass** — semua pantau tenant memerlukan grant owner.

### Notifikasi & session

- Notifikasi in-app untuk owner (request baru) dan requester (approve / reject / revoke).
- `GET /auth/me` expose scope, modul, dan masa berlaku impersonasi aktif.
- Service baru: `notification/`, `tenantaccess/` (`api.go`, `grant.go`, `service.go`).

## File utama

- `system/migrations/14_tenant_access_consent.up.sql`
- `tenantaccess/service.go`, `tenantaccess/grant.go`, `tenantaccess/api.go`
- `notification/notification.go`
- `auth/impersonation.go`, `auth/userctx.go`
- `shared/types/modules.go`

## Testing

- [x] `encore test ./shared/types/... ./tenantaccess/...`
- [x] `encore test ./order/... ./finance/... ./inbox/... ./inventory/... ./kb/... ./business/... ./events/...`
- [ ] Manual: SA tanpa grant → POST impersonate ditolak
- [ ] Manual: approve limited `finance` → order API 403, finance OK
- [ ] Manual: revoke → request berikutnya drop impersonasi

## Catatan deploy

- **Breaking change operasional:** SA yang sudah bisa pantau langsung **kehilangan akses** sampai owner menyetujui request pertama (by design).
- Tenant tanpa owner aktif tidak bisa menerima request (failed precondition).
- Wajib jalankan migrasi system #14 sebelum deploy; koordinasi dengan PR frontend untuk UI consent & notifikasi.
