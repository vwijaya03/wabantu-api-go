# Platform-admin auth (terpisah dari login client)

Branch `be/admin-auth`. Logika inti: `internal/adminauth` (tanpa Encore, unit-tested). Wiring:
`auth/login_portal.go`, `auth/admin_session.go`, `auth/platform_bootstrap.go`,
`auth/impersonation.go`, `middleware/session_kind.go`, `admin/admin.go`.

## Kontrak (fixed, CTO)

### `POST /api/v1/platform-admin/login` (public)
Body `{"email": "...", "password": "..."}` (JSON atau form).
- `200` → `{"accessToken": "<jwt>", "expiresAt": "2026-10-13T07:20:00Z"}` (RFC3339, UTC).
  **Tidak ada cookie yang di-set**; kirim sebagai `Authorization: Bearer`. (Cookie admin bernama
  `wabantu_admin_at` dibaca bila FE memilih menyimpannya sendiri; client tetap `wabantu_at`.)
- `401` `{"error":"Email atau password salah"}` — akun tidak ada, password salah, ATAU akun client
  (owner/staff) mencoba portal admin. Body identik.
- `429` `{"error":"too many attempts — coba lagi nanti"}` — rate limit / lockout.
- `400` `{"error":"email and password are required"}`.

Token admin: JWT `typ:"admin"`, berlaku 60 menit; sesi Redis `kind:"admin"` dengan batas
**absolut 2 jam** (tidak diperpanjang oleh reauth/touch).

### Profil (`me`)
- `GET /api/v1/platform-admin/me` — HANYA token admin (sesi `kind:admin`, belum lewat 2 jam) →
  `200 {"id","email","name","role"}`. Token client/impersonation → ditolak (middleware session-kind
  `403 permission_denied`, atau `401` dari handler).
- Klasifikasi: `/api/v1/platform-admin/*` = endpoint admin (prefix admin kedua, selain `/api/v1/admin/*`),
  dicocokkan lewat identitas route Encore yang sudah di-match (`auth.PlatformAdminMe`, `auth.PlatformAdminLogin`;
  `adminauth.PlatformAdminEndpoints`), bukan URL mentah — variasi `..`, `%2F`, huruf besar, trailing slash
  tidak mengubah keputusan. Tanpa sesi (exact match): hanya `auth.PlatformAdminLogin` dan
  `auth.BootstrapPlatformAdmin` (`adminauth.SessionlessEndpoints`). Bug sebelumnya: token admin ke `/me`
  ditolak middleware sebagai "client session required" (asal: d37f17e, d495df0; master 0ad6b5a tidak terdampak).
- `GET /api/v1/auth/me` — token client dan impersonation tetap jalan; token admin → `401`
  (tidak lagi dikecualikan).

### `POST /api/v1/auth/login` (client) — perubahan
Akun `super_admin` tanpa tenant kini ditolak dengan `401 Email atau password salah` (sama
dengan password salah). Response bertambah `sessionType:"client"`.

### `POST /api/v1/admin/impersonate/:tenantId` (admin token SAJA)
- Butuh grant persetujuan owner (tenant-access, tidak berubah).
- Mengembalikan token **impersonation terpisah**: `{ok, tenant, accessToken, expiresAt}`.
  JWT `typ:"impersonation"`, sesi `kind:"impersonation"`, berlaku **30 menit** (< sesi admin 2 jam).
- Token admin TIDAK diubah. FE memakai token impersonation untuk endpoint tenant
  (`/api/v1/orders`, dst) dan tetap memakai token admin untuk `/admin/*`.
- Token impersonation **ditolak di semua `/admin/*`** (termasuk AI config/catalog, impersonate
  dan stop-impersonation) → `403 permission_denied`.
- Audit: `audit_log` action `admin.impersonate.start` dengan `adminAccountId`, `adminEmail`,
  `tenantId`, `grantId`, `impersonationSessionId` (+ `impersonation_log` lama tetap).
- `POST /api/v1/admin/stop-impersonation` (admin token) mencabut sesi impersonation di Redis
  dan menulis `admin.impersonate.stop` (admin + tenant).

## Matriks akses (middleware global `SessionKind`)

