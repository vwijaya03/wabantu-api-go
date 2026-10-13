# API — AI config (superadmin) & Chat simulator

Status: **config = FINAL & implemented** (`admin/ai_config.go`). **Simulator = FINAL & implemented** (`webhook/simulator.go`, validation in `internal/simulator`).
Base: same backend origin as other `/api/v1` endpoints, session cookie/bearer as usual. Error body is Encore's standard: `{"code": "<errs code>", "message": "...", "details": null}`.

| Encore code | HTTP |
|---|---|
| `invalid_argument` | 400 |
| `unauthenticated` | 401 |
| `permission_denied` | 403 |
| `not_found` | 404 |
| `resource_exhausted` | 429 |
| `failed_precondition` | 400 |
| `internal` / `unavailable` | 500 / 503 |

---

## 1. AI function config (superadmin only)

All endpoints: `auth` + `tag:super_admin`. The server checks `role == "super_admin"` from the **auth session** (never from params) → `403 permission_denied` otherwise. Every write is audit-logged (`audit_log`, actor = session account) and logged with the actor.
Changes apply immediately on the instance that handled the request and within **≤30 s** on other instances (resolver TTL; no Pub/Sub).

Functions: `chat`, `intent`, `ocr`, `payment_ocr`, `vision`, `memory`, `embedding`.
Allowed providers per function: `embedding` → `openai` only; all others → `anthropic`, `gemini`. **OpenAI has no chat adapter** (catalog marks its models `embedding_only`).
`model: ""` = provider default (Anthropic: caller's legacy default, e.g. Haiku 4.5; Gemini: `gemini-2.5-flash`).

### Types
```ts
type ChainLink = { provider: "anthropic" | "gemini" | "openai"; model: string; timeout_ms?: number /* 0..60000 */ };
type FunctionConfig = { function: string; tenantId: string | null; chain: ChainLink[] /* 1..3 */; maxRetries: number /* 0..3, not applied yet */ };
```
Validation (server, `400 invalid_argument` with message): unknown function; chain empty or > 3 links; provider not allowed for the function; **provider+model checked together against the allowlist** (e.g. `gemini` + `claude-…` rejected; model id must match `^[a-z0-9][a-z0-9.-]{0,63}$` because it is placed in the Gemini URL path); duplicate link; `timeout_ms` out of range; `maxRetries` out of range.
`tenantId`: must be a UUID of an **existing** tenant (`400` if malformed, `404 not_found` if unknown). Superadmin may target any tenant.

### GET `/api/v1/admin/ai/function-config?tenantId=<uuid optional>`
Returns all global rows (`tenantId: null`) plus the overrides of `tenantId` if given. Functions without a row use the code default (Anthropic) — not listed.
```json
{ "configs": [
  { "function": "payment_ocr", "tenantId": null,
    "chain": [ {"provider":"gemini","model":"gemini-2.5-flash","timeout_ms":8000}, {"provider":"anthropic","model":""} ],
    "maxRetries": 1 },
  { "function": "payment_ocr", "tenantId": "0b6c…", "chain": [ {"provider":"anthropic","model":"claude-haiku-4-5"} ], "maxRetries": 1 }
] }
```

### PUT `/api/v1/admin/ai/function-config/:function`
Body: `{ "tenantId": string | null, "chain": ChainLink[], "maxRetries": number }` — `null` = global default.
Response `200`: the saved `FunctionConfig`.

### DELETE `/api/v1/admin/ai/function-config/:function?tenantId=<uuid required>`
Removes the **tenant override** only (global rows can't be deleted here → use PUT). Response: `{ "deleted": true|false }` (`false` = no override existed).

### GET `/api/v1/admin/ai/catalog`
```json
{ "providers": [ {"id":"anthropic","enabled":true,"available":true},
                 {"id":"gemini","enabled":true,"available":false},
                 {"id":"openai","enabled":true,"available":false} ],
  "models": [ {"provider":"gemini","id":"gemini-2.5-flash","label":"Gemini 2.5 Flash","status":"supported","vision":true},
              {"provider":"openai","id":"text-embedding-3-small","label":"OpenAI text-embedding-3-small","status":"embedding_only","vision":false} ] }
```
- `enabled`: `ai_providers.enabled` (no row = enabled). Disabled providers are skipped in chains.
- `available`: credentials present at runtime (Gemini key set, Anthropic key set). An unavailable link is skipped at runtime and the chain falls back (logged once). OpenAI availability is not reported yet (always `false`).
- `status`: `supported` | `embedding_only`.

Cost policy (defaults seeded by migrations 26/27): `payment_ocr`, `intent`, `memory` → Gemini 2.5 Flash, fallback Anthropic (Haiku). `chat` not seeded: legacy Haiku/Sonnet routing (Sonnet only for complex chat / tool-use). Actual provider/model is logged per call and stored in usage (`model`, `tier` = family alias).

---

## 2. Chat simulator for judges (WhatsApp-style)

Same pipeline as the WhatsApp webhook (inbound → AI auto-reply / order flow / payment-proof OCR), but **nothing is sent to Meta**: outbound messages are stored only.
Auth: `auth`; roles `owner`, `staff`, or `super_admin` while acting as a tenant. **Tenant = session tenant** (no tenantId in the request). The buyer can only send text or an image — there are no fields for price, order status, totals or tier; prices always come from catalog/tier tables and order status only changes through the normal order flow / owner approval.
Rate limit: 20 messages / minute / tenant → `429 resource_exhausted`.

### POST `/api/v1/simulator/messages`
```ts
// request
{ sessionId: string;        // client-generated buyer id, ^[A-Za-z0-9_-]{6,64}$; same id = same buyer/conversation
  buyerName?: string;       // ≤ 60 chars, default "Pembeli Demo"
  text?: string;            // ≤ 1000 chars
  image?: { base64: string; mimeType: "image/jpeg" | "image/png" | "image/webp" } // decoded ≤ 2 MB (payment proof / product photo)
}
// exactly one of text | image (image may carry text as caption via `text`)
// response 202
{ conversationId: string; messageId: string; status: "queued" }
```
Errors: `400 invalid_argument` (bad sessionId, empty message, text too long, bad mime, image too large/invalid base64), `401`, `403` (no tenant in session / role not allowed), `429`.

### GET `/api/v1/simulator/messages?sessionId=<id>&after=<RFC3339 optional>`
Poll for the AI reply (recommended every 1–2 s while waiting; replies normally arrive in 2–10 s).
```ts
{ conversationId: string | null;   // null = no message sent yet for this session
  messages: Array<{
    id: string;
    direction: "inbound" | "outbound";
    author: string;                  // "contact" | "ai" | "system" | "agent"
    type: "text" | "image";
    text: string;                    // image: caption or "[gambar]"
    createdAt: string;               // RFC3339
    status: string;                  // outbound: "sent" | "pending" | "failed"
  }> }
```
Order status / payment-proof result (badge "cocok / perlu dicek", owner approve) use the existing order endpoints for the session tenant; the simulator does not expose order mutation.
Errors: `400` (bad sessionId/after), `401`, `403`.
