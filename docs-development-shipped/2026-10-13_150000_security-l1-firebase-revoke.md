# Security L-1 + revoke Firebase saat logout (PR #217)

- **L-1:** bootstrap platform admin fail-closed. Redis tidak ada/error → 503 (`adminauth.BootstrapPolicy`, `Guard.CheckStatus`). `shared/ratelimit.Allow` tidak diubah (tetap fail-open untuk caller lain). `redisCounters` aman untuk client nil.
- **Redis WAJIB di Cloud Run** (docs/ADMIN_AUTH.md, docs/DEPLOY_REDIS.md). Belum dikonfigurasi di infra.
- **Firebase revoke:** logout dan stop-impersonation mencabut refresh token Firebase untuk uid=accountID lewat Identity Toolkit REST. Async, timeout 2 detik, no-op tanpa `FIREBASE_SERVICE_ACCOUNT_JSON`, tidak pernah memblokir logout.
- Tes: `go test ./internal/adminauth ./internal/realtime` (fake store nil/error → 503, rate limit → 429, mode client/admin tidak berubah; fake HTTP Identity Toolkit, fake revoker non-blocking/detached/timeout).
- Belum diverifikasi: Redis/Firebase live, `encore test`.
