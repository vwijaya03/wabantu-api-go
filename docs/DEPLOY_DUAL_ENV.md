# Deploy dua environment: Cloud Run (lomba) dan Encore Cloud

Panduan agar setiap hotfix/fix/feat aman di **dua target deploy** yang berjalan dari kode yang sama. Tidak ada nilai secret di dokumen ini, hanya nama. Setiap klaim dicek ke repo; yang tidak bisa dicek ditandai **belum terverifikasi di repo**.

Dokumen terkait: [DEPLOY_ENCORE_CLOUD.md](./DEPLOY_ENCORE_CLOUD.md), [DEPLOY_REDIS.md](./DEPLOY_REDIS.md), [LLM_FIRST_PIPELINE.md](./LLM_FIRST_PIPELINE.md), `deploy/cloudrun/README.md`.

---

## 1. Target

| | Cloud Run ("env lomba") | Encore Cloud |
|---|---|---|
| Branch | `release-cr` | `master` |
| Cara deploy | `scripts/cloudrun/build-image.sh` lalu `scripts/cloudrun/deploy.sh` (manual, dari mesin yang punya `gcloud`) | `git push encore ...:master` (lihat `docs/DEPLOY_ENCORE_CLOUD.md`); juga otomatis lewat `.github/workflows/deploy-encore-staging.yml` |
| Nama service / region | `SERVICE_NAME` (default `wabantu-api`), `GCP_REGION` (default `asia-southeast2`) di `deploy.sh` | App ID `wabantu-viko-8vni` (`encore.app`) |
| Infra | Neon (Postgres), Upstash (Redis), Pub/Sub GCP prefix `lomba-` (`deploy/cloudrun/README.md`, `render_infra_config.py`) | Dikelola Encore (Postgres, Pub/Sub, cache) + Redis eksternal lewat `RedisURL` (`DEPLOY_ENCORE_CLOUD.md`) |

Cloud Run deploy dari `release-cr` lewat `scripts/cloudrun/deploy.sh`. Image tag default di skrip adalah `release-cr` (`IMAGE_TAG`).

Encore Cloud deploy dari `master`. Workflow `deploy-encore-staging.yml` berjalan setelah workflow "AI Regression" selesai (`workflow_run`) dan menjalankan `git push encore HEAD:master`. Isi kondisi lengkapnya (apakah hanya saat sukses) **belum diverifikasi** di dokumen ini; baca file workflow-nya.

> **PERINGATAN: remote `encore` = deploy.**
> Repo lokal punya dua remote (dicek dengan `git remote -v`, hanya host yang dicatat):
>
> | Remote | Host | Fungsi |
> |---|---|---|
> | `origin` | `github-wabantu` (alias SSH ke GitHub, repo `vwijaya03/wabantu-api-go`) | PR dan review |
> | `encore` | `encore://wabantu-viko-8vni` | **push ke sini langsung men-deploy ke Encore Cloud** |
>
> Jangan `git push encore` dari branch eksperimen. PR selalu ke `origin`. Kirim ke `encore` hanya `master` yang sudah di-merge dan lulus CI.

Catatan kondisi saat dokumen ini ditulis: tree `origin/master` dan `origin/release-cr` identik (hash tree sama; `git diff --stat origin/master origin/release-cr` kosong), termasuk berkas `scripts/cloudrun/*` dan `deploy/cloudrun/*` yang ada di kedua branch. Bedanya hanya di riwayat commit (merge twin). Tabel berkas khusus-deploy yang berbeda antar branch: **belum terverifikasi di repo** (saat ini tidak ada perbedaan). Selalu jalankan ulang diff di bagian 5.

---

## 2. Pemetaan env/secret per target

### 2.1 Secret Encore (`var secrets struct`)

Sumber: `grep -rn "var secrets struct" --include='*.go'`.

