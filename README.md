# WABantu API (Encore.go)

Backend WABantu ditulis ulang dengan [Encore.go](https://encore.dev). Kode NestJS asli tetap ada di `../api/` sebagai referensi.

Dokumentasi alur lengkap (mirip `api/APP_FLOW_GUIDE.md`): **[APP_FLOW_GUIDE.md](./APP_FLOW_GUIDE.md)**.

Perbandingan endpoint vs NestJS / kompatibilitas frontend: **[ENDPOINT_COMPATIBILITY.md](./ENDPOINT_COMPATIBILITY.md)**.

Dokumentasi teknis lengkap (arsitektur, API, DB, **platform admin**): **[DEVELOPER_DOCUMENTATION.md](./DEVELOPER_DOCUMENTATION.md)** — untuk akun operator internal WABantu langsung ke **[Bagian 8.1](./DEVELOPER_DOCUMENTATION.md#81-platform-admin-internal-operator-wabantu-owner)**.

---

## Untuk developer baru — checklist sebelum coding

Centang semua ini **sebelum** `encore run` atau membuka `web-frontend`. Kalau ada yang belum, gejala di kolom kanan troubleshooting di bawah.

| # | Harus sudah ready | Cara cek |
|---|-------------------|----------|
| 1 | **Docker Desktop** jalan | `docker info` tanpa error |
| 2 | **Go 1.24+** | `go version` |
| 3 | **Encore CLI** terpasang | `encore version` |
| 4 | **Node.js 18+** (untuk frontend) | `node -v` — Next 16 butuh ≥ 18 |
| 5 | **Redis** jalan (session, rate limit, import staging, SSE) | `docker compose -f ../infra/docker-compose.yml ps redis` atau `redis-cli ping` → `PONG` |
| 6 | **Encore login** | `encore auth login` (sekali per laptop) |
| 7 | **App terdaftar** di Encore Cloud | `encore.app` punya `id` valid; tidak error `app_not_found` saat `encore run` |
| 8 | **Secrets** ter-set | `encore secret list` → minimal `JWTSecret`, `DataEncryptionKey`, `RedisURL`; untuk platform admin tambah `PlatformAdminBootstrapSecret` ([Bagian 8.1](./DEVELOPER_DOCUMENTATION.md#81-platform-admin-internal-operator-wabantu-owner)) |
| 9 | File **`../api/.env`** ada (sumber secret) | `ls ../api/.env` — copy dari `api/.env.example` bila perlu |
| 10 | **`encore check`** lulus | `cd api-go && encore check` |

**Urutan hari pertama (full stack lokal):**

```bash
# 1) Redis
cd ../infra && docker compose up -d redis

# 2) Backend
cd ../api-go
encore auth login
./scripts/setup-secrets-from-env.sh
encore check
encore run                    # API :4000, dashboard :9400

# 3) Frontend (terminal baru, Node 18+)
cd ../web-frontend
cp .env.example .env.local    # atau pakai .env yang sudah ada
npm install
npm run dev                   # :3000 → rewrite /api/v1 → :4000
```

Buka `http://localhost:3000` → register/login → dashboard. **Jangan** jalankan Nest `api/` (port 3001) atau `ai-worker/` untuk stack ini.

Detail alur: **[APP_FLOW_GUIDE.md](./APP_FLOW_GUIDE.md)** · Frontend: **`../web-frontend/README.md`**.

---

## Perbedaan utama vs NestJS (`api/`)

| Topik | NestJS `api/` | Encore `api-go/` |
|--------|----------------|------------------|
| Config | File `api/.env` | **Encore Secrets** (`encore secret set`) |
| Prefix URL | `/api/v1/...` | **`/api/v1/...`** (sama dengan Nest) |
| Port dev default | `3001` | **`4000`** (API) + **`9400`** (dev dashboard) |
| Database | `jb_system` + `jb_tenant` (2 DB) | **2 DB Encore:** `system` + `tenant` (schema `t_<slug>` per bisnis) |
| Postgres dev | `infra/docker-compose` | Encore **auto-provision** Postgres sendiri (Docker) |
| Queue AI | BullMQ + `ai-worker/` (Node) | **Pub/Sub Encore** (`ai-jobs` → subscriber di `encore run`) |
| Jalankan | `npm run start:dev` | `encore run` |

---

## Prerequisites

| Tool | Install | Cek |
|------|---------|-----|
| Go 1.24+ | `brew install go` | `go version` |
| Encore CLI | `brew install encoredev/tap/encore` | `encore version` |
| Docker Desktop | [docker.com](https://www.docker.com/) | `docker info` |

Redis untuk session (disarankan sama dengan stack lama):

```bash
cd ../infra && docker compose up -d redis
```

---

## Setup pertama kali (urutan wajib)

### 1) Login Encore (sekali per mesin)

Secrets **local** tetap disimpan lewat Encore Cloud — CLI harus login dulu:

```bash
encore auth login
```

Tanpa browser / SSH: buat Auth Key di https://app.encore.cloud/settings/access/auth-keys lalu:

```bash
encore auth login --auth-key=<KEY_ANDA>
```

### 2) Daftarkan app ke Encore Cloud (sekali per repo)

Error `app_not_found` = app di `encore.app` belum terdaftar di akun Encore kamu.

**Penting:** `encore app init` **gagal** jika file `encore.app` sudah ada (`an encore.app file already exists`). Untuk repo ini, pakai trik berikut:

```bash
cd api-go
encore auth login

# Simpan id lama, daftarkan ke cloud (nama bebas, Encore bisa tambah suffix)
mv encore.app encore.app.bak
encore app init wabantu-viko
# Contoh hasil: id "wabantu-viko-8vni" — cek output CLI

# Pastikan encore.app punya lang go (init kadang hanya menulis id)
# { "id": "wabantu-viko-8vni", "lang": "go" }

rm encore.app.bak
```

Dashboard app: https://app.encore.cloud/ (lihat id di output `encore app init`).

Sudah punya app di cloud? Link saja:

```bash
encore app link <app-id-di-cloud> -f
```

### 3) Set secrets dari `api/.env`

Mapping nama secret (field di kode Go → nilai dari Nest `.env`):

| Encore secret | Sumber `api/.env` | Wajib untuk dev? |
|---------------|-------------------|------------------|
| `JWTSecret` | `JWT_ACCESS_SECRET` | Ya |
| `DataEncryptionKey` | `DATA_ENCRYPTION_KEY` | Ya |
| `RedisURL` | `redis://localhost:6379` | Ya |
| `AnthropicApiKey` | `ANTHROPIC_API_KEY` | Untuk AI |
| `AnthropicAPIKey` | sama (nama lain di service `business`) | Untuk import website |
| `AiInternalToken` | `AI_INTERNAL_TOKEN` | Untuk worker AI |
| `WebhookVerifyToken` | `META_WEBHOOK_VERIFY_TOKEN` | Untuk webhook Meta |
| *(per channel)* `meta_app_secret` di DB | disimpan saat OAuth WhatsApp connect | Verifikasi webhook signature |
| `MidtransServerKey` | env Midtrans | Payment |
| `MidtransClientKey` | env Midtrans | Payment |
| `MidtransIsProduction` | `false` / `true` | Payment |
| `RajaOngkirAPIKey` | env RajaOngkir | Shipping |
| `RajaOngkirAccountType` | `starter` | Shipping |
| `SentryDSN` | `SENTRY_DSN` | Opsional |
| `PlatformAdminBootstrapSecret` | *(buat manual, min. 32 karakter)* | **Hanya** untuk membuat akun `super_admin` internal (bukan password login) — lihat [Bagian 8.1](./DEVELOPER_DOCUMENTATION.md#81-platform-admin-internal-operator-wabantu-owner) |

Set manual (contoh):

```bash
cd api-go

printf '%s' 'ISI_DARI_JWT_ACCESS_SECRET' | encore secret set --type local JWTSecret
printf '%s' 'ISI_DARI_DATA_ENCRYPTION_KEY' | encore secret set --type local DataEncryptionKey
printf '%s' 'redis://localhost:6379' | encore secret set --type local RedisURL
printf '%s' 'ISI_ANTHROPIC_API_KEY' | encore secret set --type local AnthropicApiKey
printf '%s' 'ISI_ANTHROPIC_API_KEY' | encore secret set --type local AnthropicAPIKey
printf '%s' 'change-me-long-random-secret' | encore secret set --type local AiInternalToken
```

Atau impor otomatis setelah login:

```bash
cd api-go
./scripts/setup-secrets-from-env.sh
```

Cek secret terdaftar:

```bash
encore secret list
```

### 4) Jalankan API

```bash
cd api-go
encore run
```

Yang terjadi otomatis:

- Compile semua **service** (folder `auth/`, `inbox/`, `ai/`, …)
- Buat/start **Postgres lokal** (Encore) untuk DB `system` + `tenant`
- Jalankan migrasi SQL di `system/migrations/` + `tenant/migrations/`
- Serve HTTP di **`http://localhost:4000`** dengan prefix **`/api/v1`** (sama seperti Nest)
- Buka dev dashboard di **`http://localhost:9400`** (API Explorer, trace, diagram arsitektur)

**Redis tidak** di-provision Encore — wajib sudah jalan (`infra` atau Redis lokal) dan `RedisURL` secret mengarah ke instance itu.

### 5) AI auto-reply

**Tidak perlu** menjalankan `ai-worker-go` untuk development api-go. Cukup `encore run` — subscriber Pub/Sub `ai-jobs` / `ai-auto-reply` ikut jalan di dalam proses Encore (`ai/inbound_jobs.go`).

---

## Perintah Encore yang sering dipakai

```bash
# Dev server + infra lokal
encore run

# Cek error schema/API tanpa full run
encore check

# Unit test Encore
# Pakai Encore runtime karena beberapa package mendeklarasikan sqldb/cache/pubsub.
encore test ./...
# Atau paket tertentu:
encore test ./ai ./usage

# Shell ke database system / tenant
encore db shell system
encore db shell tenant
encore db shell tenant --write    # INSERT/UPDATE

# Connection string Postgres lokal
encore db conn-uri system
encore db conn-uri tenant

# Proxy DB ke mesin lain (staging)
encore db proxy tenant --env=staging

# Secret
encore secret set --type local NamaSecret
encore secret list
encore secret delete --type local NamaSecret

# Build image production
encore build docker wabantu

# Bantuan
encore help
encore help db
encore help secret
```

---

## Struktur project (service map)

Setiap folder di bawah ini adalah **satu Encore service** (package Go terpisah):

```
api-go/
  encore.app                 # id app: "wabantu"
  shared/                    # crypto, errors, types, tenant DB helper
  auth/                      # register, login, logout, me, JWT, Redis session
  system/                    # DB control-plane (jb_system) + migrasi
  tenant/                    # DB tenant data (jb_tenant) + provisioning schema t_*
  branch/                    # multi-cabang (Pro)
  workflow/                  # rule-based automation
  middleware/                # global rate limit
  business/                  # profil bisnis, import website, katalog CRUD + import gambar (vision)
  docs/CATALOG_IMAGE_IMPORT.md  # import screenshot → Haiku → konfirmasi → business_catalog_item
  docs/UNIT_ECONOMICS_AND_PRICING.md  # biaya Meta + Anthropic, margin paket, rekomendasi harga
  docs/META_WHATSAPP_MESSAGING_AND_BILLING.md  # CSW 24 jam, template, skenario inbox, beda tagihan Meta vs kuota WABantu
  docs/FINANCE_MODULE.md        # modul keuangan — endpoint, schema, arsitektur saldo, approval, cron
  kb/                        # knowledge base FAQ
  whatsapp/                  # library Meta Cloud API (kirim pesan, Graph)
  whatsappapi/               # REST OAuth + channels (/api/v1/whatsapp/*)
  webhook/                   # GET/POST webhook Meta (+ alias legacy)
  finance/                   # modul keuangan (wallet, transaksi, anggaran, investasi, recurring, checklist, laporan)
  broadcast/                 # broadcast WA (plan Business+)
  inbox/                     # conversations, messages, contacts
  ai/                        # auto-reply, order flow, catalog_reply, vision (import gambar)
  leads/                     # lead pipeline
  billing/                   # subscription overview, invoices, AI quota top-up
  payment/                   # Midtrans QRIS + webhook
  order/                     # pesanan ringan
  shipping/                  # RajaOngkir
  analytics/                 # dashboard metrics
  usage/                     # metering, quota, cron reset
  audit/                     # audit log
  admin/                     # super admin, impersonation
  flag/                      # feature flags
  importcsv/                 # import CSV/XLSX
  scripts/
    setup-secrets-from-env.sh
```

---

## Multi-tenant & database

Selaras NestJS (`api/.env` → `SYSTEM_DB_*` / `TENANT_DB_*`):

| Nest | Encore `api-go` | Isi |
|------|-----------------|-----|
| `jb_system` | DB resource **`system`** | `tenant`, `tenant_account`, `tenant_company`, `audit_log`, `feature_flag`, `payment_webhook_map`, … |
| `jb_tenant` | DB resource **`tenant`** | Schema per bisnis **`t_<slug>`** (inbox, katalog, order, …) |

```bash
encore db conn-uri system   # ≈ jb_system
encore db conn-uri tenant   # ≈ jb_tenant
```

Saat **register**, `auth` menulis ke **`system`**, lalu `tenant.RunTenantDDL` membuat schema di DB **`tenant`**.

Akses data tenant di kode:

```go
conn, err := appdb.TenantConn(ctx, tenant.DataDB.Stdlib(), user.TenantSchema)
```

**Penting:** Postgres dari `encore run` **bukan** otomatis DB di `infra/postgres` untuk Nest. Migrasi data lama = manual.

## Batasan & kuota (dokumentasi lengkap)

**→ [LIMITS_AND_QUOTAS.md](./LIMITS_AND_QUOTAS.md)** — rate limit HTTP, kuota trial/Starter/Business/Pro, entitlement, checkout QRIS, top-up AI, routing AI.

Ringkas:

| Topik | Nilai |
|-------|--------|
| Rate limit global | **400** req/menit / IP |
| Auth login/register | **20** req/menit / IP |
| Trial | Semua fitur aktif; kuota bulanan ketat (mis. AI 60 conv, 100k token, broadcast 20 kontak) |
| Katalog UI | `starter`, `business`, `pro` (tanpa duplikat `basic`) |
| Checkout | `select-plan` → invoice `pending` → QRIS → webhook `paid` → subscription aktif |
| AI top-up | `topup_ai_20000` / `topup_ai_30000` → invoice `pending` → QRIS → `quota_topup` bulan berjalan |
- Encore tidak punya throttler bawaan seperti Nest `@nestjs/throttler`; pola ini = **Redis + middleware** (best practice untuk multi-instance).

## Import CSV/XLSX (staging)

- **Preview** (`POST /import/preview`): parse file → simpan **semua baris** di **Redis** (`import:staging:<jobId>`, TTL 24 jam) → response `jobId` + sample 5 baris.
- **Execute** (`POST /import/execute`): kirim `jobId` + `columnMapping` → worker Pub/Sub `file-import`.
- **Production berikutnya:** ganti staging Redis dengan **S3/R2** (seperti Jubelio) — interface tetap preview → execute by `jobId`.

## Platform admin (operator internal WABantu)

Akun **`super_admin` tanpa toko** — untuk tim WABantu memantau tenant klien. **Bukan** untuk UMKM; klien **tidak bisa** jadi super admin lewat Register.

**Panduan lengkap:** [DEVELOPER_DOCUMENTATION.md Bagian 8.1](./DEVELOPER_DOCUMENTATION.md#81-platform-admin-internal-operator-wabantu-owner)

### Quick start (lokal)

```bash
# 1. Secret bootstrap (sekali, min. 32 karakter — BUKAN password login)
# encore run memakai --type LOCAL, bukan dev — lihat DEVELOPER_DOCUMENTATION Bagian 4.1
printf '%s' 'wabantu-internal-bootstrap-2026-sangat-rahasia' | encore secret set --type local PlatformAdminBootstrapSecret

# 2. API jalan
encore run

# 3. Buat akun internal (sekali per email)
curl -X POST http://localhost:4000/api/v1/internal/platform-admin/bootstrap \
  -H "Content-Type: application/json" \
  -H "X-Platform-Bootstrap-Secret: SECRET_ANDA_DARI_LANGKAH_1" \
  -d '{"email":"owner@wabantu.internal","password":"PasswordMinimal10","name":"Nama Anda"}'

# 4. Login di web-frontend dengan email + password di atas → /dashboard/admin → Pantau tenant
```

| Endpoint | Fungsi |
|----------|--------|
| `POST /api/v1/internal/platform-admin/bootstrap` | Buat akun platform admin (header `X-Platform-Bootstrap-Secret`) |
| `GET /api/v1/admin/tenants?q=&page=&pageSize=` | Daftar tenant dengan search + pagination |
| `PUT /api/v1/admin/tenant/:id/plan` | Override paket tenant (`starter`, `business`, `pro`) |
| `DELETE /api/v1/admin/tenant/:id?confirmSchemaName=...` | Hapus tenant permanen: `DROP SCHEMA ... CASCADE` + soft-delete metadata (wajib konfirmasi schema) |
| `POST /api/v1/admin/impersonate/:tenantId` | Pantau tenant (update session Redis) |
| `POST /api/v1/admin/stop-impersonation` | Keluar dari mode pantau |
| `POST /api/v1/admin/migrate-tenant-schemas` | Patch DDL semua tenant (termasuk tabel `fin_*`) — super_admin |

### Curl / Postman examples

Gunakan ini untuk testing lokal atau Postman. Ganti `BASE_URL`, `ACCESS_TOKEN`, `TENANT_ID`, dan `SCHEMA_NAME` sesuai environment.

```bash
export BASE_URL="http://localhost:4000/api/v1"
export BOOTSTRAP_SECRET="wabantu-internal-bootstrap-2026-sangat-rahasia"
export ACCESS_TOKEN="PASTE_ACCESS_TOKEN_DARI_LOGIN"
export TENANT_ID="PASTE_TENANT_ID"
export SCHEMA_NAME="t_nama_schema_tenant"
```

**1. Bootstrap akun platform admin** (sekali per email):

```bash
curl -s -X POST "$BASE_URL/internal/platform-admin/bootstrap" \
  -H "Content-Type: application/json" \
  -H "X-Platform-Bootstrap-Secret: $BOOTSTRAP_SECRET" \
  -d '{
    "email": "owner@wabantu.internal",
    "password": "PasswordMinimal10Karakter",
    "name": "Viko Owner"
  }'
```

**2. Login platform admin** (ambil `data.accessToken` dari response):

```bash
curl -s -X POST "$BASE_URL/auth/login" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "owner@wabantu.internal",
    "password": "PasswordMinimal10Karakter"
  }'
```

Jika ada `jq`, token bisa disimpan langsung:

```bash
export ACCESS_TOKEN="$(curl -s -X POST "$BASE_URL/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"email":"owner@wabantu.internal","password":"PasswordMinimal10Karakter"}' \
  | jq -r '.data.accessToken // .accessToken')"
```

**3. List tenant** (search + pagination):

```bash
curl -s "$BASE_URL/admin/tenants?q=&page=1&pageSize=10" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

**4. Detail tenant**:

```bash
curl -s "$BASE_URL/admin/tenant/$TENANT_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

**5. Ubah paket tenant** (`starter`, `business`, `pro`):

```bash
curl -s -X PUT "$BASE_URL/admin/tenant/$TENANT_ID/plan" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"planCode":"pro"}'
```

**6. Hapus tenant permanen** (destructive: `DROP SCHEMA ... CASCADE`):

```bash
curl -s -X DELETE "$BASE_URL/admin/tenant/$TENANT_ID?confirmSchemaName=$SCHEMA_NAME" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

`confirmSchemaName` wajib sama persis dengan schema tenant, misalnya `t_agency_properti_jg`.

**7. Pantau / impersonate tenant**:

```bash
curl -s -X POST "$BASE_URL/admin/impersonate/$TENANT_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

Setelah impersonate, session Redis berubah ke tenant tersebut. Client cukup panggil ulang `/auth/me`; tidak ada token impersonation terpisah.

**8. Stop impersonation**:

```bash
curl -s -X POST "$BASE_URL/admin/stop-impersonation" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

**9. Migrasi schema semua tenant**:

```bash
curl -s -X POST "$BASE_URL/admin/migrate-tenant-schemas" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

**Migrasi schema tenant lama** (setelah deploy modul baru, mis. Finance):

```bash
encore run   # terminal 1
encore exec ./cmd/migrate-tenant-schemas   # terminal 2 — bukan `encore call`
```

Atau dari web: login super_admin → **Konsol Platform** → tombol **Migrasi schema tenant**.  
Internal API (private): `POST /api/v1/internal/tenant/migrate-schemas` — hanya dari service Encore lain, bukan curl publik.

Migrasi DB: `system/migrations/4_platform_admin.up.sql` (`tenant_id` nullable untuk `super_admin`).

> **Catatan:** Pola lama `superadmin@gmail.com` saat register **sudah tidak dipakai**. Akun lama yang masih punya tenant dummy bisa tetap login; untuk pola tanpa toko, buat akun baru via bootstrap di atas.

---

## Autentikasi

- Login/register: endpoint **raw HTTP** di `auth/` — response JSON berisi `accessToken` (+ cookie `wabantu_at` opsional).
- **Frontend saat ini:** `Authorization: Bearer <token>` dari `sessionStorage` (lihat `web-frontend/lib/auth/session.ts`).
- Endpoint bertag `//encore:api auth` membutuhkan Bearer **atau** cookie **atau** query `access_token` (SSE).
- Session disimpan di **Redis** (`RedisURL` secret); JWT 15 menit; state impersonation platform admin juga di Redis.

Handler auth global: `auth.AuthHandler` (`//encore:authhandler`) → `buildAuthUser` (tenant efektif saat impersonate).

---

## AI auto-reply (ringkas)

1. `POST /webhook/whatsapp` (atau `/whatsapp/webhook/meta`) → simpan pesan masuk.
2. `ai.PublishInboundJob` → topic Pub/Sub **`ai-jobs`**.
3. Subscriber **`ai-auto-reply`** (`ai/inbound_jobs.go`) di proses yang sama dengan API:
   - Retry hingga **4×** (Encore `RetryPolicy` + penghitung Redis per `inboundMessageId`)
   - Setelah gagal terus → **fallback** (`FallbackAutoReplyJob`, setara `ai-worker` Node)
4. Pipeline di `ai/autoreply.go` (orchestrator) + `order_flow.go`, `greeting.go`, `product_scope.go`, `safety.go` — order Redis state, scope/classifier, lalu Anthropic + kirim WhatsApp.
5. **Hybrid AI routing** (`ai/classifier_routing.go`, `ai/routing.go`): per plan — `starter` = Haiku only; **`trial` + business** = hybrid; `pro` = hybrid priority. FAQ match tinggi bisa bypass LLM. Lihat [LIMITS_AND_QUOTAS.md bagian 2.3](./LIMITS_AND_QUOTAS.md#23-routing-ai-airoutinggo).
6. Log aktivitas AI per tenant: `GET /api/v1/usage/ai-activity` (owner).

`ai-worker-go/` hanya untuk eksperimen Asynq terpisah — **bukan** jalur default api-go. Lihat `../ai-worker-go/README.md`.

Detail lengkap: [APP_FLOW_GUIDE.md](./APP_FLOW_GUIDE.md#91-alur-ai-auto-reply).

---

## Build production

```bash
encore build docker wabantu
```

Deploy via Encore Cloud atau image Docker + Postgres/Redis managed sendiri.

Stack compose contoh: `../infra/docker-compose.yml` (service `api-go`).

---

## Troubleshooting

| Gejala | Penyebab | Solusi |
|--------|----------|--------|
| `not logged in: run encore auth login` | Belum login saat `secret set` | `encore auth login` |
| `app_not_found` | App belum didaftarkan ke Encore Cloud | `encore app init` (di folder `api-go`) |
| `fetch secrets ... failed` | Secrets belum di-set | `setup-secrets-from-env.sh` atau set manual |
| `encore run` parse error query `*string` | Sudah diperbaiki di repo | `git pull` / pastikan inbox/kb/leads pakai `string` bukan pointer di query GET |
| AI 401 dari worker | Token tidak sama | Samakan `AiInternalToken` dan `AI_INTERNAL_TOKEN` worker |
| Frontend tidak connect | `api-go` belum jalan atau env salah | Pastikan `encore run`; `web-frontend` `API_BACKEND_URL=http://localhost:4000` |
| `getServerUser` selalu null | `API_URL_INTERNAL` tanpa `/api/v1` di versi lama | Pakai `lib/env.ts` terbaru (auto-append `/api/v1`) atau set `http://localhost:4000` |
| Inbox tidak live-update | SSE lewat rewrite gagal | Set `NEXT_PUBLIC_SSE_API_URL=http://localhost:4000` di `web-frontend/.env` |
| `middleware` compile error | Versi Encore lama | Update Encore CLI; atau hapus `middleware/` dan andalkan limit di `auth` saja |
| DB kosong setelah register | Normal di DB Encore baru | Register tenant baru; jangan expect data Nest lama |
| `invalid bootstrap secret` | Header curl ≠ `PlatformAdminBootstrapSecret` | `encore secret set` ulang; samakan string di curl |
| Login super admin OK, inbox 403 | Belum impersonate tenant | Admin → **Pantau** tenant (atau dropdown topbar) |

---

## File penting untuk dibaca

| File | Isi |
|------|-----|
| [DEVELOPER_DOCUMENTATION.md](./DEVELOPER_DOCUMENTATION.md) | Dokumentasi teknis lengkap + [Bagian 8.1 Platform Admin](./DEVELOPER_DOCUMENTATION.md#81-platform-admin-internal-operator-wabantu-owner) |
| [APP_FLOW_GUIDE.md](./APP_FLOW_GUIDE.md) | Alur end-to-end, peta endpoint, perintah step-by-step |
| `finance/finance.go` | Wallet, kategori, transaksi, approval, period lock, audit, dashboard finance |
| `finance/budget.go` | Anggaran per kategori, spending report, perbandingan bulanan |
| `finance/investment.go` | Aset investasi, harga manual, portfolio summary (P&L) |
| `finance/recurring.go` | Transaksi berulang + cron scheduler harian (07:00 WIB) |
| `finance/checklist.go` | Checklist keuangan harian + template |
| `finance/report.go` | Export laporan async (CSV/PDF job) |
| `tenant/finance_seed.go` | Seed kategori default + wallet Kas Tunai saat tenant baru daftar |
| `auth/platform_bootstrap.go` | Buat akun `super_admin` internal |
| `auth/impersonation.go` | Pantau / stop impersonation (Redis session) |
| `auth/auth.go` | Register, login, JWT, `AuthHandler` |
| `tenant/tenant.go` | Provisioning schema tenant |
| `webhook/webhook.go` | Ingest WhatsApp + enqueue AI |
| `ai/inbound_jobs.go` | Pub/Sub `ai-jobs`, retry + fallback |
| `ai/autoreply.go` | Orchestrator pipeline balasan AI |
| `ai/order_flow.go` | State machine order + checkout follow-up |
| `usage/ai_activity.go` | Log aktivitas model/path per tenant |
| `inbox/inbox.go` | Inbox REST |
| `../api/.env` | Sumber nilai secret (jangan commit) |

---

## Relasi repo

```
WABantu/
  infra/           Postgres + Redis (shared, untuk Nest / worker)
  api/             NestJS (referensi, port 3001, /api/v1)
  api-go/          Encore (ini, port 4000)
  ai-worker/       Node BullMQ → Nest internal API (hanya untuk stack `api/`)
  ai-worker-go/    Opsional; tidak dipakai oleh webhook api-go (lihat README-nya)
  web-frontend/    Next.js (rewrite `/api/v1` → api-go :4000)
```
