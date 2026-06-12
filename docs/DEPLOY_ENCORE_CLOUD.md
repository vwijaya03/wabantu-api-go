# Deploy `api-go` ke Encore Cloud

Tutorial deploy backend WABantu (Encore.go) ke **Encore Cloud**, termasuk secrets, push git, migrasi data, dan koneksi frontend.

**App ID repo ini:** `wabantu-viko-8vni` (lihat `encore.app`).

Setup Redis cloud (wajib): [DEPLOY_REDIS.md](./DEPLOY_REDIS.md).

---

## Ringkasan arsitektur

```
┌─────────────────────────────────────────────────────────┐
│  Encore Cloud (env: staging / production)             │
│  ├── api-go (HTTP :443)                                 │
│  ├── PostgreSQL system + tenant  ← otomatis Encore      │
│  └── Pub/Sub + Encore Cache      ← otomatis Encore      │
└───────────────────────────┬─────────────────────────────┘
                            │ RedisURL (secret)
                            ▼
┌─────────────────────────────────────────────────────────┐
│  Upstash / provider Redis eksternal                     │
│  (session, SSE, rate limit, staging import)           │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  web-frontend (Vercel) — opsional                       │
│  API_BACKEND_URL → URL Encore Cloud                     │
└─────────────────────────────────────────────────────────┘
```

Deploy **bukan** perintah `encore deploy`. Encore Cloud deploy lewat **git push** (remote `encore` atau GitHub).

---

## Prerequisites