| Paket / file | Nama secret |
|---|---|
| `webhook/webhook.go` | `WebhookVerifyToken`, `DataEncryptionKey` |
| `order/pii_search.go` | `DataEncryptionKey` |
| `inbox/contact_store.go` | `DataEncryptionKey` |
| `leads/pii_store.go` | `DataEncryptionKey` |
| `broadcast/pii_store.go` | `DataEncryptionKey` |
| `whatsappapi/whatsappapi.go` | `DataEncryptionKey` |
| `payment/payment.go` | `MidtransServerKey`, `MidtransClientKey`, `MidtransIsProduction` |
| `auth/auth.go` | `JWTSecret`, `DataEncryptionKey`, `RedisURL`, `PlatformAdminBootstrapKey` |
| `business/business.go` | `AnthropicAPIKey` |
| `admin/ai_triage_github.go` | `GitHubActionsToken`, `AiTriageInternalToken` |
| `shared/retrieval/config.go` | `OpenAIApiKey`, `PineconeApiKey`, `PineconeIndexHost`, `RetrievalBudgetMs` |
| `shared/mediastorage/s3.go` | `AWSS3Bucket`, `AWSS3Region`, `AWSS3AccessKeyID`, `AWSS3SecretAccessKey` |
| `shared/sentry/sentry.go` | `SentryDSN` |
| `shipping/shipping.go` | `RajaOngkirAPIKey`, `RajaOngkirAccountType` |
| `ai/api.go` | `AnthropicAPIKey`, `RedisURL`, `AiInternalToken`, `DataEncryptionKey` |
| `finance/transaction_image.go` | `AnthropicAPIKey`, `DataEncryptionKey` |
| `events/helpers.go` | `DataEncryptionKey`, `AnthropicAPIKey` |

Daftar unik: 23 nama, sama persis dengan `ENCORE_SECRETS` di `scripts/cloudrun/render_infra_config.py` dan daftar "Secret Encore" di `deploy/cloudrun/README.md`. Cek ulang tiap kali ada `var secrets struct` baru (bagian 5).

### 2.2 Sumber nilai per target

| Kategori | Encore Cloud | Cloud Run |
|---|---|---|
| 23 secret Encore di atas | `encore secret set --env=<ENV> <Nama>` (`DEPLOY_ENCORE_CLOUD.md`) | Secret Manager dengan nama sama, di-map ke env oleh `deploy.sh` (`--set-secrets=Nama=Nama:latest`); `render_infra_config.py` menulis `"secrets": {nama: {"$env": nama}}` ke `infra.config.json` yang ter-bake ke image |
| Env proses saja (bukan secret Encore) | tidak disetel | ada di array `SECRETS` `deploy.sh`: `ENCORE_SERVICE_AUTH_KEY`, `SQL_PASSWORD`, `REDIS_AUTH`, `GeminiAPIKey`, `SimulatorTenantAllowlist`, `FIRESTORE_PROJECT_ID`, `FIREBASE_SERVICE_ACCOUNT_JSON` |
| Env polos di `deploy.sh` (`--set-env-vars`) | tidak disetel | `INLINE_INBOUND_AI=1`, `RETRIEVAL_BACKGROUND_DRAIN=1` |
| Host/DB yang ter-bake ke image (bukan secret) | n/a | dari `deploy/cloudrun/.env` (gitignored; contoh di `deploy/cloudrun/env.example`): `GCP_PROJECT`, `GCP_REGION`, `AR_REPO`, `SERVICE_NAME`, `RUNTIME_SA`, `SQL_HOST`, `SQL_USER`, `SQL_SYSTEM_DB`, `SQL_TENANT_DB`, `REDIS_HOST`, `CLOUD_RUN_URL` |

`Dockerfile` sengaja gagal (`exit 1`); image Cloud Run hanya dari `encore build docker` lewat `scripts/cloudrun/build-image.sh`. Dockerfile tidak menyuntik env apa pun.

### 2.3 Env proses saja: dibaca di mana

