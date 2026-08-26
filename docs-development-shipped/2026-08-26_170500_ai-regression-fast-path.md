# AI regression fast path — fase 2 selesai

**Tanggal:** 2026-08-26  
**PR:** #139 (`feat/ai-regression-fast-gotest`)

## Ringkasan

Ekstraksi routing buyer ke `internal/buyerflow` — pure Go, tanpa Encore/Postgres.

| Sebelum | Sesudah |
|---------|---------|
| `encore test ./ai/` ~60–90s | `go test ./internal/buyerflow/` **<1s** |
| Simulator & production duplikat FSM | `handleOrderFlow` → `AdvanceOrderFlow` |

## Perubahan utama

1. **`internal/buyerflow/`** — `Turn`, `AdvanceOrderFlow`, intent detectors, catalog, FSM
2. **`ai/buyerflow_bridge.go`** — type alias + delegate (Encore layer tetap di `ai/`)
3. **`ai/order_flow_handler.go`** — production memanggil `AdvanceOrderFlow` (hapus ~485 baris duplikat)
4. **CI `regression-fast`** — `go test ./internal/buyerflow/` (tanpa Encore di PR gate)
5. **Smoke master** — `encore test ./ai/` subset tetap jalan

## Menambah regression test

Tambah case di `internal/buyerflow/regression_cases.go`, jalankan:

```bash
./scripts/run-ai-regression-tests.sh
```

## PR #138

Ditutup — digabung ke #139.
