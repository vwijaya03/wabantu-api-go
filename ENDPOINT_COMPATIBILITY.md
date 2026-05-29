# Perbandingan endpoint: NestJS `api/` vs Encore `api-go/`

Dokumen untuk integrasi **`web-frontend/`** dan onboarding developer. Stack aktif: **api-go** + rewrite Next.

## Ringkasan singkat

| Pertanyaan | Jawaban |
|------------|---------|
| Path sama persis? | **Ya** — keduanya memakai suffix **`/api/v1/...`** di path HTTP. |
| Frontend tanpa ubah path client? | **Ya** — `lib/api/*.ts` tetap `/api/v1/...`; Next rewrite ke port **4000**. |
| Response JSON sama? | **Mayoritas ya** — api-go membungkus `{ success, data }` (`shared/response/envelope.go`); axios FE meng-unwrap. |
| Perlu Nest / ai-worker? | **Tidak** untuk dev FE + api-go. |

**Base URL dev**

| Stack | URL | Catatan |
|-------|-----|---------|
| NestJS (legacy) | `http://localhost:3001` | Jangan dipakai frontend baru |
| Encore (aktif) | `http://localhost:4000` | Semua path `/api/v1/...` |
| Next.js FE | `http://localhost:3000` | Proxy `/api/v1/*` → api-go |

**Konfigurasi frontend (sudah default):**

```env
NEXT_PUBLIC_API_URL=/api/v1
API_BACKEND_URL=http://localhost:4000
API_URL_INTERNAL=http://localhost:4000
```

---

## Legenda kolom

- **Nest** = `GET /api/v1/...`
- **api-go** = `GET /api/v1/...` (path identik di kode Go)
- **FE** = dipakai `web-frontend`
- **Status**: ✅ ada & cocok · ⚠️ ada, kontrak/query beda tipis · ❌ belum ada · ➕ hanya api-go

---

## Auth

| Method | Path | FE | Status |
|--------|------|-----|--------|
| POST | `/api/v1/auth/register` | ✅ | ✅ |
| POST | `/api/v1/auth/login` | ✅ | ✅ |
| POST | `/api/v1/auth/logout` | ✅ | ✅ |
| GET | `/api/v1/auth/me` | ✅ | ✅ |
| GET/POST/DELETE | `/api/v1/team/members` | ✅ team page | ✅ |

Cookie `wabantu_at` + body `accessToken` — selaras Nest.

---

## Business & KB

| Method | Path | FE | Status |
|--------|------|-----|--------|
| GET/PATCH | `/api/v1/business/profile` | ✅ | ✅ |
| POST | `/api/v1/business/profile/import-preview` | — | ➕ |
| CRUD | `/api/v1/knowledge-base` | ✅ | ✅ |

---

## Inbox

| Method | Path | FE | Status |
|--------|------|-----|--------|
| GET | `/api/v1/inbox/unread-summary` | ✅ | ✅ |
| GET (SSE) | `/api/v1/inbox/stream` | ✅ | ✅ |
| GET | `/api/v1/inbox/conversations` | ✅ | ⚠️ query `unreadOnly`/`aiHandled`: string `"true"`/`"false"` |
| GET | `/api/v1/inbox/conversations/:id/messages` | ✅ | ✅ |
| PATCH | `/api/v1/inbox/conversations/:id/read` | ✅ | ✅ |
| POST | `/api/v1/inbox/conversations/:id/handoff` | ✅ | ✅ |
| POST | `/api/v1/inbox/conversations/:id/ai-resume` | ✅ | ✅ |
| POST | `/api/v1/inbox/conversations/:id/messages` | ✅ | ✅ |
| GET/POST | `/api/v1/inbox/contacts?q=&page=&pageSize=` | ✅ | ✅ |
| POST | `/api/v1/auth/reauth` (password + Bearer kedaluwarsa) | ✅ | ✅ |
| GET/PATCH/DELETE | `/api/v1/inbox/contacts/:id` (+ `priceTypeId`) | ✅ | ✅ |
| POST | `/api/v1/inbox/conversations-batch/handoff`, `.../ai-resume` | ✅ | ✅ |
| PATCH | `/api/v1/inbox-contact-status/batch`, `/api/v1/inbox-contacts/batch-delete` | ✅ | ✅ |
| PATCH | `/api/v1/inbox/conversations/:id` | — | ➕ |

