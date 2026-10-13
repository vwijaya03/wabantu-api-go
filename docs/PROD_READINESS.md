# Kesiapan produksi — PR #216 dan #217

**Status:** belum merge. Checklist pra-produksi, bukan catatan rilis. Env Encore Cloud yang bernama `staging` adalah produksi dan auto-deploy dari `master`. Jangan merge atau deploy dari dokumen ini.

Branch yang dicek terhadap `origin/master` (`0ad6b5a`): keduanya **0 commit di belakang master** dan `git merge-tree` bersih.

| PR | Branch | HEAD saat checklist ditulis |
|----|--------|------------------------------|
| #216 | `be/h1-llm-master` | `18910e3` |
| #217 | `be/admin-auth` | `fa7dca5` |

`#217` **tidak** memuat 5 commit terbaru `#216` (termasuk migrasi `29` dan guard `expectedUpdatedAt`). Keduanya bersih terhadap master, tetapi saling menyimpang.

## Secret baru (nama saja, tanpa nilai)

| Nama | Di mana | Catatan |
|------|---------|---------|
| `PlatformAdminBootstrapKey` | Encore secret, service `auth` | Menggantikan `PlatformAdminBootstrapSecret`. Nilai lama tidak boleh dipakai ulang. Kosong atau kurang dari 32 karakter → bootstrap menolak (403). |
| `GeminiAPIKey` | Env proses, bukan field `secrets` Encore | `ai/llm_gemini.go` membaca `os.Getenv("GeminiAPIKey")`. Cloud Run: Secret Manager → env. Encore Cloud tidak mengisi env ini dari secret. Tanpa key, provider Gemini tidak terdaftar dan rantai jatuh ke Anthropic. |
| `FIRESTORE_PROJECT_ID` | Env | Opsional. Kosong → mirror Firestore no-op, endpoint token 503. |
| `FIREBASE_SERVICE_ACCOUNT_JSON` | Env / Secret Manager | JSON service account. Jangan di repo. Jangan isi dengan project lomba `wabantu` atau project lama `wabantu-17052026`. |

Secret yang sudah dipakai produksi dan tetap diperlukan: `JWTSecret`, `DataEncryptionKey`, `RedisURL`, `AnthropicAPIKey`. `OpenAIApiKey` tetap opsional (RAG lexical jika kosong).

Config provider/model (rantai per fungsi, on/off) dibaca dari tabel `ai_providers` dan `ai_function_config`. Tidak ada API key di kode. Kunci Gemini sengaja di luar struct secret Encore supaya deploy tidak gagal saat key belum di-set — lihat keputusan di bawah.

## Migrasi baru

Urutan Encore pada DB `system` (jalan otomatis saat deploy `master`):

| File | Isi | Risiko data produksi |
|------|-----|----------------------|
| `system/migrations/25_ai_provider_config.up.sql` | Tabel `ai_providers`, `ai_function_config`. Mulai kosong. | Rendah. Tabel baru. Resolver tetap ke default Anthropic kalau tabel kosong. |
| `system/migrations/26_ai_payment_ocr_seed.up.sql` | Perlebar CHECK `function`, seed provider + satu baris global `payment_ocr`. Idempotent. | Rendah. Tidak mengubah baris `chat`. Seed memakai `gemini-2.5-flash` lalu fallback Anthropic. |
| `system/migrations/27_ai_cost_defaults.up.sql` | Seed global `intent` dan `memory` hanya jika baris global belum ada. | Sedang. Tenant yang belum punya baris global akan mencoba Gemini dulu. Tanpa `GeminiAPIKey`, registry melewati Gemini dan memakai Anthropic. Baris yang sudah ada tidak ditimpa. |
| `system/migrations/29_gemini_default_model.up.sql` | Hanya di `#216`. Ubah model `gemini-2.5-flash` → `gemini-3.1-flash-lite` pada baris `updated_by = migration:27` untuk `intent`/`memory`. | Rendah jika 27 baru saja membuat baris itu. Baris yang diedit admin tidak disentuh. Idempotent. |
| `system/migrations/28_auth_login_attempt.up.sql` | Hanya di `#217`. Tabel audit login. | Rendah untuk data bisnis. Menyimpan email, IP, user-agent, hasil. Tidak menyimpan password atau token. Retensi belum ada job pembersih. |