| Nama | Dibaca di | Target penyedia |
|---|---|---|
| `GeminiAPIKey` | `ai/llm_gemini.go:21` (`os.Getenv(geminiKeyEnv)`, dipakai di `:34`); `shared/retrieval/gemini_embedder.go:49`; dipakai `shared/retrieval/config.go:34` dan `admin/ai_embedding_reindex.go:78` | hanya Cloud Run (ada di `SECRETS` `deploy.sh`) |
| `SimulatorTenantAllowlist` | `webhook/simulator.go:241` dengan nama dari `internal/simulator/guard.go:12` | Cloud Run (`SECRETS`); di Encore Cloud: nilai staging disebut di `docs-development-shipped/2026-10-14_141300_simulator-firebase-checklist.md` tetapi mekanisme penyetelannya **belum terverifikasi di repo** |
| `FIRESTORE_PROJECT_ID`, `FIREBASE_SERVICE_ACCOUNT_JSON` | `internal/realtime/config.go:26-27`; kosong berarti mirror no-op dan endpoint token 503 (komentar `config.go`, `auth/firebase_token.go`) | Cloud Run (`SECRETS`); Encore Cloud **belum terverifikasi di repo** |
| `SQL_PASSWORD`, `REDIS_AUTH`, `ENCORE_SERVICE_AUTH_KEY` | tidak dibaca kode Go; direferensikan sebagai `{"$env": ...}` di `scripts/cloudrun/render_infra_config.py` (runtime Encore membaca `infra.config.json`) | hanya Cloud Run; Encore Cloud memakai infra terkelola |
| `INLINE_INBOUND_AI` | `webhook/inbound_ai_enqueue.go:15` | hanya Cloud Run |
| `RETRIEVAL_BACKGROUND_DRAIN` | `kb/retrieval_drain_background.go:35` (nama di `:16`) | hanya Cloud Run |

Env lain di kode non-test: `ENCORE_DEPLOY_ID`, `ENCORE_GIT_COMMIT` (`shared/buildinfo/buildinfo.go`), konfigurasi realtime (`internal/realtime/config.go`), dan `WABANTU_AI_INTEGRATION` (`ai/test_integration.go`, tes saja).

### 2.4 Database, Redis, Pinecone, Gemini

| Topik | Cloud Run | Encore Cloud |
|---|---|---|
| Postgres | Neon: database `system` dan `tenant` (default nama dari `SQL_SYSTEM_DB`/`SQL_TENANT_DB`), host `SQL_HOST`, user `SQL_USER`, password env `SQL_PASSWORD` (`render_infra_config.py`, `deploy/cloudrun/README.md`) | SQL terkelola Encore (`DEPLOY_ENCORE_CLOUD.md`: "PostgreSQL system + tenant otomatis Encore") |
| Redis | Upstash. Cluster cache `ai-sessions` lewat `REDIS_HOST` + `REDIS_AUTH` (prefix `lomba:`), ditambah secret `RedisURL` berisi URL penuh `rediss://...` untuk session (`render_infra_config.py`, `deploy/cloudrun/README.md`) | Redis eksternal lewat secret `RedisURL`; cache internal Encore (`DEPLOY_REDIS.md`, `DEPLOY_ENCORE_CLOUD.md`) |
| Pinecone | Kode hanya membaca secret `PineconeApiKey` dan `PineconeIndexHost` (`shared/retrieval/config.go`). Index dibedakan lewat nilai `PineconeIndexHost`. Aturan "satu index terpisah per env" adalah kebijakan: tidak ada pemeriksaan di repo, **belum terverifikasi di repo** | sama |
| Gemini | `GeminiAPIKey` ada: provider chat dan vision terdaftar (`ai/llm_gemini.go`), embedding Gemini ikut terdaftar (`shared/retrieval/config.go:34`) | tidak ada key, jadi `ensureGeminiRegistered` mencatat "GeminiAPIKey not set ... chains fall back to anthropic" dan tidak mendaftarkan Gemini. Chain jatuh ke Anthropic (link tanpa model memakai Haiku, `docs/LLM_FIRST_PIPELINE.md` bagian 3 dan 7). Embedding Gemini tidak terdaftar di sana, dan reindex ke model Gemini ditolak bila key kosong (`admin/ai_embedding_reindex.go:78`) |

---

## 3. Insiden yang diketahui dan penyebabnya

