# AI Security & Privacy — Self-Healing Triage

## Data leaving the server

| Destination | What is sent | What is redacted |
|-------------|--------------|------------------|
| Cursor Composer (`composer-2.5`) | Sanitized behavior contract JSON, allowlisted file paths | Phone, email, bank/account numbers, `WB-` order refs, UUIDs (`shared/pii.SanitizeForExternalAI`) |
| GitHub Actions | Job UUID, tenant schema, generated test source | Raw `triage_job.json` is **not** uploaded as an artifact |
| Anthropic Haiku (judge) | Turn text for scoring | Existing judge pipeline; not used as Composer instructions |

Composer runs with `settingSources: []`, `sandboxOptions.enabled=true`, `idempotencyKey=jobID`. No MCP, no DB credentials, no production probe.

## Retention

- Incident + evidence snapshot lives in the **system** DB (survives tenant chat purge of 90 days).
- Tenant `web_chat_message` / session purge must not delete `ai_triage_incident`.
- GHA artifacts store regression logs only, not customer payloads.

## Authz

- All `/api/v1/admin/ai-triage/incidents*` and repair endpoints: `tag:super_admin` + `requireSuperAdmin`.
- Tenant schema, order IDs, and operations are **server-derived** from the incident. Clients send incident/plan IDs only.
- Internal GHA callbacks use `X-Ai-Internal-Token`. Stale callbacks cannot overwrite a newer job status.

## Repair

- Allowlisted operations only. No arbitrary SQL.
- Draft+unpaid only. QRIS pending/paid, Redis cart, session `order_state`, unknown variants: blocked.
- Apply is a second click after approve. `audit.RecordAudit` is required (not best-effort `audit.Log`).