DB `tenant`:

| File | Isi | Risiko |
|------|-----|--------|
| `tenant/migrations/25_order_stock_applied_at.up.sql` | `ALTER TABLE "order" ADD COLUMN IF NOT EXISTS stock_applied_at TIMESTAMPTZ` pada setiap schema `t_*`. | Rendah. Kolom nullable, tidak menulis ulang baris. Order lama tetap NULL. Kode menolak klaim stok jika kolom belum ada (`ColumnExists`). |
| `tenant/schema_patch.go` + `shared/tenantschema/cloud_patch_sql.go` | ALTER yang sama, `IF NOT EXISTS`. | Role aplikasi di Encore Cloud tidak boleh ALTER (`42501`). Kolom di produksi masuk lewat migrasi tenant di atas, atau `scripts/apply-tenant-schema-cloud.sh` dengan role admin. |

## Uji migrasi di salinan (jangan dijalankan dulu)

Tunggu izin Viko. Jangan menjalankan migrasi atau skrip admin ke DB produksi.

1. Buat salinan schema yang sudah dianonimkan (bukan koneksi ke env `staging`).
2. Terapkan migrasi system 25 → 26 → 27 → 29, lalu 28, lalu tenant 25.
3. Cek: baris order lama tidak berubah status/total; `stock_applied_at` NULL; `ai_function_config` yang `updated_by` bukan `migration:27` tidak berubah; login audit tabel ada dan kosong.
4. Baru setelah salinan lulus, putuskan apakah `apply-tenant-schema-cloud.sh staging` perlu dijalankan (hanya jika migrasi tenant 25 tidak ikut deploy).

## Regresi fitur yang sudah di master

Centang di salinan atau di env non-produksi sebelum merge:

- [ ] Login owner dan staf (bukan hanya platform admin)
- [ ] Inbox: daftar percakapan, balas, stream SSE (`/api/v1/inbox/stream?access_token=`)
- [ ] Katalog: list dan edit
- [ ] Order biasa: buat, ubah status, tanpa tombol approve baru
- [ ] Webhook WhatsApp masuk dan balasan AI yang sudah hidup di master
- [ ] Billing / kuota
- [ ] Broadcast

`?access_token=` hanya diterima di path `/api/v1/inbox/stream` (PR #217). Endpoint lain mengabaikan query itu; Bearer dan cookie tetap berlaku.

## Urutan merge (BE dan FE bersamaan)

Jangan merge sampai regresi di atas lulus dan izin Viko untuk migrasi jelas.

1. Backend `#216` (`be/h1-llm-master`) — migrasi AI + kolom stok + approve.
2. Frontend `#97` (`fe/simulator`) — halaman yang memanggil kontrak #216.
3. Gabungkan `be/h1-llm-master` ke `be/admin-auth` (5 commit belum ada di #217), lalu backend `#217`.
4. Gabungkan `fe/simulator` ke `fe/admin-auth` (saling menyimpang: #98 kurang 8 commit #97, #97 kurang 15 commit #98), lalu frontend `#98`.

FE admin-auth memakai `GET /platform-admin/me` dan login admin dari #217. Merge #98 sebelum #217 membuat konsol admin 404.

## Keputusan yang dibutuhkan

- Izin menjalankan uji migrasi pada salinan teranonim, lalu (terpisah) izin menyentuh schema produksi.
- `GeminiAPIKey` di Encore Cloud: tambah sebagai secret wajib, atau biarkan env-only sehingga produksi tetap Anthropic sampai key dipasang.
- Firestore produksi: biarkan kosong (disarankan untuk merge ini) atau project Firebase produksi yang bukan project lomba.
- Nilai baru `PlatformAdminBootstrapKey` dipasang Viko di Encore Cloud. Jangan dicetak dan jangan mengulang nilai secret bootstrap yang lama.