---

## WhatsApp

| Method | Path | FE | Status |
|--------|------|-----|--------|
| GET | `/api/v1/whatsapp/channels` | ✅ | ✅ |
| POST | `/api/v1/whatsapp/meta/connect/init` | ✅ | ✅ |
| POST | `/api/v1/whatsapp/meta/connect/callback` | ✅ | ✅ |
| DELETE | `/api/v1/whatsapp/channels/:id` | ✅ | ✅ |
| POST | `/api/v1/whatsapp/channels/:id/test-message` | — | ⚠️ cek api-go |
| GET/POST | `/api/v1/whatsapp/webhook/meta` | — | ✅ |
| GET/POST | `/api/v1/webhook/whatsapp` | — | ✅ (alias) |

REST OAuth: package **`whatsappapi/`** · Graph send: **`whatsapp/`**.

---

## Billing, payment, usage

| Method | Path | FE | Status |
|--------|------|-----|--------|
| GET | `/api/v1/billing/overview` | ✅ | ✅ — `plans`: starter/business/pro; `topUpOptions`; `pendingCheckout`; `invoices`: paid/issued only |
| POST | `/api/v1/billing/select-plan` | ✅ | ✅ — buat invoice **`pending`**; subscription aktif setelah bayar |
| POST | `/api/v1/billing/top-up` | ✅ | ✅ — buat invoice top-up AI **`pending`**; kuota masuk setelah QRIS lunas |
| POST | `/api/v1/payment/create-qris` | ✅ billing | ✅ — `invoiceId` wajib; validasi status `pending` |
| GET | `/api/v1/usage/summary` | ✅ billing | ✅ — `plan`: `trial` jika `is_trial`; limit efektif termasuk `quota_topup` paid bulan berjalan |

Plan codes: `starter`, `business`, `pro` (+ alias `basic` → business, tidak di katalog UI).

**Kuota & rate limit:** [LIMITS_AND_QUOTAS.md](./LIMITS_AND_QUOTAS.md).

---

## Fitur baru (api-go + FE)

