# WABantu API (Encore.go)

Backend WABantu ditulis ulang dengan [Encore.go](https://encore.dev). Kode NestJS asli tetap ada di `../api/` sebagai referensi.

Dokumentasi alur lengkap (mirip `api/APP_FLOW_GUIDE.md`): **[APP_FLOW_GUIDE.md](./APP_FLOW_GUIDE.md)**.

**Routing WhatsApp & AI (webhook → deteksi intent → path metadata):** **[docs/WHATSAPP_AI_ROUTING.md](./docs/WHATSAPP_AI_ROUTING.md)** · **Indeks semua docs:** **[docs/README.md](./docs/README.md)**

Perbandingan endpoint vs NestJS / kompatibilitas frontend: **[ENDPOINT_COMPATIBILITY.md](./ENDPOINT_COMPATIBILITY.md)**.

Dokumentasi teknis lengkap (arsitektur, API, DB, **platform admin**): **[DEVELOPER_DOCUMENTATION.md](./DEVELOPER_DOCUMENTATION.md)** — untuk akun operator internal WABantu langsung ke **[Bagian 8.1](./DEVELOPER_DOCUMENTATION.md#81-platform-admin-internal-operator-wabantu-owner)**.

**Fitur yang sudah rilis (bukan roadmap):** **[docs-development-shipped/](./docs-development-shipped/)** — catatan implementasi per PR.

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
| `AnthropicAPIKey` | sama (juga dipakai `business`, `finance`) | Import katalog/transaksi dari gambar, import website |
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

# Unit test Encore (default — dipakai Encore Cloud build)
# Pakai Encore runtime karena beberapa package mendeklarasikan sqldb/cache/pubsub.
encore test ./...
# Atau paket tertentu:
encore test ./ai ./usage
# AI smoke + unit (tanpa suite 3000+ skenario — lebih cepat)
./scripts/run-ai-unit-tests.sh
# Suite berat 1000/2000+ skenario WA — jalankan manual sebelum merge besar AI
./scripts/run-ai-integration-tests.sh
# Lihat docs/WHATSAPP_BUYER_BEHAVIOR_TESTS.md

# Shell ke database system / tenant (lokal)
encore db shell system
encore db shell tenant
encore db shell tenant --write    # INSERT/UPDATE

# Staging cloud — tambah --env=staging (lihat docs/STAGING_ACCESS.md)
encore db shell tenant --env=staging --write
encore db proxy tenant --env=staging --write -p 5433   # TablePlus: 127.0.0.1:5433

# Connection string Postgres
encore db conn-uri system
encore db conn-uri tenant
encore db conn-uri tenant --env=staging --write        # user/password untuk TablePlus

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
  docs/EVENTS_MODULE.md         # modul acara & terapi — roster staf, pasien dari kontak, slot AUTO/MANUAL
  events/                       # event reservation & therapy (Encore package)
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

## Docs Hub integration

`web-frontend` punya menu internal **Platform → Dokumentasi** (`/dashboard/docs`) yang menggabungkan README dan file `.md` dari `web-frontend/` + `api-go/`.

Selama masih monorepo, frontend menjalankan generator dengan default:

```bash
cd ../web-frontend
API_GO_DOCS_ROOT="../api-go" npm run docs:generate
```

Jika `api-go` nanti dipisah repo/server, jangan pindahkan semua file `.md` ke frontend. Generate `docs-index.json` dari repo/service `api-go`, expose sebagai static JSON via CDN/service URL, lalu isi URL tersebut di panel **Sumber Dokumentasi** pada `/dashboard/docs`:

```bash
API_GO_DOCS_INDEX_URL="https://api.example.com/docs-index.json"
```

Frontend akan mengambil remote index lewat `/api/docs/remote-index` dan menggabungkannya dengan dokumentasi lokal. Dengan pola ini, dokumentasi backend tetap dekat dengan kode backend, tapi tetap searchable dari UI.

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

## Sesi login & re-auth

- JWT access token berlaku **60 menit**; sesi Redis tenant **7 hari**.
- Jika token kedaluwarsa tetapi sesi masih ada, frontend menampilkan **modal konfirmasi password** (bukan redirect login penuh).
- `POST /api/v1/auth/reauth` — body `{ "password", "accessToken" }` (**tanpa** header `Authorization`, agar JWT kedaluwarsa tidak ditolak Encore sebelum handler); mengeluarkan token baru untuk sesi Redis yang masih ada.

## Katalog produk & tipe harga

**Tipe harga** (master data tenant):

- `GET/POST/PATCH/DELETE /api/v1/business/price-types` — CRUD label harga (umum, reseller, kustom). Seed sistem: `umum` (default), `reseller`.
- Panduan lengkap: [docs/PRICE_TYPES_AND_CATALOG_PRICING.md](./docs/PRICE_TYPES_AND_CATALOG_PRICING.md)

**Katalog:**

- `GET /api/v1/business/catalog?q=&page=&pageSize=&contactId=` — list produk; response menyertakan `prices[]` per tipe; dengan `contactId` juga `effectiveSellPrice`.
- `POST /api/v1/business/catalog` — tambah produk manual; body boleh `prices[]`. SKU yang pernah di-soft-delete (`source=manual`) di-restore jika dibuat lagi.
- `PATCH /api/v1/business/catalog/:id` — edit nama, deskripsi, harga per tipe, satuan, barcode, status aktif.
- `DELETE /api/v1/business/catalog/:id` — soft delete produk.

Index unik SKU: `(source, external_code) WHERE deleted_at IS NULL` — produk terhapus tidak memblokir SKU yang sama.

UI `/dashboard/catalog` + `/dashboard/catalog/price-types`. List memakai pagination (default 25 item). Jalankan migrasi tenant untuk patch index lama.

## Contacts dan pesanan

Contacts:

- `GET /api/v1/inbox/contacts?q=&page=&pageSize=` — list kontak WhatsApp dengan search + pagination.
- `POST /api/v1/inbox/contacts` — tambah kontak manual (`priceTypeId` opsional).
- `PATCH /api/v1/inbox/contacts/:id` — edit nama, catatan, tag, status, `priceTypeId` (`null` = default tenant).
- `DELETE /api/v1/inbox/contacts/:id` — soft delete kontak.

Pesanan:

- `GET /api/v1/orders?q=&status=&page=&pageSize=` — list pesanan dengan search + filter status + pagination.
- `POST /api/v1/orders` — tambah pesanan manual; harga item katalog di-resolve dari tipe harga kontak (bukan hanya harga dari body).
- `PATCH /api/v1/orders/:id` — edit contact, item, status, pengiriman. Status `completed` → catat pemasukan di Finance (idempoten, `reference_no` = order id). Status `draft` / `cancelled` → soft-delete transaksi pemasukan terkait.
- `DELETE /api/v1/orders/:id` — soft delete pesanan.
- `PATCH /api/v1/order-status/batch` — update status banyak pesanan (termasuk sinkron finance per order).
- `PATCH /api/v1/order-delete/batch` — hapus banyak pesanan sekaligus.

Status operasional pesanan: `draft`, `processing`, `shipped`, `completed`, `cancelled`. Jalankan migrasi schema tenant untuk tenant lama.

AI order flow dari webhook WhatsApp hanya membuat draft pesanan setelah data wajib sesuai sistem lengkap: produk harus match `business_catalog_item`, varian/ukuran atau warna ada, qty valid, nama + nomor penerima ada, serta alamat punya jalan, kota, provinsi, dan kode pos 5 digit. Jika salah satu kurang, AI menyimpan state percakapan dan menanyakan data yang kurang ke customer, bukan langsung mencatat order.

Contacts mendukung status `active` dan `inactive`, termasuk batch update via `PATCH /api/v1/inbox-contact-status/batch` dan batch delete via `PATCH /api/v1/inbox-contacts/batch-delete`.

## Import CSV/XLSX (staging)

- **Preview** (`POST /import/preview`): parse file → simpan **semua baris** di **Redis** (`import:staging:<jobId>`, TTL 24 jam) → response `jobId` + sample 5 baris.
- **Target produk/katalog:** UI mengirim `targetTable: "business_catalog_item"` saat execute, sehingga backend tahu file ini adalah import produk. Kolom produk yang didukung: `external_code` dan `name` wajib; `description`, `sell_price`, `sell_unit`, `is_active`, `barcode` opsional.
- **Execute** (`POST /import/execute`): kirim `jobId`, `targetTable`, dan `columnMapping` → worker Pub/Sub `file-import`; worker menyimpan ke `business_catalog_item` dengan `source='import'`.
- **Template:** UI `/dashboard/import` menyediakan sample CSV dan XLSX lewat route frontend `/api/import/templates/product?format=csv|xlsx`.
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
| `POST /api/v1/admin/migrate-tenant-schemas` | Patch DDL semua tenant + **repair cloud grants** (super_admin) |

### Curl / Postman examples

Gunakan ini untuk testing lokal atau Postman. Ganti `BASE_URL`, `ACCESS_TOKEN`, `TENANT_ID`, dan `SCHEMA_NAME` sesuai environment.

| Environment | `BASE_URL` |
|-------------|------------|
| Lokal | `http://localhost:4000/api/v1` |
| Encore Cloud staging | `https://staging-wabantu-viko-8vni.encr.app/api/v1` |

Panduan staging (Postman + TablePlus): **[docs/STAGING_ACCESS.md](./docs/STAGING_ACCESS.md)**.

```bash
export BASE_URL="http://localhost:4000/api/v1"
# staging: export BASE_URL="https://staging-wabantu-viko-8vni.encr.app/api/v1"
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

**Tutorial deploy Encore Cloud:** [docs/DEPLOY_ENCORE_CLOUD.md](./docs/DEPLOY_ENCORE_CLOUD.md) · Redis cloud (Upstash): [docs/DEPLOY_REDIS.md](./docs/DEPLOY_REDIS.md) · Postman & TablePlus staging: [docs/STAGING_ACCESS.md](./docs/STAGING_ACCESS.md)

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
| Deploy cloud OK, login gagal | `RedisURL` masih `localhost` | [docs/DEPLOY_REDIS.md](./docs/DEPLOY_REDIS.md) — set Upstash ke `--env=staging` |
| Login staging `db error` setelah migrasi DB | GRANT Postgres hilang (`pg_restore --no-privileges`) | `./scripts/fix-cloud-db-grants.sh staging` — [DEPLOY_ENCORE_CLOUD.md](./docs/DEPLOY_ENCORE_CLOUD.md) |
| Deploy cloud gagal: `permission denied for table business_profile` | Schema yatim `t_*` / tabel owned `encore_container_*` memblokir Encore dynamic grants | [Hot-fix 2am](./docs/DEPLOY_ENCORE_CLOUD.md#hot-fix-2am-permission-denied-for-table-business_profile) — diagnose → prune → fix-grants → verify |
| Push ke Encore tidak trigger deploy | Remote `encore` belum ada | `git remote add encore encore://<app-id>` — lihat [DEPLOY_ENCORE_CLOUD.md](./docs/DEPLOY_ENCORE_CLOUD.md) |

---

## File penting untuk dibaca

| File | Isi |
|------|-----|
| [docs/DEPLOY_ENCORE_CLOUD.md](./docs/DEPLOY_ENCORE_CLOUD.md) | Tutorial deploy ke Encore Cloud (secrets, git push, migrasi DB, frontend) + hot-fix dynamic grants |
| [docs/DEPLOY_REDIS.md](./docs/DEPLOY_REDIS.md) | Setup Redis eksternal (Upstash) untuk session di cloud |
| [docs/STAGING_ACCESS.md](./docs/STAGING_ACCESS.md) | Test API (Postman/curl) & koneksi DB staging (TablePlus, `db proxy`) |
| `scripts/diagnose-cloud-db-grants.sh` | Debug owner schema/tabel saat deploy gagal dynamic grants |
| `scripts/prune-orphan-tenant-schemas-cloud.sh` | Hapus schema `t_*` yatim (penyebab `permission denied for table business_profile`) |
| `scripts/fix-cloud-db-grants.sh` | Reassign owner + GRANT setelah migrasi / sebelum redeploy |
| `scripts/verify-cloud-deploy-ready.sh` | Cek DB siap deploy (orphan + owner tabel) sebelum push |
| [DEVELOPER_DOCUMENTATION.md](./DEVELOPER_DOCUMENTATION.md) | Dokumentasi teknis lengkap + [Bagian 8.1 Platform Admin](./DEVELOPER_DOCUMENTATION.md#81-platform-admin-internal-operator-wabantu-owner) |
| [APP_FLOW_GUIDE.md](./APP_FLOW_GUIDE.md) | Alur end-to-end, peta endpoint, perintah step-by-step |
| `finance/finance.go` | Wallet, kategori, transaksi, approval, period lock, audit, dashboard finance |
| `finance/budget.go` | Anggaran per kategori, spending report, perbandingan bulanan |
| `finance/investment.go` | Aset investasi, harga manual, portfolio summary (P&L) |
| `finance/recurring.go` | Transaksi berulang + cron scheduler harian (07:00 WIB) |
| `finance/checklist.go` | Checklist template & harian |
| `finance/checklist_billing.go` | Tagihan bulanan per periode + auto-post transaksi |
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