| Tool / akun | Cek |
|-------------|-----|
| [Encore CLI](https://encore.dev/docs/install) | `encore version` |
| Login Encore Cloud | `encore auth login` |
| App ter-link | `encore.app` berisi id valid |
| Git | repo `api-go` punya commit siap push |
| Redis cloud | Upstash URL — lihat [DEPLOY_REDIS.md](./DEPLOY_REDIS.md) |
| `../api/.env` | Sumber nilai secret (jangan commit) |
| `pg_dump` / `pg_restore` | Hanya jika migrasi data lokal |

---

## Checklist sebelum deploy pertama

- [ ] `encore auth login`
- [ ] Redis Upstash sudah dibuat, URL `rediss://...` siap
- [ ] Secrets env cloud sudah di-set (`setup-secrets-for-cloud.sh` atau manual)
- [ ] `encore check` lulus (opsional tapi disarankan)
- [ ] Perubahan kode sudah di-commit
- [ ] Remote `encore` sudah ditambah **atau** GitHub terintegrasi di dashboard

---

## Langkah 1 — Login & pastikan app terdaftar

```bash
cd api-go
encore auth login
```

Tanpa browser / di CI:

```bash
encore auth login --auth-key=<KEY_DARI_DASHBOARD>
```

Auth key: https://app.encore.cloud/settings/access/auth-keys

App sudah ada di `encore.app`:

```json
{
  "id": "wabantu-viko-8vni",
  "lang": "go"
}
```

Kalau belum ter-link ke akun kamu:

```bash
encore app link wabantu-viko-8vni -f
```

Dashboard: https://app.encore.cloud/

---

## Langkah 2 — Siapkan Redis cloud

**Wajib** sebelum push pertama. Detail lengkap: [DEPLOY_REDIS.md](./DEPLOY_REDIS.md).

Ringkas:

1. Buat database di Upstash (region Singapore)
2. Copy **Redis URL** (`rediss://...`)

---

## Langkah 3 — Set secrets untuk environment cloud

Secret `type:local` **tidak** dipakai di cloud. Set per **nama environment** (mis. `staging`).

### Opsi A — Script (disarankan)

Copy secret dari `../api/.env` + `REDIS_URL` cloud:

```bash
cd api-go

REDIS_URL='rediss://default:PASSWORD@HOST.upstash.io:6379' \
  ./scripts/setup-secrets-for-cloud.sh staging
```

### Opsi B — Manual

```bash
cd api-go

printf '%s' 'rediss://...' | encore secret set --env=staging RedisURL
printf '%s' '...' | encore secret set --env=staging JWTSecret
printf '%s' '...' | encore secret set --env=staging DataEncryptionKey
printf '%s' '...' | encore secret set --env=staging AnthropicApiKey
printf '%s' '...' | encore secret set --env=staging AnthropicAPIKey
printf '%s' '...' | encore secret set --env=staging AiInternalToken
printf '%s' '...' | encore secret set --env=staging WebhookVerifyToken
printf '%s' '...' | encore secret set --env=staging MidtransServerKey
printf '%s' '...' | encore secret set --env=staging MidtransClientKey
printf '%s' 'false' | encore secret set --env=staging MidtransIsProduction
printf '%s' '...' | encore secret set --env=staging RajaOngkirApiKey
printf '%s' 'starter' | encore secret set --env=staging RajaOngkirAccountType
printf '%s' '...' | encore secret set --env=staging SentryDSN
printf '%s' '...' | encore secret set --env=staging PlatformAdminBootstrapSecret
```

Mapping nama secret ↔ `api/.env`: lihat tabel di [README.md](../README.md#3-set-secrets-dari-apienv).

Verifikasi:

```bash
encore secret list --env=staging
```

> **`DataEncryptionKey` harus sama** dengan lokal jika kamu akan migrasi data terenkripsi dari laptop.

---

## Langkah 4 — Push untuk trigger deploy

### Opsi A — Remote Encore (paling cepat)

Sekali saja:

```bash
cd api-go
git remote add encore encore://wabantu-viko-8vni
```

Setiap deploy:

```bash
git add -A
git commit -m "Deploy to Encore staging"
git push encore master
```

Ganti `master` jika branch utama kamu `main`.

### Opsi B — GitHub (disarankan production)

1. Dashboard → **wabantu-viko** → **App Settings** → **Integrations** → **GitHub**
2. Connect repo `vwijaya03/wabantu-api-go`
3. Environment **staging** → Settings → set branch deploy (mis. `master`)
4. Push seperti biasa:

```bash
git push origin master
```

---

## Langkah 5 — Pantau deploy

1. Buka https://app.encore.cloud/
2. Pilih app **wabantu-viko**
3. Tab **Deployments** / **Rollouts**

Fase deploy:

1. **Build & test** — compile semua service Encore
2. **Infrastructure** — provision Postgres, Pub/Sub, cache
3. **Deploy** — jalankan API

Deploy pertama biasanya **5–15 menit**.

Log dari CLI:

```bash
encore logs --env=staging
```

---

## Langkah 6 — Verifikasi setelah deploy sukses

Panduan lengkap **Postman**, **TablePlus**, dan perbandingan lokal vs staging: **[STAGING_ACCESS.md](./STAGING_ACCESS.md)**.

### URL API

Di dashboard → environment **staging** → salin URL publik, mis.:

```
https://staging-wabantu-viko-8vni.encr.app
```

Base path API selalu:

```
https://staging-wabantu-viko-8vni.encr.app/api/v1
```

> URL `*.encr.app` = HTTP API saja. Database Postgres diakses lewat `encore db shell` / `encore db proxy`, bukan lewat browser atau TablePlus langsung ke domain Encore.

### Health / API (curl / Postman)

```bash
BASE=https://staging-wabantu-viko-8vni.encr.app/api/v1

curl -s -X POST "$BASE/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"email":"...","password":"..."}'
```

Postman: set variable `base_url` = URL di atas; login `POST {{base_url}}/auth/login`; request lain pakai `Authorization: Bearer {{access_token}}`. Detail: [STAGING_ACCESS.md](./STAGING_ACCESS.md).

### Database cloud (terminal)

```bash
encore db shell system --env=staging --write
# SELECT count(*) FROM tenant;

encore db shell tenant --env=staging --write
# \dn   -- list schema tenant t_*
```

### Database cloud (TablePlus)

```bash
# Terminal 1 — biarkan jalan
encore db proxy tenant --env=staging --write -p 5433

# Terminal 2 — ambil user/password
encore db conn-uri tenant --env=staging --write
```

TablePlus: Host `127.0.0.1`, Port `5433`, Database `tenant`, SSL off. Langkah lengkap: [STAGING_ACCESS.md](./STAGING_ACCESS.md).

### Login

Buka frontend (atau API login) — pastikan tidak error Redis. Kalau gagal, cek [DEPLOY_REDIS.md](./DEPLOY_REDIS.md). Setelah migrasi DB, user harus login ulang (session Redis tidak ikut).

---

## Langkah 7 — Migrasi data lokal (opsional)

Pindahkan data Postgres dari laptop ke cloud **setelah deploy pertama sukses**.

Yang **bisa** dimigrasi:

- DB `system` (tenant, akun, billing control-plane)
- DB `tenant` (semua schema `t_*` — pasien, event, inbox, dll.)

Yang **tidak** dimigrasi:

- Redis (session) → user **login ulang**
- Encore Pub/Sub / cache internal → kosong di awal, normal

### Persiapan

1. Hentikan tulis ke DB lokal (jangan `encore run` yang aktif menulis)
2. Pastikan `pg_dump` dan `pg_restore` terpasang
3. Pastikan schema cloud sudah ada (deploy sukses minimal sekali)

### Jalankan script

Dry-run (export saja, tanpa restore):

```bash
cd api-go
./scripts/migrate-local-db-to-encore.sh staging --dry-run
```

Migrasi penuh:

```bash
./scripts/migrate-local-db-to-encore.sh staging
```

Script akan:

1. `pg_dump` schema + data lokal `system`
2. `pg_dump` schema `t_*` (DDL) + data tenant dari lokal
3. Konfirmasi `y/N`
4. `pg_restore` **schema** system ke cloud (wajib jika cloud DB masih kosong)
5. `pg_restore` data system (`encore db conn-uri --admin`)
6. `pg_restore` schema tenant → data tenant
7. **GRANT** otomatis di DB `system` + schema `t_*` (wajib — tanpa ini login mengembalikan `{"message":"db error"}`)

Backup dump disimpan di `api-go/.db-migrate/<timestamp>/`.

### Setelah migrasi

1. **Login ulang** di staging (Redis tidak ikut migrasi) — [DEPLOY_REDIS.md](./DEPLOY_REDIS.md)
2. Test: `POST .../api/v1/auth/login` — harus `Email atau password salah` (bukan `db error`)
3. Jika masih `db error`, jalankan ulang GRANT:

```bash
./scripts/fix-cloud-db-grants.sh staging
```

### Manual (tanpa script)

```bash
pg_dump "$(encore db conn-uri system)" -Fc --data-only --disable-triggers -f system.dump
pg_dump "$(encore db conn-uri tenant)"  -Fc --data-only --disable-triggers -f tenant.dump

pg_restore -d "$(encore db conn-uri system --env=staging --admin)" \
  --data-only --disable-triggers --no-owner --no-privileges system.dump

pg_restore -d "$(encore db conn-uri tenant --env=staging --admin)" \
  --data-only --disable-triggers --no-owner --no-privileges tenant.dump
```

---

## Langkah 8 — Hubungkan frontend (Vercel)

Set environment di `web-frontend`:

| Variable | Nilai |
|----------|-------|
| `API_BACKEND_URL` | `https://staging-....encr.app` |
| `NEXT_PUBLIC_API_URL` | `/api/v1` |
| `NEXT_PUBLIC_SSE_API_URL` | `https://staging-....encr.app` (langsung ke API, bukan lewat rewrite Vercel) |

Redeploy frontend setelah ubah env.

---

## Langkah 9 — Webhook & integrasi eksternal

Update URL callback ke domain Encore Cloud:

| Integrasi | Yang diubah |
|-----------|-------------|
| **Meta WhatsApp** | Webhook URL → `https://staging-....encr.app/webhook/whatsapp` (atau path yang dipakai) |
| **Midtrans** | Notification URL production/sandbox |
| **OAuth redirect** | Jika ada callback ke API |

`WebhookVerifyToken` secret harus sama dengan yang didaftarkan di Meta Developer Console.

---

## Langkah 7b — Schema tenant & PII di cloud (setelah deploy)

Di Encore Cloud, role aplikasi **tidak bisa** `CREATE TABLE` / `ALTER` — tenant lama yang di-migrate bisa ketinggalan patch (pricing, `contact.status`, dll.). Jalankan dengan role **admin**:

```bash
./scripts/apply-tenant-schema-cloud.sh staging
./scripts/apply-pii-schema-cloud.sh staging
```

Script idempotent: menambah kolom `*_enc` / `*_idx` per schema `t_*`. Tabel modul opsional (events, finance, broadcast) **dilewati** jika belum ada di tenant tersebut — aman untuk schema parsial seperti `t_omah_apparel_1`.

Lalu backfill data lama untuk **semua** tenant sekaligus:

```bash
./scripts/backfill-pii-cloud.sh staging
```

Verifikasi semua migrasi tenant sudah lengkap:

```bash
./scripts/verify-tenant-migrations-cloud.sh staging
```

(Satu schema saja: `DATABASE_URL=... DATA_ENCRYPTION_KEY=... go run ./scripts/cmd/backfill-pii/ -schema=t_xxx`)

---

## Deploy ulang (update kode)

Setelah setup awal, deploy berikutnya cukup:

```bash
git add -A
git commit -m "..."
git push encore master    # atau git push origin master jika pakai GitHub
```

Secrets **tidak** perlu di-set ulang kecuali ada secret baru atau rotasi.

---

## Environment & limit free tier

Encore Cloud free (fair use) kira-kira:

| Resource | Limit |
|----------|-------|
| Request | ~100.000 / hari |
| Database storage | ~1 GB |
| Pub/Sub messages | ~100.000 / hari |

Tanpa SLA — cocok staging, demo, development bersama tim.

Custom domain & production AWS/GCP: plan Pro — https://encore.dev/docs/platform/management/billing

---

## Troubleshooting deploy

| Gejala | Penyebab | Solusi |
|--------|----------|--------|
| Build gagal di dashboard | Error compile / test | `encore check` lokal; baca build logs di dashboard |
| Deploy sukses, login gagal | `RedisURL` localhost / kosong | [DEPLOY_REDIS.md](./DEPLOY_REDIS.md) |
| `fetch secrets ... failed` | Secret belum di env cloud | `setup-secrets-for-cloud.sh staging` |
| `app_not_found` | App belum ter-link | `encore app link wabantu-viko-8vni -f` |
| `not logged in` saat `secret set` | CLI belum login | `encore auth login` |
| Frontend 502 / timeout | `API_BACKEND_URL` salah | Cek URL staging di dashboard |
| SSE inbox tidak update | SSE lewat Vercel rewrite | Set `NEXT_PUBLIC_SSE_API_URL` langsung ke Encore |
| Data kosong setelah deploy | Normal tanpa migrasi | Register ulang atau jalankan `migrate-local-db-to-encore.sh` |
| `pg_restore` error constraint | Schema belum ada / urutan salah | Deploy sukses dulu; pakai `--disable-triggers` seperti script |
| `pg_restore` `must be owner` / `permission denied` | Conn-uri cloud default = **read-only** | Script pakai `--admin`; manual: `encore db conn-uri system --env=staging --admin` |
| `schema "t_..." does not exist` | Schema tenant belum dibuat di cloud | Jalankan script terbaru (langkah schema-only); atau `pg_dump --schema-only` dulu |
| `pg_restore` exit 1, `RI_ConstraintTrigger` | Managed PG tidak bisa `DISABLE TRIGGER` pada FK system | Abaikan jika script cetak `ok system tenant rows: N` |
| `schema "public" already exists` | Schema system sudah ada di cloud | Normal saat re-run; script terbaru skip DDL jika tabel sudah ada |
| Login `db error` setelah migrasi | `pg_restore --no-privileges` — role `encore_writer` tidak punya `SELECT` | `./scripts/fix-cloud-db-grants.sh staging` (script migrasi terbaru sudah GRANT otomatis) |
| Deploy gagal: `permission denied for schema t_*` (dynamic grants) | Schema/tabel bukan milik `encore-migrator` setelah `pg_restore` | `./scripts/fix-cloud-db-grants.sh staging` → `./scripts/verify-cloud-deploy-ready.sh staging` → redeploy |
| Deploy gagal: `permission denied for table schema_migrations` | Tabel migrasi system DB owner salah | Sama — `fix-cloud-db-grants.sh` (reassign ke `encore-migrator`) |
| API `prepare catalog pricing failed` / DDL error | App role cloud tidak bisa `CREATE`/`ALTER`/`DROP` | Deploy kode terbaru (`shared/tenantschema` skip DDL jika schema sudah ada); `./scripts/verify-cloud-tenant-schemas.sh staging` |
| `relation "public.tenant" does not exist` | DB cloud **kosong** (belum ada tabel) | Script terbaru restore **schema system** dulu; atau `git push encore` sampai deploy sukses |
| Webhook Meta gagal verifikasi | Token tidak sama | Samakan `WebhookVerifyToken` dengan Meta console |

---

## Deploy alternatif (bukan Encore Cloud)

### Docker image

```bash
encore build docker wabantu
```

Jalankan image + Postgres + Redis sendiri. Contoh compose: `../infra/docker-compose.yml`.

### AWS / GCP via Encore Pro

Connect cloud account di dashboard → Encore provision RDS, dll. Lihat https://encore.dev/docs/platform/infrastructure/infra

---

## File terkait

| File | Fungsi |
|------|--------|
| [STAGING_ACCESS.md](./STAGING_ACCESS.md) | Postman, TablePlus, `db shell` / `db proxy` staging |
| [DEPLOY_REDIS.md](./DEPLOY_REDIS.md) | Setup Upstash & `RedisURL` |
| `scripts/setup-secrets-for-cloud.sh` | Copy secrets `api/.env` → env cloud |
| `scripts/setup-secrets-from-env.sh` | Copy secrets → `type:local` |
| `scripts/migrate-local-db-to-encore.sh` | Migrasi Postgres lokal → cloud (+ GRANT otomatis) |
| `scripts/fix-cloud-db-grants.sh` | Reassign owner ke `encore-migrator` + GRANT (wajib setelah migrasi DB) |
| `scripts/verify-cloud-deploy-ready.sh` | Cek owner `schema_migrations` + `t_*` sebelum push deploy |
| `scripts/apply-tenant-schema-cloud.sh` | DDL pricing, `contact.status`, branch, workflow per `t_*` |
| `scripts/apply-pii-schema-cloud.sh` | DDL kolom PII (`*_enc`, `*_idx`) semua schema `t_*` |
| `scripts/backfill-pii-cloud.sh` | Backfill plaintext → terenkripsi semua tenant di cloud |
| `scripts/verify-tenant-migrations-cloud.sh` | Cek PII, kolom contact, plaintext tersisa, GRANT per `t_*` |
| `scripts/verify-cloud-tenant-schemas.sh` | Cek tabel tenant + GRANT SELECT di cloud |
| `shared/tenantschema/` | Skip runtime DDL di Encore Cloud bila schema sudah lengkap |
| `encore.app` | App ID Encore |
| `../infra/docker-compose.yml` | Redis & Postgres lokal (dev) |

---

## Urutan cepat (cheat sheet)

```bash
# 1. Redis Upstash → dapat rediss://...

# 2. Secrets
cd api-go
REDIS_URL='rediss://...' ./scripts/setup-secrets-for-cloud.sh staging

# 3. Remote (sekali)
git remote add encore encore://wabantu-viko-8vni

# 4. Deploy
git add -A && git commit -m "Deploy staging" && git push encore master

# 5. Pantau dashboard → dapat URL API

# 6. (Opsional) Migrasi data
./scripts/migrate-local-db-to-encore.sh staging

# 7. WAJIB setelah migrasi — sebelum deploy berikutnya
./scripts/fix-cloud-db-grants.sh staging
./scripts/verify-cloud-deploy-ready.sh staging
```