| Gejala | Penyebab | Perbaikan (hanya Cloud Run) | Dibaca di |
|---|---|---|---|
| Webhook WhatsApp 200 tetapi AI tidak membalas | Push Pub/Sub ke `ai-auto-reply` gagal validasi JWT | `INLINE_INBOUND_AI=1`: handler AI dipanggil di proses yang sama, tanpa Pub/Sub | `webhook/inbound_ai_enqueue.go:15`; disetel di `deploy.sh` |
| Indexing RAG tidak jalan | Push Pub/Sub ke `retrieval-index-worker` gagal JWT yang sama | `RETRIEVAL_BACKGROUND_DRAIN=1`: outbox dikuras di goroutine terpisah setelah respons (per tenant, batas 10 menit) | `kb/retrieval_drain_background.go:35`; disetel di `deploy.sh` |
| Ganti model embedding ke Gemini gagal | `GeminiAPIKey` tidak ada di proses | isi `GeminiAPIKey` (Secret Manager di Cloud Run) | `admin/ai_embedding_reindex.go:78` |

Untuk mencari di log (Cloud Run), gunakan string `unable to validate JWT` pada permintaan `POST .../__encore/pubsub/push/<subscription>` (dari `deploy/cloudrun/README.md`). Itu hanya kata kunci pencarian, bukan perintah. Perbaikan akar JWT (audience / `CLOUD_RUN_URL` benar saat build) dicatat di README sebagai track terpisah dan belum dikerjakan di repo ini.

Encore Cloud tidak memakai kedua env itu dan tetap lewat topik `ai-jobs` dan subscription `retrieval-index-worker` (`deploy/cloudrun/README.md`).

---

## 4. Aturan untuk setiap hotfix/fix/feat

1. **Buka pasangan PR.** Satu ke `master`, satu twin `-cr` yang dibranch dari `origin/release-cr` dan menargetkan `release-cr`. Isi kode harus identik. Saat ini tree kedua branch identik, jadi tidak ada berkas yang boleh berbeda kecuali Anda sengaja mengubah berkas deploy (`scripts/cloudrun/*`, `deploy/cloudrun/*`). Buktikan dengan diff di bagian 5.
2. **Env/secret baru harus ditambah di semua tempat**:
   - kode: `var secrets struct` (secret Encore) atau `os.Getenv` (env proses);
   - Encore Cloud: `encore secret set --env=<ENV> <Nama>` (hanya untuk secret Encore);
   - Cloud Run: nama di array `SECRETS` `scripts/cloudrun/deploy.sh` + buat secret di Secret Manager dengan nama sama;
   - Cloud Run, secret Encore baru: tambah juga ke `ENCORE_SECRETS` di `scripts/cloudrun/render_infra_config.py` dan daftar di `deploy/cloudrun/README.md`;
   - env polos Cloud Run: tambah ke `--set-env-vars` di `deploy.sh`;
   - dokumentasi: dokumen ini dan `docs/LLM_FIRST_PIPELINE.md` bagian 7 bila relevan.
3. **Migrasi jalan di kedua target.** Migrasi ada di `system/migrations/` dan `tenant/migrations/` (format `N_nama.up.sql`); nomor terakhir di branch ini adalah `system/migrations/29_gemini_default_model.up.sql` dan `tenant/migrations/25_order_stock_applied_at.up.sql`. Seed harus idempotent: contoh yang ada memakai `ON CONFLICT (id) DO NOTHING` (`system/migrations/26_ai_payment_ocr_seed.up.sql`). Jangan menulis migrasi yang hanya benar untuk satu database; Neon dan SQL Encore keduanya menjalankannya.
4. **Feature flag ada di DB system masing-masing env.** Flag yang ada di satu env tidak ada di env lain. Flag LLM-first (`ai_llm_first`, `ai_llm_first_shadow`, `ai_llm_first_kill`, lihat `docs/LLM_FIRST_PIPELINE.md` bagian 2) harus dibuat di tiap env. Migrasi seed nomor 30 yang membuat baris `ai_llm_first*` ada di PR #255/#256 dan **belum ada di branch ini** (`ls system/migrations` berakhir di 29; `grep ai_llm_first system/migrations` kosong). Sampai PR itu masuk, buat flag lewat endpoint pada bagian 2 dokumen pipeline.
5. **Setelah deploy, verifikasi:**
   - AI membalas: kirim WA uji atau pakai simulator (tenant harus ada di `SimulatorTenantAllowlist`, `webhook/simulator.go:241`);
   - status indexing RAG: `GET /api/v1/flags/retrieval-indexing/:tenantId` (`deploy/cloudrun/README.md`);
   - log Cloud Run tidak berisi `unable to validate JWT` setelah deploy (atau, jika ada, `INLINE_INBOUND_AI` dan `RETRIEVAL_BACKGROUND_DRAIN` masih terpasang di revision aktif).

