# API HTTP Smoke Tests

Encore integration smoke untuk **20+ service** — bootstrap tenant + auth, panggil handler typed API (`et.OverrideAuthInfo`).

## Menjalankan

```bash
./scripts/run-api-smoke-tests.sh
# atau
encore test ./internal/apitest/ -count=1 -v
```

Skip DB/Redis: `encore test ./internal/apitest/ -short`

## Cakupan

| Area | Tests |
|------|-------|
| R1 health/auth | `TestHealthSmoke_*`, `TestAuthSmoke_*` |
| R2 order/inbox | `TestOrder*`, `TestInbox*` |
| R3 inventory/finance | `TestInventorySmoke_*`, `TestFinanceSmoke_*` |
| R4+ services | events, billing, branch, leads, analytics, business, kb, usage |
| Public | webhook, payment Midtrans, health |
| Lainnya | flag, shipping, notification, whatsappapi, ai models |

## Menambah smoke baru

1. `BootstrapOwner(t)` + `WithOwnerAuth(fx)`
2. Panggil handler Encore langsung (bukan raw `//encore:api` — gunakan `Serve*HTTP` di package target)
3. `AssertJSONFields` / `AssertJSONArrayField`

Raw handler wrappers: `auth/http_testutil.go`, `webhook/http_testutil.go`, `payment/http_testutil.go`.

## CI

`.github/workflows/api-smoke.yml` — PR ke `master` saat `internal/apitest/**` berubah.
