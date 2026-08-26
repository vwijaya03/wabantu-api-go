# AI Triage — catalog snapshot di auto-gen regression

**Status:** rilis (api-go)  
**Dokumen loop lengkap:** [AI_TRIAGE_LOOP_NEXT_DEV.md](../docs/AI_TRIAGE_LOOP_NEXT_DEV.md)  
**Lokasi autogen (2026-08-26):** `internal/buyerflow/regression_autogen_test.go` — lihat juga [2026-08-26_170500_ai-regression-fast-path.md](./2026-08-26_170500_ai-regression-fast-path.md)

## Masalah

Analyzer loop memakai `BuildSimulatorFromTenant` (katalog live DB tenant). Test GHA auto-gen memakai `newOmahSimulator()` (fixture hardcoded) → mismatch massal (`want consulting` vs `order_flow`) meski routing produksi benar.

## Perilaku setelah fix

1. `AnalyzeConversation` menyimpan `simulatorSnapshot` di `analysis_json` job.
2. `GenerateRegressionCases` embed snapshot katalog tenant.
3. `scripts/triage-apply.go` menulis `internal/buyerflow/regression_autogen_test.go` dengan snapshot embedded.
4. `TestRegressionAutoGen` di buyerflow membangun simulator dari snapshot (bukan fixture Omah).
5. `fixHints.testUsesFixture` = `tenant_catalog_snapshot`.

## File kunci

| File | Peran |
|------|--------|
| `ai/triage_snapshot.go` | Serialize / deserialize snapshot |
| `ai/triage.go` | Analyze + generate cases dengan snapshot |
| `scripts/triage-apply.go` | Tulis `internal/buyerflow/regression_autogen_test.go` |
| `internal/buyerflow/regression_autogen_test.go` | Auto-gen cases dari triage loop |
| `admin/ai_triage.go` | Pass snapshot ke `GenerateRegressionCases` |

## Catatan operasional

- Job/PR triage **sebelum** fix snapshot perlu **loop ulang** agar file auto-gen punya snapshot.
- Snapshot max ~40 item katalog (sama dengan `loadActiveCatalog` di analyze).
- Golden suite manual di `internal/buyerflow/regression_cases.go` tetap pakai fixture Omah — tidak berubah.
- Stub redirect lama: `ai/conversation_regression_auto_gen_test.go` (tidak dipakai lagi).
