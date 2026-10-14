# Cloud Run lomba (`release-cr`)

Branch ini untuk env lomba. `master` tetap jalur Encore Cloud. Jangan jalankan skrip ini ke project atau database produksi Encore.

Image dibuat dengan `encore build docker`, bukan `docker build`. Config infra ter-bake ke image. Secret tidak masuk file ini.

## Yang disiapkan

| Bagian | Lokasi |
|--------|--------|
| Render infra | `scripts/cloudrun/render_infra_config.py` |
| Build image amd64 | `scripts/cloudrun/build-image.sh` |
| Artifact Registry, SA, 14 topik | `scripts/cloudrun/provision-gcp.sh` |
| Deploy, maks 2 instance, push subscription | `scripts/cloudrun/deploy.sh` |
| Daftar topik | `deploy/cloudrun/pubsub.json` |

Ada 14 topik di kode, termasuk `webhook-retry-dlq` yang tidak punya subscriber. Nama GCP-nya `lomba-<nama-encore>`.

Dua cron Encore tidak dipasang: `finance-recurring` (`POST /api/v1/finance/recurring/process`) dan `reset-monthly-usage` (`POST /api/v1/usage/reset`). Keduanya endpoint private.

## Sebelum build

1. Salin `deploy/cloudrun/env.example` ke `deploy/cloudrun/.env`.
2. Isi host Neon (`SQL_HOST`, `SQL_USER`) dan host Upstash (`REDIS_HOST`, bentuk `hostname:6379`, bukan URL `rediss://`).
3. Di Neon, buat database `system` dan `tenant`. User itu harus bisa migrasi (owner).
4. Secret di Secret Manager, nama = nama env. Nilai tidak ditulis di repo.

Secret Encore (string kosong boleh untuk yang opsional): `JWTSecret`, `DataEncryptionKey`, `RedisURL`, `PlatformAdminBootstrapKey`, `WebhookVerifyToken`, `AnthropicAPIKey`, `AiInternalToken`, `GitHubActionsToken`, `AiTriageInternalToken`, `MidtransServerKey`, `MidtransClientKey`, `MidtransIsProduction`, `OpenAIApiKey`, `PineconeApiKey`, `PineconeIndexHost`, `RetrievalBudgetMs`, `RajaOngkirAPIKey`, `RajaOngkirAccountType`, `AWSS3Bucket`, `AWSS3Region`, `AWSS3AccessKeyID`, `AWSS3SecretAccessKey`, `SentryDSN`.

Env proses, bukan secret Encore: `GeminiAPIKey`, `SimulatorTenantAllowlist`, `FIRESTORE_PROJECT_ID`, `FIREBASE_SERVICE_ACCOUNT_JSON`, plus `SQL_PASSWORD`, `REDIS_AUTH` (password Upstash, tanpa `default:`), `ENCORE_SERVICE_AUTH_KEY` (kunci endpoint private).

`RedisURL` tetap URL penuh `rediss://...` untuk session. Cluster cache `ai-sessions` memakai `REDIS_HOST` + `REDIS_AUTH`.

Upload bukti transfer memakai klien S3 AWS di `shared/mediastorage`. Endpoint kustom tidak ada, jadi HMAC GCS belum bisa dipakai. Isi keempat secret `AWSS3*`.

`FIREBASE_SERVICE_ACCOUNT_JSON` harus JSON service account project Firebase lomba (`client_email` + `private_key`). Service account yang menempel di Cloud Run tidak dibaca kode realtime.

## Perintah

```bash
./scripts/cloudrun/provision-gcp.sh
./scripts/cloudrun/build-image.sh
./scripts/cloudrun/deploy.sh
```

Deploy pertama memakai `base_url` `https://pending.invalid` bila `CLOUD_RUN_URL` kosong. Setelah URL Cloud Run tampil, tulis URL itu ke `CLOUD_RUN_URL`, lalu build dan deploy sekali lagi. Push Pub/Sub baru mengarah ke `/__encore/pubsub/push/<subscription>`.

Service: `wabantu-api`, region `asia-southeast2`, `--max-instances=2`, port 8080, 1 CPU, 1Gi.

CORS credential untuk `*.hosted.app` ada di `encore.app` pada branch ini.
