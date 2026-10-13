# API — AI config (superadmin) & Chat simulator

Status: **config = FINAL & implemented** (`admin/ai_config.go`). **Simulator = FINAL & implemented** (`webhook/simulator.go`, validation in `internal/simulator`). Golden flow, order page and realtime: §3–§5.
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
Poll for the AI reply (recommended every 1–2 s while waiting; replies normally arrive in 2–10 s). Or subscribe to Firestore (§5).
```ts
{ conversationId: string | null;   // null = no message sent yet for this session
  messages: Array<{
    id: string;
    direction: "inbound" | "outbound";
    author: string;                  // "contact" | "ai" | "system" | "human" (owner manual reply / broadcast)
    type: "text" | "image";
    text: string;                    // image: caption or "[gambar]"
    createdAt: string;               // RFC3339
    status: string;                  // outbound: "sent" | "pending" | "failed"
    imageUrl: string | null;         // image only: "/api/v1/inbox/messages/{id}/media" — GET with the same Bearer token; cached 1 h
  }>;
  pricing: { tier: string; tierLabel: string } | null;   // tier the AI quotes to this buyer: "umum" | "reseller" | "agen" | ...
  order: OrderSummary | null;        // latest order of this conversation
}
type OrderSummary = {
  id: string; orderNumber: string;   // "WB-1A2B3C4D"
  status: "draft"|"confirmed"|"paid"|"processing"|"shipped"|"completed"|"cancelled";
  paymentStatus: "unpaid"|"proof_submitted"|"verified"|"rejected";
  total: number;
  proofCheck?: "cocok" | "perlu_dicek";   // absent = no payment proof processed yet
  proofCheckReasons?: string[];           // e.g. "mismatch_amount","account_mismatch","missing_amount","invalid_date","duplicate_hash","low_confidence","kb_empty","proof_blocked"
  items: { name: string; qty: number; unitPrice: number; unit?: string }[];  // unitPrice = tier price at order time
  updatedAt: string;
}
```
`pricing`/`order` are null when nothing exists yet (query errors are logged as `simulator state query failed part=pricing|order`, not swallowed). Every message sent to a simulator buyer (AI reply, payment-proof result, owner manual reply from the inbox, broadcast) is stored here and **never** sent to Meta; the simulator channel is hidden from `GET /api/v1/whatsapp/channels` and cannot be disconnected/deleted there.
Errors: `400` (bad sessionId/after), `401`, `403`.

---

## 3. Golden flow (judges)

| Step | Endpoint | Auth | Code |
|---|---|---|---|
| Buyer chats, AI quotes **tier price** and creates the order at that price | `POST /api/v1/simulator/messages` → AI pipeline | auth; owner/staff/super_admin (session tenant) | `webhook/simulator.go` (SimulatorSend); tier pricing `ai/order_catalog_db.go` `applyContactTierPrices` (called from `ai/autoreply.go`, `ai/order_flow_handler.go`, `ai/order_amend.go`) |
| Buyer sends transfer proof → OCR (Gemini, fallback Anthropic) → stored in `order.payment_proof_meta`, exposed as `proofCheck` | same POST with `image` | same | `ai/payment_proof.go` (`evaluatePaymentProofRules`, `applyPaymentProofResult`); derivation `internal/proofcheck` |
| Owner approves → proof verified **and** order `completed` + finance income, one transaction | `POST /api/v1/orders/:id/approve` | auth; owner (or super_admin impersonating the tenant) | `order/approve.go` Approve; rules `internal/orderapprove` |

Demo data: `go run ./scripts/cmd/demo-seed/` (DEMO ONLY; see header). Simulator session `reseller-demo` buys at the reseller price.

---

## 4. Order page (Frontend)