| Token \ endpoint | `/api/v1/admin/*` & `tag:super_admin` | endpoint tenant/client | `/api/v1/internal/*` & private |
|---|---|---|---|
| client (`typ` kosong/`client`) | 403 | OK | n/a (private) |
| admin (`typ:admin`) | OK | **403** | OK (service-to-service) |
| impersonation | **403** | OK (sebagai tenant, scope grant) | OK |

`typ` di JWT wajib cocok dengan `kind` sesi di Redis (token client tidak bisa memakai sesi admin
dan sebaliknya) → 401 bila tidak cocok. `GET /api/v1/audit/logs` dan `GET/POST/PATCH /api/v1/flags`
kini ber-`tag:super_admin` (admin-only). `GET /api/v1/flags/check/:key` tetap client.

### Aturan keamanan: `HandlerEnforcedRaw` adalah bagian dari security boundary

- `adminauth.HandlerEnforcedRaw` membuat middleware `SessionKind` **melewatkan** pengecekan kind untuk endpoint raw tertentu (Encore membuang error middleware di endpoint raw → klien dapat 200 kosong). Endpoint di map ini **tidak** dilindungi middleware.
- Endpoint raw baru di bawah `/api/v1/platform-admin/` **hanya boleh** ditambahkan ke map ini bila handler-nya sendiri menegakkan sesi admin (mis. memanggil `adminauth.MeAccess` / validasi token `typ:admin` + kind sesi Redis, dan menolak client & impersonation dengan 401) — atau bila endpoint itu memang sessionless (`SessionlessEndpoints`, exact match: login & bootstrap saja).
- Menambah entri tanpa enforcement di handler = bypass otorisasi. Perubahan map ini wajib review Security.
- `TestPlatformAdminEndpointsComplete` menjaga sinkron: setiap route raw `/api/v1/platform-admin/*` harus terdaftar di `PlatformAdminEndpoints`, dan entri `HandlerEnforcedRaw` harus termasuk di sana; test gagal bila ada yang terlewat.

## Lockout & rate limit (Redis, `adminauth.Guard`)

Dihitung per **(akun,IP)**, per **IP**, dan per **akun** (email di-hash; akun yang tidak ada
dihitung sama persis → tidak bisa dipakai enumerasi):

| Portal | pair (akun+IP) | IP | akun (semua IP) | window/lock | rate per menit |
|---|---|---|---|---|---|
| admin | 5 gagal | 10 gagal | 20 gagal | 15 m / 15 m | 10/IP, 5/akun |
| client | 5 gagal | 30 gagal | 50 gagal | 15 m / 15 m | 20/IP, 10/akun (+ limit lama `AuthRPM`) |
| bootstrap | 5 gagal | 5 gagal | – | 15 m / 15 m | 5/IP |

Lock akun (semua IP) sengaja jauh di atas lock pair, jadi penyerang dari satu IP tidak bisa
mengunci owner lain dari IP-nya sendiri. Store Redis error/tidak ada: admin **fail-closed** (429),
bootstrap **fail-closed (503)**, client fail-open. `shared/ratelimit.Allow` (dipakai simulator dll.)
tetap fail-open; tidak diubah.
Timing: akun tidak ada → bcrypt compare ke hash dummy **asli** (cost 12) — dulu string dummy tidak
valid sehingga return instan (oracle timing). Portal salah juga melewati bcrypt penuh.

## Audit login (`auth_login_attempt`, migration system `28_auth_login_attempt.up.sql`)

Setiap percobaan (client, admin, bootstrap): `at, portal, email, account_id, ip, user_agent,
result (success|failure|blocked), reason`. `reason` hanya dari daftar kode tetap
(`invalid_credentials`, `portal_mismatch`, `locked_*`, `rate_limited_*`, `bootstrap_*`, …);
UA dipotong 256 char dan karakter kontrol dibuang. Tidak ada kolom/field untuk password, token
atau secret. Error body login hanya pesan generik di atas.

## Bootstrap `POST /api/v1/internal/platform-admin/bootstrap`

- Header `X-Platform-Bootstrap-Secret` vs Encore secret **baru** `PlatformAdminBootstrapKey`
  (secret lama `PlatformAdminBootstrapSecret` tidak dibaca lagi).
