# Akses staging — API (Postman) & database (TablePlus)

Cara test dan inspect environment **Encore Cloud staging** setelah deploy.

**App ID:** `wabantu-viko-8vni`  
**URL API staging (contoh):** `https://staging-wabantu-viko-8vni.encr.app`

> URL `*.encr.app` = **HTTP API** (Postman, frontend).  
> Database Postgres **tidak** bisa diakses lewat URL itu — pakai Encore CLI (`db shell` / `db proxy`).

Prasyarat: `encore auth login` dan perintah dijalankan dari folder `api-go`.

---

## Perbandingan lokal vs staging

| | Lokal (`encore run`) | Staging (Encore Cloud) |
|---|----------------------|-------------------------|
| **API base URL** | `http://localhost:4000/api/v1` | `https://staging-wabantu-viko-8vni.encr.app/api/v1` |
| **Database** | `encore db shell tenant` | `encore db shell tenant --env=staging` |
| **Session Redis** | Docker lokal | Upstash (`RedisURL` secret) — lihat [DEPLOY_REDIS.md](./DEPLOY_REDIS.md) |

Salin URL pasti dari [Encore Dashboard](https://app.encore.cloud/) → app → environment **staging**.

---

## Test API — Postman / curl

### Environment Postman

Buat environment mis. `WABantu Staging`:

| Variable | Value |
|----------|--------|
| `base_url` | `https://staging-wabantu-viko-8vni.encr.app/api/v1` |
| `access_token` | *(kosong; diisi setelah login)* |

### Login

**POST** `{{base_url}}/auth/login`

Headers: `Content-Type: application/json`

Body (gunakan akun yang ada di DB staging — setelah migrasi, sama dengan lokal):

```json
{
  "email": "owner@contoh.com",
  "password": "PasswordMinimal10Karakter"
}
```

Salin `data.accessToken` dari response → variable `access_token`.

### Request berikutnya

Header:

```
Authorization: Bearer {{access_token}}
```

### Contoh endpoint

| Aksi | Method | Path |
|------|--------|------|
| Login | POST | `/auth/login` |
| Profil | GET | `/auth/me` |
| List tenant (super admin) | GET | `/admin/tenants?page=1&pageSize=10` |
| Logout | POST | `/auth/logout` |

Contoh curl:

```bash
BASE=https://staging-wabantu-viko-8vni.encr.app/api/v1

curl -s -X POST "$BASE/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"email":"...","password":"..."}'

curl -s "$BASE/auth/me" -H "Authorization: Bearer TOKEN"
```

Lebih banyak contoh admin: [README.md — Curl / Postman](../README.md#curl--postman-examples).

### Catatan API

- Path selalu diawali `/api/v1/...`
- Setelah migrasi DB, user harus **login ulang** (session Redis tidak ikut migrasi)
- Login gagal + error Redis → cek [DEPLOY_REDIS.md](./DEPLOY_REDIS.md)
- Response `{"message":"db error"}` setelah migrasi Postgres → jalankan `./scripts/fix-cloud-db-grants.sh staging` ([DEPLOY_ENCORE_CLOUD.md](./DEPLOY_ENCORE_CLOUD.md) Langkah 7)

---

## Database — terminal (psql)

WABantu punya **dua** database Encore:

| DB | Isi |
|----|-----|
| `system` | Tenant, akun, billing, webhook map, audit |
| `tenant` | Data bisnis per schema `t_*` |

```bash
cd api-go

# Baca saja
encore db shell system --env=staging
encore db shell tenant --env=staging

# INSERT / UPDATE / DELETE
encore db shell system --env=staging --write
encore db shell tenant --env=staging --write

# Admin (migrasi, DDL) — jarang dipakai manual
encore db shell tenant --env=staging --admin
```

Contoh SQL di `tenant`:

```sql
SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_';

SET search_path TO t_nama_tenant, public;
SELECT count(*) FROM business_profile;
```

Connection string untuk script:

```bash
encore db conn-uri system --env=staging --write
encore db conn-uri tenant --env=staging --write
```

| Flag `conn-uri` | Hak akses |
|-----------------|-----------|
| (tanpa flag) | Read-only |
| `--write` | Baca + tulis **data** di tabel (bukan owner schema `t_*`) |
| `--admin` | Owner schema `t_*` — `DROP SCHEMA`, migrasi, restore |

Schema `t_*` dimiliki **`db_tenant_admin`** (database owner — role yang dipakai Encore saat deploy dynamic grants).  
Koneksi `--admin` (`encore_admin_...`) bisa `SET ROLE db_tenant_admin` untuk DDL.  
`DROP SCHEMA ... CASCADE` dengan koneksi `--write` → error `must be owner of schema`.

### Hapus tenant / schema `t_*`

**Disarankan — lewat API** (juga bersihkan baris di DB `system`):

```http
DELETE /api/v1/admin/tenant/{tenantId}?confirmSchemaName=t_omah_apparel
Authorization: Bearer <super_admin_token>
```

**Manual SQL di TablePlus** — pakai koneksi **`--admin`**, bukan `--write`:

```bash
encore db proxy tenant --env=staging --admin -p 5433
encore db conn-uri tenant --env=staging --admin   # user = owner schema
```

```sql
DROP SCHEMA IF EXISTS t_omah_apparel CASCADE;
```

Setelah drop schema, hapus metadata di DB `system` (`tenant`, `tenant_company`, dll.) atau tenant akan “yatim” di control-plane.

---

## Database — TablePlus (GUI)

Encore membuka **tunnel lokal** ke Postgres cloud. Terminal proxy harus **tetap jalan** selama TablePlus terhubung.

### Langkah 1 — Tunnel (biarkan terminal ini terbuka)

**Tenant DB** (schema `t_*`, pasien, event, dll.):

```bash
cd api-go
# Baca/tulis data di tabel
encore db proxy tenant --env=staging --write -p 5433

# DDL (DROP SCHEMA, CREATE TABLE, dll.) — ganti --write dengan --admin
# encore db proxy tenant --env=staging --admin -p 5433
```

Output yang diharapkan:

```
dbproxy: listening for TCP connections on localhost:5433
```

**System DB** (metadata tenant) — terminal terpisah:

```bash
encore db proxy system --env=staging --write -p 5434
```

Tanpa `--write` = koneksi read-only.

### Langkah 2 — User & password

Di terminal **baru** (proxy langkah 1 tetap jalan):

```bash
cd api-go
encore db conn-uri tenant --env=staging --write
```

Contoh output:

```
postgresql://encore:PASSWORD@127.0.0.1:52771/tenant?sslmode=disable
```

Ambil **user**, **password**, dan nama **database** (`tenant`).  
Port di URI (`52771`) **abaikan** — pakai port proxy **`5433`**.

### Langkah 3 — TablePlus

1. **Create** → **PostgreSQL**
2. Isi koneksi:

| Field | Tenant | System |
|-------|--------|--------|
| Name | `WABantu Staging Tenant` | `WABantu Staging System` |
| Host | `127.0.0.1` | `127.0.0.1` |
| Port | `5433` | `5434` |
| User | dari `conn-uri` | dari `conn-uri` |
| Password | dari `conn-uri` | dari `conn-uri` |
| Database | `tenant` | `system` |

3. Tab **SSL** → **Disable** (`sslmode=disable`)
4. **Test** → **Connect**

### Alternatif: import URL

1. Jalankan proxy (langkah 1)
2. Salin output `encore db conn-uri ...`
3. TablePlus → **Import from URL** → **ganti port** di URL menjadi `5433` (atau `5434` untuk system)

### Setelah connect — schema tenant

Di sidebar TablePlus buka **Schemas** → `t_omah_apparel`, dll.

Atau SQL:

```sql
\dn t_*
SET search_path TO t_omah_apparel, public;
```

### Troubleshooting TablePlus

| Gejala | Penyebab | Solusi |
|--------|----------|--------|
| Connection refused | Proxy tidak jalan | Jalankan `encore db proxy` lagi |
| Auth failed | Password kedaluwarsa | Ambil ulang `encore db conn-uri` |
| Port salah | Pakai port dari URI, bukan proxy | Host `127.0.0.1`, port **5433** / **5434** |
| Putus tiba-tiba | Terminal proxy ditutup | Buka proxy lagi, reconnect TablePlus |
| `must be owner of schema` | Koneksi `--write`, bukan `--admin` | Proxy + `conn-uri` dengan `--admin`; atau hapus via API admin |
| Login API `db error` | GRANT DB hilang setelah `pg_restore` | `./scripts/fix-cloud-db-grants.sh staging` |

---

## Log & dashboard

```bash
encore logs --env=staging
```

Dashboard deploy & URL: https://app.encore.cloud/

---

## File terkait

| File | Isi |
|------|-----|
| [DEPLOY_ENCORE_CLOUD.md](./DEPLOY_ENCORE_CLOUD.md) | Deploy, migrasi DB, secrets |
| [DEPLOY_REDIS.md](./DEPLOY_REDIS.md) | Redis Upstash (wajib untuk login) |
| [README.md](../README.md) | Contoh curl admin & dev lokal |
