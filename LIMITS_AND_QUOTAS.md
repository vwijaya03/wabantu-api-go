# Batasan sistem — WABantu (api-go)

Dokumen **sumber kebenaran** untuk rate limit HTTP, kuota pemakaian bulanan, entitlement fitur, billing/checkout, dan routing AI per paket.

**Kode terkait:**

| Area | File |
|------|------|
| Kuota bulanan | `usage/usage.go` → `planQuotas` |
| Plan katalog & trial display | `billing/billing.go` → `PlanCatalog`, `trialLimits` |
| Fitur on/off per plan | `shared/entitlement/entitlement.go` |
| Rate limit HTTP | `middleware/ratelimit.go`, `shared/ratelimit/ratelimit.go`, `auth/auth.go` |
| Checkout & invoice | `billing/billing.go` → `SelectPlan`, `ActivatePaidInvoice` |
| Pembayaran QRIS | `payment/payment.go` + webhook → `billing.ActivatePaidInvoice` |
| Routing model AI | `ai/routing.go`, `ai/autoreply.go` → `loadSubscriptionPlanCode` |

Frontend: `web-frontend/hooks/use-plan.ts`, halaman `/dashboard/billing`, `lib/api/rate-limit.ts` (pesan HTTP 429).

---

## 1. Rate limit HTTP (Redis, per IP)

| Scope | Key Redis | Batas | Window |
|-------|-----------|-------|--------|
| Semua API (global middleware) | `rl:api:<ip>` | **400** request | 1 menit |
| Login / register | `rl:auth:<ip>` | **20** request | 1 menit |
| Platform admin bootstrap | `rl:platform-bootstrap:<ip>` | **5** request | 1 menit |

Konstanta: `shared/ratelimit.DefaultPublicRPM = 400`, `AuthRPM = 20`.

Respons kelebihan: HTTP **429**, kode `resource_exhausted`, pesan `too many requests — coba lagi dalam satu menit`.

**Catatan frontend:** `DashboardAuthShell` memanggil `/auth/me` sekali per sesi (bukan tiap navigasi). React Query tidak retry pada 429 (`web-frontend/lib/query/rate-limit.ts`).

---

## 2. Trial vs paket berbayar

### 2.1 Fitur (entitlement)

| Plan efektif | Cara ditentukan | Semua fitur produk? |
|--------------|-----------------|---------------------|
| `trial` | `subscription.is_trial = true` | **Ya** — `entitlement.HasFeature("trial", *)` selalu `true` |
| `starter` | Berbayar, plan_code starter | Tidak — tanpa broadcast, workflow, CRM leads, multi-branch, API |
| `business` | Berbayar, business / alias `basic` | Business tier (broadcast, workflow, CRM, hybrid AI) |
| `pro` | Berbayar, pro | Semua fitur Business + multi-branch + API access |

Trial **bukan** Starter berbayar: menu & API terbuka seperti evaluasi penuh; **kuota** yang membatasi (lihat §3).

### 2.2 Kuota pemakaian bulanan (`usage.planQuotas`)

Periode: kalender **`YYYY-MM`** (`usage_aggregate.period`). Reset agregat: cron `reset-monthly-usage` (rotasi periode; tidak menghapus baris lama).

| `event_type` | Trial | Starter | Business | Pro |
|--------------|-------|---------|----------|-----|
| `ai_conversation` | **60** | 1.500 | 6.000 | 20.000 |
| `ai_token` | **100.000** | 2.000.000 | 8.000.000 | 30.000.000 |
| `broadcast_contact` | **20** | 0¹ | 500 | 10.000 |
| `storage_byte` | **52.428.800** (50 MB) | 268.435.456 (256 MB) | 2.147.483.648 (2 GB) | 10.737.418.240 (10 GB) |
| `admin_seat` | **1** | 1 | 3 | 10 |
| `workflow_exec` | **8** | 50 | 500 | 5.000 |

¹ Starter: `broadcast_contact = 0` → fitur broadcast **nonaktif** di entitlement; kuota 0 = tidak ada kuota terpisah.

**Plan efektif untuk kuota:** `usage.getTenantPlan()` → `"trial"` jika `is_trial`, else `plan_code` (normalisasi `basic` → `business`).

**Harga katalog (IDR/bulan):** Starter 299.000 · Business 799.000 · Pro 1.999.000 · Trial 0.

**Katalog API/UI:** hanya `starter`, `business`, `pro` (`billing.listSellablePlans()`). Alias `basic` tidak ditampilkan di UI.

### 2.3 Routing AI (`ai/routing.go`)