- Secret tidak diset / kosong / < 32 char → `403 {"error":"forbidden"}`; secret salah → `403`
  dengan body yang sama. Compare SHA-256 + `subtle.ConstantTimeCompare`.
- Satu kali: dalam SATU transaksi `pg_advisory_xact_lock` → cek super_admin tanpa tenant →
  `409 platform admin already exists` → insert → commit.
- Password min 12 char. Response `201 {accountId,email}` — **tidak menerbitkan token**; login
  lewat `/api/v1/platform-admin/login`.
- Semua percobaan tercatat di `auth_login_attempt` (portal `bootstrap`) tanpa secret.
- **Fail-closed (Security L-1):** Redis tidak ada atau error → `503 {"error":"temporarily unavailable"}`
  sebelum secret dicek (audit `blocked`, reason `store_unavailable`). Rate limit/lock → `429`.
  Policy: `adminauth.BootstrapPolicy()` (`FailClosed: true`).

## Redis WAJIB di Cloud Run

Session, lockout admin, dan bootstrap bergantung pada Redis (`RedisURL`). Di Cloud Run, Redis
**wajib** ada (Memorystore atau Redis eksternal, lihat `docs/DEPLOY_REDIS.md`). Tanpa Redis,
login gagal dan bootstrap menjawab 503. Rate limit lain yang fail-open (simulator) akan berjalan
tanpa batas, jadi jangan deploy tanpa Redis. Ini belum dikonfigurasi (butuh login GCP oleh Viko).

## Pencabutan sesi Firebase saat logout (Security M-1, bagian backend)

`POST /api/v1/auth/logout` dan stop-impersonation (`admin.StopImpersonation`) memanggil
`revokeFirebaseAsync(accountID)` → Identity Toolkit `accounts:update {localId, validSince=now}`
(setara Admin SDK `RevokeRefreshTokens`; uid Firebase = accountID dari `/auth/firebase-token`).
- Best-effort: goroutine terpisah, timeout 2 detik, lepas dari cancel request; gagal hanya di-log
  (`firebase revoke failed`). Logout tidak pernah menunggu atau gagal karenanya.
- No-op kalau `FIRESTORE_PROJECT_ID` / `FIREBASE_SERVICE_ACCOUNT_JSON` tidak diset.
- Butuh service account dengan izin Firebase Auth admin (scope `identitytoolkit`).
- Batasan: ID token yang sudah terbit tetap berlaku sampai kedaluwarsa (maks 1 jam), dan revoke
  berlaku untuk semua perangkat akun itu. Frontend tetap wajib `signOut` + `inMemoryPersistence` (M-1).
- Kode: `internal/realtime/revoke.go`, `auth/firebase_revoke.go`.

**Viko:** set KEY BARU (jangan pakai nilai lama) di tiap environment:
`openssl rand -base64 48 | tr -d '\n' | encore secret set --env=<env> PlatformAdminBootstrapKey`.
Setelah admin pertama dibuat, ganti ke nilai pendek mis. `disabled` → bootstrap mati (403).

## Dampak Frontend / tooling

- Admin console (`wabantu-web-frontend`) harus login via `/api/v1/platform-admin/login`
  (bukan `/auth/login`), simpan token admin terpisah dari token client, re-login tiap ≤ 60 menit /
  2 jam absolut (tidak ada refresh).
- "Pantau" tenant: simpan `accessToken` dari `/admin/impersonate` dan pakai untuk request
  tenant; jangan kirim token admin ke endpoint tenant (403) atau token impersonation ke `/admin/*`.
- Skrip/CI yang login super_admin lewat `/auth/login` atau memakai bootstrap lama (mengharapkan
  token dari bootstrap) harus diubah. `scripts/setup-secrets-for-cloud.sh` kini menset
  `PlatformAdminBootstrapKey`.
- Admin console memakai `/api/v1/platform-admin/me` (bukan `/auth/me`) untuk cek sesi.
- Endpoint raw public lain yang memakai `AuthenticateHTTP` (SSE, firebase-token) tidak
  melewati middleware; token admin di sana tidak punya tenant sehingga endpoint tenant menolak.
