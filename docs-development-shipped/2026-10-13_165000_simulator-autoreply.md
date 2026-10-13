# Simulator: AI auto-reply aktif, demo-only, tanpa Meta

Catatan rilis (branch `be/sim-autoreply`, belum di-merge).

## Masalah
Pesan dari simulator juri masuk (`status: queued`) tapi AI tidak pernah membalas:
`WRN AI job: unsupported/invalid channel provider=simulator` di `ai/autoreply.go` — gerbang auto-reply hanya menerima `meta_cloud`.

## Perubahan
- `autoReplyRoute()` (`ai/autoreply.go`): `simulator` (connected) → rute simulator tanpa kredensial Meta; `meta_cloud` tetap butuh access token + phone number id.
- `sendAiMessage` bercabang ke `sendSimulatorMessage` **sebelum** kode Meta; balasan hanya disimpan di percakapan simulator.
- Simulator hanya untuk tenant demo: env `SimulatorTenantAllowlist` (ID tenant atau schema, dipisah koma). Kosong = semua ditolak (`403`). Tenant selalu dari sesi server.
- Rate limit simulator: 20/menit per tenant **dan** per session, di Redis + limiter in-memory per instance (Redis mati tetap `429` setelah batas).
- Tidak ada perubahan default katalog model di kode. Gemini-first hanya lewat config DB (`ai_providers`, `ai_function_config`).

## Deploy
Set `SimulatorTenantAllowlist` di service demo (mis. `t_toko_demo`) sebelum dipakai juri; tanpa itu simulator menjawab 403.

## Test
- `ai/simulator_route_test.go`: tripwire `graph.facebook.com` (0 request), spy sender (simulator 1×, Meta 0×; meta_cloud tetap lewat sender Meta), tool katalog read-only vs prompt injection.
- `webhook/simulator_guard_test.go`: tenant non-demo 403, allowlist kosong fail-closed, Redis mati → 429 setelah 20.
- `internal/simulator/guard_test.go`: parsing allowlist, limiter in-memory reset per window.
- Sudah ada: `shared/llm/tools_tenant_test.go` (arg tool tidak bisa ganti tenant).
