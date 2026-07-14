# AI Triage — catalog snapshot di auto-gen regression

**Status:** rilis (api-go)  
**Dokumen loop lengkap:** [AI_TRIAGE_LOOP_NEXT_DEV.md](../docs/AI_TRIAGE_LOOP_NEXT_DEV.md)

## Masalah

Analyzer loop memakai `BuildSimulatorFromTenant` (katalog live DB tenant). Test GHA auto-gen memakai `newOmahSimulator()` (fixture hardcoded) → mismatch massal (`want consulting` vs `order_flow`) meski routing produksi benar.

## Perilaku setelah fix

1. `AnalyzeConversation` menyimpan `simulatorSnapshot` di `analysis_json` job.
2. `GenerateRegressionCases` embed `const triageAutoGenSnapshotJSON = "..."` di `conversation_regression_auto_gen_test.go`.
3. `TestConversationRegressionAutoGen` membangun simulator dari snapshot (bukan fixture).
4. `fixHints.testUsesFixture` = `tenant_catalog_snapshot`.

## File kunci

| File | Peran |
|------|--------|
| `ai/triage_snapshot.go` | Serialize / deserialize snapshot |
| `ai/triage.go` | Analyze + generate cases dengan snapshot |
| `scripts/triage-apply.go` | Harness test `mustTriageAutoGenSimulator` |
| `admin/ai_triage.go` | Pass snapshot ke `GenerateRegressionCases` |

## Catatan operasional

- Job/PR triage **sebelum** fix ini perlu **loop ulang** agar file auto-gen punya snapshot.
- Snapshot max ~40 item katalog (sama dengan `loadActiveCatalog` di analyze).
- Golden suite `conversation_regression_test.go` tetap pakai fixture Omah — tidak berubah.
