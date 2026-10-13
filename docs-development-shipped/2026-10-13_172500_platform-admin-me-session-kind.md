# Fix: GET /api/v1/platform-admin/me ditolak untuk token admin

Catatan rilis (branch `be/fix-admin-me`, belum di-merge). Severity High (admin console tidak bisa cek sesi).

## Gejala
Token admin valid → `/api/v1/platform-admin/me` tidak mengembalikan profil; log `client session required` code=permission_denied endpoint=PlatformAdminMe.

## Akar masalah
`middleware/session_kind.go` → `adminauth.RequestAccess`: `IsAdminEndpoint` hanya mengenal `/api/v1/admin/*` dan tag `super_admin`, jadi `/api/v1/platform-admin/me` dianggap endpoint client → token admin ditolak. Asal: d37f17e, d495df0. Master 0ad6b5a tidak punya kode ini.

## Perbaikan
- `/api/v1/platform-admin/*` = prefix admin kedua, dicocokkan via identitas route Encore (`service.Endpoint`), bukan URL mentah.
- Tanpa cek sesi hanya (exact): `auth.PlatformAdminLogin`, `auth.BootstrapPlatformAdmin`.
- Test: sinkron dengan source, matrix admin/client/impersonate × {platform-admin/me, /admin/*}, variasi path (`..`, `%2F`, case, trailing slash), sessionless exact-only.

## Catatan raw endpoint
Encore tidak menulis error middleware untuk endpoint `raw` (klien dapat `200` body kosong — itulah gejala awalnya). `auth.PlatformAdminMe` dan `auth.PlatformAdminLogin` (raw) memeriksa jenis sesi di handler sendiri (`MeAccess` → `401`, cek portal login), jadi middleware menyerahkan ke handler (`adminauth.HandlerEnforcedRaw`).

Live (127.0.0.1:4100, DB sintetis): admin → `/platform-admin/me` 200 + profil; client/impersonate → 401; tanpa token → 401. `/admin/*`: admin 200, client/impersonate 403, tanpa token 401. Variasi path: trailing slash → 301 ke path kanonik; `%2F`, `..`, huruf besar → 404.
