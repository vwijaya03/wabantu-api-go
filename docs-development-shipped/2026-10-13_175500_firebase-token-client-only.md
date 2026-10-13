# Fix: POST /api/v1/auth/firebase-token hanya untuk sesi client

Catatan rilis (branch `be/admin-auth`, belum di-push). Keputusan CTO opsi B, 2026-10-13.

## Masalah
- Token impersonation lolos cek role (`super_admin`) dan tenant (`ActAsTenantID`) di `internal/realtime/token_http.go:34-48`, lalu akan mendapat Firebase custom token dengan uid **akun admin** + klaim `tenantId` tenant yang dipantau. Lokal hanya terlihat 503 karena Firebase tidak dikonfigurasi.
- Token admin: middleware menolak, tetapi Encore membuang error pada endpoint raw, sehingga klien dapat **200 + body kosong**.

## Perbaikan
- `auth.FirebaseToken` masuk `HandlerEnforcedRaw`; `adminauth.RawHandlerAccess("auth.FirebaseToken")`: client OK, admin → 401, impersonation → 403 (`ErrImpersonationForbidden`). Error JSON `{code,message}`.
- Cek kind berjalan sebelum cek role/tenant/konfigurasi Firebase (`serveFirebaseTokenFor`).
- `revokeFirebaseAsync` pada logout/stop-impersonation tidak diubah.
- Test: `TestRawHandlerAccessMatrix`, `auth.TestFirebaseTokenKindGate`, `auth.TestFirebaseTokenKindCheckedBeforeConfig`.

## Dampak FE
- Saat pantau (impersonation) realtime Firestore tidak tersedia (403): pakai fallback REST polling. Admin portal tidak memakai endpoint ini.
- Lanjutan (opsi A, pasca-kontes): `tickets/impersonate-firebase-own-uid.md`.
