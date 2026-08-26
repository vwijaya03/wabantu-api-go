# AI regression: percepat CI + rencana <10 detik

**Tanggal:** 2026-08-26  
**PR:** #138 (cache CI), branch `feat/ai-regression-fast-gotest` (workflow + doc)  
**Status:** Fase 1 selesai; fase 2 (internal/buyerflow) direncanakan

## Masalah

- Workflow **AI Regression** 1–3 menit (kadang 10 menit antrian/cache miss).
- Eksekusi test simulator sebenarnya **<1 detik**; bottleneck = `encore test` (compile monorepo + Postgres Docker).

## Fase 1 — CI cache (sekarang)

- Cache Encore CLI (`~/.encore/bin`, `~/.encore/cache`)
- Pre-pull `encoredotdev/postgres:18`
- `paths` filter PR (skip jika hanya docs)
- `concurrency` cancel-in-progress
- Skip `fix-encore-test-db.sh` di GHA

Target: **~60–90s** saat cache warm (bukan <10s).

## Fase 2 — `internal/buyerflow` (rencana)

Ekstrak routing buyer murni ke `internal/buyerflow/` agar:

```bash
go test ./internal/buyerflow/ -count=1   # <10s di CI
```

Detail: [`internal/buyerflow/README.md`](../../internal/buyerflow/README.md).

### Menambah regression test baru (setelah fase 2)

1. Edit `internal/buyerflow/regression_cases.go` — tambah `regressionCase{...}`.
2. Lokal: `go test ./internal/buyerflow/ -run TestRegression/your_case -v`
3. Bug dari WA nyata → bisa juga lewat AI Triage → auto-gen file.

Sampai fase 2: tetap edit `ai/conversation_regression_test.go` + `./scripts/run-ai-regression-tests.sh`.

## Dua tier CI

| Job | Kapan | Tool |
|-----|-------|------|
| `regression-fast` | Setiap PR | `encore test` (→ `go test` buyerflow) |
| `regression-encore-smoke` | Push master | `encore test` subset tambahan |

## Catatan

Build tag `airegress` di paket `ai/` **tidak** dipakai — coupling terlalu dalam (`order`, `autoreply`, `triage`). Ekstrak paket terpisah lebih aman untuk maintenance jangka panjang.
