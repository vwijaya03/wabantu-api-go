# Drop loop routing AI Triage (self-heal only)

**Status:** PR (belum merge)

## Masalah / Kebutuhan

Loop path-only (`CreateAITriageJob` → `TestRegressionAutoGen` → GHA `ai-triage-fix.yml`) tidak mengubah isi/keranjang dan bisa mengunci path bug sebagai golden. Self-healing memakai `ai_triage_incident` + behavior job + `ai-triage-behavior-fix.yml`.

## Perubahan

- Cabut endpoint job routing, cron anomaly, internal GHA job callback.
- Drop tabel `ai_triage_job` dan `ai_triage_anomaly` (migration 21 + cloud patch).
- Hapus workflow `ai-triage-fix.yml` dan `ai-triage-cursor-fix.yml`.
- Tetap: Insiden, behavior job, repair, LLM scan, laporan Inbox, `CompareConversationRoutes` + tes, file `regression_autogen_*` yang sudah ada.

## File utama

- `admin/ai_triage_github.go` (dispatch Composer behavior)
- `admin/ai_triage_internal.go` (token + UUID path behavior-jobs)
- `system/migrations/21_drop_ai_triage_loop.up.sql`
- `.github/workflows/ai-triage-behavior-fix.yml` (tetap)

## Testing

- `encore check`
- `encore test ./admin/ ./ai/ ./internal/triageautogen/ ./internal/apiregistry/`

## Catatan deploy

Encore Cloud menjalankan migration 21. `CloudSystemPatchSQL` DROP IF EXISTS untuk staging yang patch manual. Frontend harus merge PR yang tidak lagi memanggil `/admin/ai-triage/jobs`.