| Plan | Mode | Model (ringkas) |
|------|------|-----------------|
| `starter` | `haiku_only` | Haiku saja |
| `trial` | `hybrid` | Haiku (sederhana) / Sonnet (kompleks) — kuota token ketat |
| `business`, `basic` | `hybrid` | Haiku / Sonnet |
| `pro` | `hybrid_priority` | Sonnet lebih agresif untuk pesan kompleks |

`loadSubscriptionPlanCode` di `ai/autoreply.go` mengembalikan `"trial"` bila `is_trial`.

---

## 3. Billing & invoice

### Alur checkout (benar)

1. Owner memilih paket → `POST /api/v1/billing/select-plan`
2. Backend membuat invoice status **`pending`** — **tidak** mengubah `subscription` / **tidak** menganggap lunas
3. Owner bayar → `POST /api/v1/payment/create-qris` (wajib `invoiceId` pending, nominal harus cocok)
4. Webhook Midtrans `PAID` → `billing.ActivatePaidInvoice`:
   - `subscription`: `plan_code`, `is_trial=false`, `trial_ends_at=NULL`
   - `invoice`: status **`paid`**, `paid_at` diisi

### Status invoice

| Status | Arti | Tampil di riwayat FE |
|--------|------|----------------------|
| `pending` | Menunggu QRIS | Tidak (banner “Menunggu pembayaran”) |
| `paid` | Lunas | Ya |
| `issued` | Data lama (flow sebelum perbaikan) | Ya |
| `void` | Checkout diganti / dibatalkan | Tidak |

`GET /billing/overview` → `invoices` hanya `paid` + `issued`; `pendingCheckout` = invoice `pending` terbaru.

### Trial default saat registrasi

`ensureSubscription`: `plan_code=starter`, `plan_name=Starter`, **`is_trial=true`**, `trial_ends_at` = now + **7 hari**.

---

## 4. Enforcement di API (contoh)

| Fitur | Cek entitlement | Cek kuota |
|-------|-----------------|-----------|
| Broadcast | `FeatureBroadcast` | `broadcast_contact` |
| Workflow | `FeatureWorkflow` | `workflow_exec` |
| CRM / leads | `FeatureCRMLeads` | — |
| Multi cabang | `FeatureMultiBranch` | — |
| AI auto-reply | — | `ai_conversation`, `ai_token` via `CheckAICostLimit` |
| Undang staff | — | `admin_seat` |

Pesan kuota habis: HTTP 403 / fallback pesan AI ramah (lihat `ai/autoreply.go`).

---

## 5. Frontend (`web-frontend`)

`hooks/use-plan.ts`:

- `isTrial === true` → `hasBroadcast`, `hasWorkflow`, `hasMultiBranch`, `hasCRMLeads` = **true**
- Paket berbayar: gate seperti tabel §2.1

Kuota tampilan: `GET /api/v1/usage/summary` → `plan: "trial"` saat trial.

---

## 6. WhatsApp webhook → tenant schema

Meta mengidentifikasi channel lewat **`phone_number_id`** (bukan slug tenant).

| Aturan | Detail |
|--------|--------|
| **Satu nomor Meta = satu tenant** | Tabel `system.whatsapp_inbound_map` (`meta_phone_number_id` PRIMARY KEY) |
| **Saat OAuth connect** | `tenant.RegisterWhatsAppInbound` — tolak jika ID sudah dipakai tenant lain |
| **Saat webhook** | `tenant.ResolveWhatsAppInbound` — lookup map dulu; scan legacy + error jika duplikat |
| **Disconnect channel** | `tenant.UnregisterWhatsAppInbound` |

**Jika 4 schema punya `meta_phone_number_id` sama:** hanya satu yang boleh aktif; tenant lain harus putuskan WA atau pakai nomor / WABA Meta berbeda. Reconnect di tenant yang benar setelah migrasi `5_whatsapp_inbound_map.up.sql`.

---

## 7. Changelog kebijakan (referensi)

| Tanggal | Perubahan |
|---------|-----------|
| 2026-05 | Rate limit global 120 → **400** / menit / IP |
| 2026-05 | Trial: kuota terpisah + **semua fitur** entitlement; tidak disamakan dengan Starter berbayar |
| 2026-05 | Checkout: invoice `pending` → bayar QRIS → `paid` + aktivasi subscription |
| 2026-05 | Katalog UI: hapus duplikat paket `basic` |
| 2026-05 | WhatsApp: `whatsapp_inbound_map` — routing webhook per `meta_phone_number_id` unik |

---

*Terakhir diselaraskan dengan kode di branch kerja aktif. Jika mengubah angka di `usage/usage.go` atau `billing/billing.go`, update tabel di dokumen ini.*
