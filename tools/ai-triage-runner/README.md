# AI Triage Runner (CI only)

Node.js runner untuk workflow GitHub Actions `ai-triage-behavior-fix.yml`.
Memanggil Cursor SDK (`@cursor/sdk`) untuk patch buyerflow sesuai kontrak Insiden (Composer 2.5).

**Bukan runtime Encore** — tidak di-deploy ke production.

## Lokal (debug)

Butuh **Node.js ≥ 20**. Workflow CI memakai Node 24.

```bash
cd tools/ai-triage-runner
npm ci
export CURSOR_API_KEY=...
cd ../..   # api-go root sebagai cwd agent
node tools/ai-triage-runner/triage-behavior-cursor-fix.mjs
```

## Struktur repo

| Path | Peran |
|------|--------|
| `tools/ai-triage-runner/triage-behavior-cursor-fix.mjs` | Composer self-heal (Insiden) |
| `scripts/run-triage-behavior-tests.sh` | Prove RED `TestBehavior_` di GHA |
| `admin/ai_triage_behavior_job.go` | Orkestrasi job Composer |