| Area | Path contoh | Halaman FE |
|------|-------------|------------|
| Orders | `/api/v1/orders` + batch; harga item dari tipe kontak; `completed` → finance income, `draft`/`cancelled` → hapus income | `/dashboard/orders` — contact, multi-item katalog, `effectiveSellPrice`, batch |
| Price types | `GET/POST/PATCH/DELETE /api/v1/business/price-types` | `/dashboard/catalog/price-types` |
| Catalog | `/api/v1/business/catalog` (+ `contactId`, `prices[]`) + POST/PATCH/DELETE | `/dashboard/catalog` — multi-harga per tipe |
| Catalog image AI | `GET/POST /api/v1/business/catalog/import-image/*` | `/dashboard/catalog/import-image` |
| Transaction image AI | `GET/POST /api/v1/finance/transactions/import-image/*` | `/dashboard/finance/transactions/import-image` |
| Broadcast | `/api/v1/broadcast/...` | `/dashboard/broadcast` |
| Import CSV/XLSX produk | `/api/v1/import/preview`, `/execute` (`targetTable=business_catalog_item`) | `/dashboard/import` — download template CSV/XLSX produk, preview mapping, import ke katalog |
| Branches | `/api/v1/branches` | `/dashboard/branches` |
| Workflow | `GET/POST/PATCH/DELETE /api/v1/workflows` | `/dashboard/workflow` |
| Usage AI activity | `GET /api/v1/usage/ai-activity`, `GET /api/v1/usage/ai-activity/summary` | super_admin saat impersonate tenant |
| Admin AI activity | `GET /api/v1/admin/tenant/:id/ai-activity`, `.../summary` | super_admin — `/dashboard/admin/ai-activity` |
| Admin | `/api/v1/admin/*` | `/dashboard/admin` — tenant search/pagination, impersonate, plan override, delete tenant |
| Shipping | `/api/v1/shipping/*` | (API client ada; UI minimal) |
| Analytics | `/api/v1/analytics/overview` | `/dashboard/analytics` |
| Finance — Dashboard | `GET /api/v1/finance/dashboard` | `/dashboard/finance` |
| Finance — Wallet | `GET/POST/PUT/DELETE /api/v1/finance/wallets` | `/dashboard/finance/wallets` |
| Finance — Category | `GET/POST/DELETE /api/v1/finance/categories` | `/dashboard/finance/transactions` |
| Finance — Transaction | `GET/POST/PUT/DELETE /api/v1/finance/transactions` | `/dashboard/finance/transactions` |
| Finance — Transaction image AI | `GET/POST /api/v1/finance/transactions/import-image/*` | `/dashboard/finance/transactions/import-image` |
| Finance — Duplikat | `POST /api/v1/finance/transactions/duplicate` | `/dashboard/finance/transactions` |
| Finance — Approval | `POST /api/v1/finance/transactions/approve` | `/dashboard/finance/transactions` |
| Finance — Budget | `GET/POST/DELETE /api/v1/finance/budgets` | `/dashboard/finance/budget` |
| Finance — Investasi | `GET /api/v1/finance/investments/portfolio`, `/assets`, `/prices` | `/dashboard/finance/investment` |
| Finance — Recurring | `GET/POST/DELETE /api/v1/finance/recurring` | `/dashboard/finance/recurring` |
| Finance — Checklist / tagihan bulanan | `GET /checklist/monthly`, `POST /checklist/monthly/toggle`, `POST /checklist/clone-from-recurring`, `GET /checklist/templates/manage`, `PATCH /checklist/templates/:id`, plus `/today`, `/templates`, `/action` | `/dashboard/finance/checklist` |
| Finance — Laporan | `POST /api/v1/finance/reports/export`, `GET /reports/jobs/:id` | `/dashboard/finance/reports` |
| Finance — Audit | `GET /api/v1/finance/audit-log` | `/dashboard/finance` (owner) |
| Leads | `/api/v1/leads` | internal CRM capture; `/dashboard/contacts` now uses `/api/v1/inbox/contacts` |

---

## Health

| Method | Path | Status |
|--------|------|--------|
| GET | `/api/v1/health` | ✅ api-go |
| GET | `/api/v1/health/ready` | ✅ api-go |

---

## AI internal

| Method | Path | Pemanggil |
|--------|------|-----------|
| POST | `/api/v1/internal/ai/auto-reply` | Pub/Sub subscriber (default) |
| POST | `/api/v1/internal/ai/auto-reply/fallback` | retry/fallback |

Header: `X-Ai-Internal-Token` = secret `AiInternalToken`.

**Tidak perlu** `ai-worker/` Node saat memakai api-go — job di `encore run`.

---

## Checklist developer baru (gabungan FE + BE)

1. `infra`: `docker compose up -d redis`
2. `api-go`: `encore auth login` → `setup-secrets-from-env.sh` → `encore check` → `encore run`
3. `web-frontend`: `cp .env.example .env.local` → `npm install` → `npm run dev`
4. Buka `http://localhost:3000`, register tenant baru
5. Uji: login, inbox, business profile, billing overview

---

## Kesimpulan

**Frontend saat ini ditargetkan ke api-go.** Path client `/api/v1/...` tidak perlu diubah; cukup pastikan rewrite ke **:4000** dan backend + Redis jalan. Nest `api/` tetap referensi migrasi bisnis, bukan dependency runtime FE.
