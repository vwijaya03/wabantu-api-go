# RAG hardening + webhook legacy cleanup

## Masalah / Kebutuhan

Setelah implementasi RAG v1 (`feat/rag-vector-retrieval-v1`), code review menemukan risiko produksi:

- Webhook Meta masih punya beberapa path legacy yang membingungkan operasi & Meta Developer Console.
- Indexing worker bisa “sukses palsu” saat secrets Pinecone/OpenAI kosong (mock fallback).
- Rollout massal RAG tidak idempotent; counter outbox bisa race; stagger pakai `time.Sleep` di handler Pub/Sub.
- Metadata Pinecone menyimpan teks FAQ mentah (PII / prompt-injection surface).
- Fallback lexical tidak terobservasi di metrics; frontend superadmin punya bug path API & UX rollout.

## Perubahan

### Webhook — satu path kanonik

| Sebelum (dihapus) | Sekarang (kanonik) |
|-------------------|-------------------|
| `GET/POST /api/v1/whatsapp/webhook/meta` | — |
| `GET/POST /webhook/whatsapp` | — |
| Handler legacy `HandleMetaWebhook`, `HandleMetaWebhookLegacy`, `HandleWhatsAppWebhookLegacy` | — |
| — | **`GET/POST /api/v1/webhook/whatsapp`** → `webhook.HandleWhatsAppWebhook` |

**Migrasi Meta Developer Console:** ubah Callback URL ke `https://<env>.encr.app/api/v1/webhook/whatsapp`. Verify token tetap secret `WebhookVerifyToken`.

### RAG runtime & indexing (api-go)

| ID | Perbaikan |
|----|-----------|
| CR-1 | Worker indexing **tidak** mock-fallback saat `DefaultService() == nil` — return `ErrServiceNotConfigured` (retryable) |
| CR-2 | `retrieval.DefaultService()` singleton via `sync.Once` |
| CR-4 | Handler rollout idempotent: skip item terminal + cek `RowsAffected` |
| CR-5 | `nextOutboxAttempt()` increment atomik dari DB outbox |
| CR-6 | Field `RetrieveKBResult.LexicalFallback` + metrics `retrieval_fallback_total` di `ai/retrieval_bridge.go` |
| CR-7 | Metadata Pinecone: hanya `entry_id` + `content_hash` — **tanpa** raw Q&A |
| CR-8 | Stagger rollout via `NotBefore` saat enqueue (`sequenceIndex * delayMs`); handler tidak `Sleep` |

### Superadmin API & frontend (web-frontend)

| ID | Perbaikan |
|----|-----------|
| CR-3 | `lib/api/flags.ts` — path relatif `/flags/...` (hindari double `/api/v1`) |
| CR-9–11 | Halaman `/dashboard/admin/ai-retrieval`: cache mode, polling kondisional, error state |
| CR-12 | OAuth WhatsApp: clear URL error + reset ref agar retry connect bisa dilakukan |

### Endpoint flag (super_admin)

| Method | Path | Fungsi |
|--------|------|--------|
| GET | `/api/v1/flags/retrieval-mode/:tenantId` | Mode tenant (`disabled` / `shadow` / `vector`) |
| PUT | `/api/v1/flags/retrieval-mode` | Set mode per tenant |
| GET | `/api/v1/flags/retrieval-indexing/:tenantId` | Progress embedding KB + katalog + outbox |
| GET | `/api/v1/flags/retrieval-observability` | Counter/latency retrieval |
| POST | `/api/v1/flags/retrieval-rollout` | Rollout async multi-tenant |
| GET | `/api/v1/flags/retrieval-rollout/jobs/:jobId` | Status job |
| GET | `/api/v1/flags/retrieval-rollout/active-jobs` | Job aktif |
| POST | `/api/v1/flags/retrieval-rollout/jobs/:jobId/cancel` | Batalkan job |

## File utama

**api-go**

- `webhook/webhook.go` — hapus route/handler legacy
- `shared/retrieval/config.go`, `service.go`, `ids.go` — singleton, `LexicalFallback`, metadata hash-only
- `kb/retrieval_worker.go`, `retrieval_outbox.go` — no mock, atomic attempts
- `flag/rag_rollout_jobs.go` — idempotency + `NotBefore` stagger
- `ai/retrieval_bridge.go` — observability fallback

**web-frontend**

- `lib/api/flags.ts`
- `app/(dashboard)/dashboard/admin/ai-retrieval/page.tsx`
- `app/(dashboard)/dashboard/whatsapp/onboarding/page.tsx`

## Testing

```bash
# api-go
cd api-go
encore test ./shared/retrieval/... ./flag/... ./kb/... ./webhook/... ./internal/apiregistry/... ./internal/apitest/...

# web-frontend (Node 20+)
cd web-frontend && npm run lint && npm run build
```

## Catatan deploy

1. **Meta webhook** — pastikan production/staging hanya memakai `/api/v1/webhook/whatsapp`.
2. **Secrets** — `OpenAIApiKey`, `PineconeApiKey`, `PineconeIndexHost` wajib sebelum rollout vector; tanpa secrets indexing akan retry (bukan silent mock).
3. **Rollout** — mulai `shadow` satu tenant via UI superadmin atau API; pantau `retrieval_fallback_total` dan halaman ai-retrieval.
4. **PR:** api-go [#145](https://github.com/vwijaya03/wabantu-api-go/pull/145), web-frontend [#81](https://github.com/vwijaya03/wabantu-web-frontend/pull/81).

**Commit terkait:** `dadc2fb` (webhook), `7328bb8` (CR-1–5), `eeef4f6` (CR-6–8), `eabfa18` + `7227924` (frontend CR-3/9–12).