---

## 5. Checklist (copy-paste)

```bash
# 1) Bandingkan tree kedua target. Kosong = identik.
git fetch origin
git diff --stat origin/master origin/release-cr
git rev-parse origin/master^{tree} origin/release-cr^{tree}

# 2) Env/secret baru di diff PR Anda (ganti HEAD bila perlu)
git diff origin/master...HEAD | grep -nE '^\+.*(os\.Getenv|os\.LookupEnv|secrets\.)'
git diff origin/master...HEAD | grep -nE '^\+.*var secrets struct'

# 3) Daftar semua pembacaan env dan secret di kode (nama saja)
grep -rn 'os.Getenv\|os.LookupEnv' --include='*.go' . | grep -v _test.go | grep -v '^./scripts/cmd'
grep -rn 'var secrets struct' --include='*.go' .

# 4) Nama env yang disetel deploy.sh (tanpa nilai)
sed -n '/^SECRETS=(/,/^)/p' scripts/cloudrun/deploy.sh
grep -n 'set-env-vars' scripts/cloudrun/deploy.sh
sed -n '/^ENCORE_SECRETS = \[/,/^\]/p' scripts/cloudrun/render_infra_config.py

# 5) Secret Manager punya semua nama yang diminta deploy.sh? (hanya nama, tanpa nilai)
gcloud secrets list --project=<PROJECT> --format='value(name)' | sort

# 6) Migrasi baru dan seed idempotent
git diff --name-only origin/master...HEAD -- 'system/migrations' 'tenant/migrations'
grep -L 'ON CONFLICT' $(git diff --name-only origin/master...HEAD -- 'system/migrations/*seed*' 'tenant/migrations/*seed*' 2>/dev/null)

# 7) Remote yang dipakai (jangan salah push)
git remote -v | sed -E 's#(://)[^@/]*@#\1#'
```

Jangan membuka atau mencetak `deploy/cloudrun/.env` atau `.env` apa pun.

---

## 6. Rollback

### Cloud Run

`deploy.sh` memakai variabel `GCP_PROJECT`, `GCP_REGION`, `SERVICE_NAME`. Rollback = arahkan trafik ke revision sebelumnya. Perintah di bawah memakai CLI `gcloud` standar; **belum terverifikasi di repo** (tidak ada skrip rollback), jadi cek dulu dengan `describe`.

```bash
gcloud run revisions list --service=<SERVICE> --region=<REGION> --project=<PROJECT>
gcloud run services update-traffic <SERVICE> \
  --region=<REGION> --project=<PROJECT> \
  --to-revisions=<REVISION_SEBELUMNYA>=100
```

Catatan: revision lama membawa env/secret dan `infra.config.json` miliknya. Subscription Pub/Sub `lomba-*` diarahkan ke URL service oleh `deploy.sh`; URL service tidak berubah antar revision, jadi tidak perlu diubah saat rollback trafik.

### Encore Cloud

Deploy lewat git (`DEPLOY_ENCORE_CLOUD.md`). Opsi: push ulang commit sebelumnya ke remote `encore` (revert di `master` lalu push, jangan force-push tanpa persetujuan), atau rollback lewat dashboard https://app.encore.cloud/. Langkah rollback dashboard secara spesifik **tidak didokumentasikan di `DEPLOY_ENCORE_CLOUD.md`** (grep "rollback" kosong), jadi belum terverifikasi di repo.

```bash
git revert -m 1 <MERGE_COMMIT_SHA>   # di master
git push encore master               # men-deploy ke Encore Cloud
```

### Kill switch fitur (kedua target)

Untuk fitur `ai_llm_first`, matikan lewat flag tanpa deploy: lihat [LLM_FIRST_PIPELINE.md bagian 4](./LLM_FIRST_PIPELINE.md#4-kill-switch-dan-rollback). Efek berlaku maksimal 60 detik (TTL cache flag).