All `auth`, tenant from session; mutations require owner (`CanPerformOwnerActions`: owner, or super_admin impersonating).

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/v1/orders?status=&q=&page=&pageSize=` | – | `{ orders: Order[], total, page, pageSize }` |
| GET | `/api/v1/orders/:id` | – | `Order` |
| POST | **`/api/v1/orders/:id/approve`** | – | `Order` — **the "Setujui" button**: verifies the proof and completes the order (`paymentStatus=verified`, `status=completed`, finance income + buyer notification) in one DB transaction. **Stock is NOT part of that transaction**: it is deducted after commit via `orderapprove.ApplyStockOnce` (see "Stock after `/approve`" below). Idempotent: already `completed` → 200 with the order, nothing re-written (no duplicate income). `409 aborted` when there is no payment proof, the order is cancelled, the proof is blocked, or it is still a draft without proof. Another tenant's order → 404. |
| POST | `/api/v1/orders/:id/payment-proof/verify` | – | `Order` (legacy: verify only → `status=processing`; prefer `/approve`) |
| POST | `/api/v1/orders/:id/payment-proof/reject` | `{ reason?: string }` | `Order` (`paymentStatus=rejected`; buyer is told to resend) |
| POST | `/api/v1/orders/:id/payment-proof/unblock` | – | `Order` (after repeated rejections the proof is blocked) |
| PATCH | `/api/v1/orders/:id` | `{ status?: "completed" \| "shipped" \| ..., trackingNumber?, courier?, ... }` | `Order` |
| PATCH | `/api/v1/orders/:id/cancel` | – | `Order` |

`Order` (relevant fields): `id, orderNumber, conversationId, contactId, contactDisplayName, items[{name, qty, unitPrice, sellUnit}], status, paymentStatus, total, paymentProofMeta{amount, bank, accountNumber, accountName, date, confidence, flags[], rejectReason, rejectionCount, proofBlocked}`, **`proofCheck: "cocok"|"perlu_dicek"` (absent = no proof)**, **`proofCheckReasons: string[]`**.
Badge: `proofCheck=="cocok"` → green "Bukti cocok"; `"perlu_dicek"` → amber "Perlu dicek" + reasons. Approval is always the owner's decision; `cocok` is advisory (amount equals order total and destination account matches the payment info in the knowledge base).
Proof image: `GET /api/v1/inbox/messages/{paymentProofMessageId}/media` (Bearer).
Errors: `400` invalid state (e.g. already verified on `/verify`), `403` not owner, `404` order not found (also another tenant's id), `409` approve preconditions (message explains), stock precheck failure as returned by inventory. Stock after `/approve` — runs **after commit** via `orderapprove.ApplyStockOnce` (`order/approve.go`), idempotent, at most once per order. The single transaction covers verify + complete + income + notify only; stock is separate, and a failed stock step is fixed by retrying `/approve`:
- Marker `order.stock_applied_at` (tenant migration `25_order_stock_applied_at`). `/approve` claims it atomically (`UPDATE … SET stock_applied_at=now() WHERE id=$1 AND stock_applied_at IS NULL RETURNING`); only the winner deducts. Repeated or concurrent `/approve` calls do not deduct again.
- If the deduction fails, the claim is released (compare-and-clear on the claimed value) and the endpoint returns the error; the order is already `completed`, and calling `/approve` again applies the stock exactly once.
- Every stock path (`/approve`, `PATCH /orders/:id` status, `/payment-proof/verify`, cancel/delete, AI order flow) goes through `inventory.SyncOrderStock`, which now computes "required − already issued" **under a row lock on the order** in the same transaction as the movements and stamps/clears the marker. So mixing paths (e.g. confirmed by the AI → stock issued, then `/approve`, or a concurrent `PATCH status=completed`) never deducts twice.

---

## 5. Realtime (Firestore mirror) — optional

Postgres is the source of truth. When `FIRESTORE_PROJECT_ID` and `FIREBASE_SERVICE_ACCOUNT_JSON` (service-account key JSON, from Secret Manager — never in the repo) are set, the backend **dual-writes best-effort** (failures are only logged; requests never fail because of Firestore). Unset → no-op. Only simulator conversations are mirrored.

### POST `/api/v1/auth/firebase-token`
Auth: Bearer session (owner, staff, or super_admin acting as a tenant). **Body ignored**; claims come only from the server session.
```ts
// response 200 (Cache-Control: no-store)
{ token: string;      // Firebase custom token → signInWithCustomToken(auth, token)
  tenantId: string;   // = custom claim tenantId
  role: "owner"|"staff"|"super_admin";  // = custom claim role
  expiresAt: string;  // RFC3339, custom token valid 1 h (exchange immediately; Firebase ID token refreshes itself)
  projectId: string }
```
Errors (JSON `{code, message}`): `401 unauthenticated` (no/invalid session), `403 permission_denied` (other role, or super_admin without a selected tenant), `503 unavailable` (Firestore/Firebase not configured).

### Documents (read-only for clients)
| Path | Fields |
|---|---|
| `tenants/{tenantId}/simConversations/{conversationId}` | `conversationId`, `lastMessageAt` (timestamp), `pricing` (`{tier, tierLabel}` \| null), `order` (`OrderSummary` \| null), `updatedAt` |
| `tenants/{tenantId}/simConversations/{conversationId}/messages/{messageId}` | `id`, `direction` (`inbound`\|`outbound`), `author`, `type` (`text`\|`image`), `text`, `status`, `imageUrl` (string \| null), `createdAt` (timestamp) |
| `tenants/{tenantId}/orders/{orderId}` | `conversationId`, `order` (`OrderSummary`), `updatedAt` |

`OrderSummary` = the same shape as in §2 (numbers as integers when whole). Get `conversationId` from the POST response, then listen to `.../messages` ordered by `createdAt`.
Written on: simulator inbound, AI/system replies (incl. payment-proof result), owner manual reply, and owner order actions (approve/verify/reject/unblock/update/cancel), and broadcasts to simulator buyers (job payload carries `tenantId`; jobs queued before that change skip the mirror only).

Rules (`firestore.rules`): read only if `request.auth.token.tenantId == tenantId` and the role is owner/staff/super_admin; `write: false` for all clients (the server writes with the service account).

## Isolasi tenant pada tool LLM (Security L-2)

Tool katalog (`search_catalog`, `get_product`) dijalankan oleh `CatalogToolExecutor` yang dibuat per job dari snapshot katalog tenant milik sesi/job (`ai/autoreply.go`, `NewCatalogToolExecutor(catalog)`). Executor tidak memegang koneksi DB, dan skema tool tidak punya parameter tenant/schema. Field tambahan seperti `tenantId`, `schema`, atau `search_path` di argumen tool diabaikan. Tool yang tidak dikenal ditolak. Loop Gemini dan path Anthropic meneruskan argumen apa adanya ke executor yang sama, dan `RunToolChat` me-resolve chain hanya dari tenantID pemanggil.

Test: `ai/tool_tenant_isolation_test.go` (perlu `encore test`; dengan `go test` biasa paket `ai` panic di init Encore, jadi di box hanya dicek compile + vet) dan `shared/llm/tools_tenant_test.go` (jalan dengan `go test`).
