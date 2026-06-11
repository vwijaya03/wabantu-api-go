# Redis untuk Deploy Encore Cloud

Panduan setup Redis **eksternal** yang dibutuhkan `api-go` saat deploy ke Encore Cloud.

Deploy utama: [DEPLOY_ENCORE_CLOUD.md](./DEPLOY_ENCORE_CLOUD.md).

---

## Kenapa Redis tidak ikut Encore Cloud?

Encore Cloud **otomatis** menyediakan:

| Infrastruktur | Disediakan Encore? |
|---------------|-------------------|
| PostgreSQL (`system`, `tenant`) | ✅ Ya |
| Pub/Sub (AI jobs, import CSV) | ✅ Ya |
| Encore Cache (`cache.NewCluster`) | ✅ Ya (khusus AI session tracking) |
| **Redis untuk session app** (`RedisURL`) | ❌ **Tidak** |

`api-go` menyimpan secret **`RedisURL`** yang mengarah ke instance Redis **di luar** Encore. Tanpa ini, deploy bisa sukses tapi **login gagal**.

### Apa yang pakai `RedisURL`?

| Fitur | Package |
|-------|---------|
| Session login / logout | `auth/session.go` |
| Impersonation platform admin | `auth/impersonation.go` |
| Rate limit | `shared/ratelimit`, `middleware` |
| SSE inbox (live update) | `shared/inboxrealtime` |
| Staging import gambar (pasien/staf) | `events/image_import.go` |
| State retry AI | `ai/autoreply.go` |

---

## Lokal vs cloud

| Lingkungan | Redis ada di mana? | `RedisURL` |
|------------|-------------------|------------|
| **Lokal** (`encore run`) | Docker: `../infra/docker-compose.yml` | `redis://localhost:6379` |
| **Encore Cloud** | Provider cloud (Upstash, dll.) | `rediss://...` (bukan localhost) |

Jalankan Redis lokal:

```bash
cd ../infra && docker compose up -d redis
redis-cli ping   # harus PONG
```

Set secret lokal:

```bash
cd api-go
printf '%s' 'redis://localhost:6379' | encore secret set --type local RedisURL
```

---

## Rekomendasi gratis: Upstash

[Upstash](https://upstash.com) punya free tier yang cukup untuk staging / demo WABantu.

### Langkah buat database

1. Daftar di https://upstash.com
2. **Create Database**
3. Pilih region dekat pengguna (mis. **Singapore** / `ap-southeast-1`)
4. Di halaman database, buka tab **Redis** (bukan REST / `.connect` REST saja)

Upstash memberi **dua jenis** kredensial — jangan tertukar:

| Variabel Upstash | Dipakai `api-go`? | Keterangan |
|------------------|-------------------|------------|
| `UPSTASH_REDIS_REST_URL` + `UPSTASH_REDIS_REST_TOKEN` | ❌ **Tidak** | HTTP REST API (SDK Node `@upstash/redis`) |
| `UPSTASH_REDIS_URL` / **Redis URL** | ✅ **Ya** | TCP + TLS — dipakai `go-redis` lewat secret `RedisURL` |

Yang dibutuhkan WABantu (format TLS):

```
rediss://default:PASSWORD@xxxx.upstash.io:6379
```

> `rediss` = Redis + SSL. Jangan pakai `redis://localhost` di cloud.  
> **Jangan** set `RedisURL` ke `https://....upstash.io` (itu REST, login akan gagal).

`api-go` **tidak perlu diubah** untuk REST — cukup set secret `RedisURL` ke TCP URL di atas.

### Set ke Encore Cloud

Ganti `staging` dengan nama environment kamu:

```bash
cd api-go
printf '%s' 'rediss://default:PASSWORD@HOST.upstash.io:6379' | \
  encore secret set --env=staging RedisURL
```

Atau lewat script (copy semua secret sekaligus):

```bash
REDIS_URL='rediss://default:PASSWORD@HOST.upstash.io:6379' \
  ./scripts/setup-secrets-for-cloud.sh staging
```

Verifikasi:

```bash
encore secret list --env=staging
```

---

## Alternatif provider

| Provider | Gratis? | Catatan |
|----------|---------|---------|
| **Upstash** | ✅ Free tier | Paling mudah, direkomendasikan |
| **Redis Cloud** | Trial 30 hari | Setelah trial berbayar |
| **Render Key Value** | ✅ Tier free | Redis-compatible, perlu URL dari dashboard Render |
| **Railway** | ~$5 credit trial | Bukan free permanen |
| **Oracle Cloud VPS** | ✅ Always free | Install Redis sendiri — lebih ribet |

Satu instance Redis cloud **cukup** untuk satu environment Encore (staging). Production nanti bisa pakai instance terpisah.

---

## Format URL Redis

```
redis://[user]:[password]@[host]:[port]
rediss://[user]:[password]@[host]:[port]   ← dengan TLS (Upstash)
```

Contoh tanpa password (jarang di production):

```
redis://localhost:6379
```

Contoh Upstash:

```
rediss://default:AbCdEf123456@us1-example-12345.upstash.io:6379
```

Kode mem-parse URL lewat `redis.ParseURL` (`auth/session.go`, `ai/api.go`).

---

## Troubleshooting Redis

| Gejala | Penyebab | Solusi |
|--------|----------|--------|
| Login selalu gagal di cloud | `RedisURL` masih `localhost` | Set URL Upstash ke `--env=staging` |
| `auth: invalid REDIS_URL` | Format URL salah | Pastikan `rediss://` untuk Upstash |
| Session hilang setelah redeploy | Normal | User login ulang (session di Redis, bukan DB) |
| Import gambar staging kosong | Redis beda instance / expired | Pastikan `RedisURL` sama antar deploy |
| Lokal OK, cloud error TLS | Pakai `redis://` bukan `rediss://` | Gunakan URL lengkap dari dashboard Upstash |

---

## Setelah migrasi DB

Redis **tidak** ikut saat migrasi Postgres (`migrate-local-db-to-encore.sh`). Semua user harus **login ulang** di environment cloud.

Cache lama (rate limit counter, staging import) juga tidak dipindah — itu aman; data penting ada di Postgres.

Jika login mengembalikan `{"message":"db error"}` (bukan salah password), itu masalah **GRANT Postgres** di cloud, bukan Redis. Jalankan:

```bash
./scripts/fix-cloud-db-grants.sh staging
```

Detail: [DEPLOY_ENCORE_CLOUD.md](./DEPLOY_ENCORE_CLOUD.md) Langkah 7.
