# AI Triage Runner (CI only)

Node.js runner untuk workflow GitHub Actions `ai-triage-cursor-fix.yml`.
Memanggil Cursor SDK (`@cursor/sdk`) untuk patch routing di `ai/autoreply.go` dan `ai/conversation_sim.go`.

**Bukan runtime Encore** — tidak di-deploy ke production.

## Lokal (debug)

```bash
cd tools/ai-triage-runner
npm ci
export CURSOR_API_KEY=...
export TRIAGE_JOB_JSON=/tmp/triage_job.json
cd ../..   # api-go root sebagai cwd agent
node tools/ai-triage-runner/triage-cursor-fix.mjs
```

## Struktur repo

| Path | Peran |
|------|--------|
| `tools/ai-triage-runner/` | Cursor Composer fix (Node) |
| `scripts/triage-apply.go` | Tulis auto-gen regression test (Go) |
| `scripts/run-triage-autogen-tests.sh` | `encore test` di GHA |
| `admin/ai_triage.go` | Orkestrasi job (Encore) |
