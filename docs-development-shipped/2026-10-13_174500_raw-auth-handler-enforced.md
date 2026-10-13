# Fix: /auth/me, /auth/reauth, /auth/logout dengan token admin → 200 kosong

Catatan rilis (branch `be/admin-auth`, belum di-push). Ditemukan saat QA live 2026-10-13.

## Gejala (live :4000, ae645d1)
- Token admin: `/auth/me`, `/auth/reauth`, `/auth/logout` → **200 + body kosong**. Middleware `SessionKind` menolak (log `permission_denied "client session required"`, durasi ~0,1 ms) tetapi Encore membuang error middleware pada endpoint raw. Handler **tidak** jalan: logout tidak menghapus sesi admin (token masih 200 di `/platform-admin/me`), reauth tidak menerbitkan token.
- Bukan bypass otorisasi, tetapi status salah dan admin tidak punya logout yang berfungsi.

## Perbaikan
- `adminauth.RawHandlerAccess`: me/reauth → client + impersonation (admin → 401); logout → semua kind.
- `auth.Me`, `auth.Reauth`, `auth.Logout` masuk `HandlerEnforcedRaw`; handler menegakkan sendiri. Reauth mengecek kind sebelum verifikasi password.
- Test: `TestRawHandlerAccessMatrix`, `TestHandlerEnforcedRawIsBounded`.

## Dampak FE
- Admin di `/auth/me` / `/auth/reauth` kini 401 (bukan 200 kosong) — pakai `/api/v1/platform-admin/me`. Logout admin via `/auth/logout` kini benar-benar menghapus sesi.
