# internal/buyerflow — regresi AI <10 detik

## Tujuan

Jalankan **golden regression** percakapan WA (simulator, tanpa LLM/DB) dengan:

```bash
go test ./internal/buyerflow/ -count=1
```

Target CI: **<10 detik** (pure `go test`, tanpa `encore test`/Postgres).

## Arsitektur

```
internal/buyerflow/          # pure Go — sumber kebenaran routing
  simulator.go                 Turn() — routing multi-turn
  order_fsm.go                 AdvanceOrderFlow() — checkout FSM
  regression_cases.go          ← TAMBAH CASE BARU DI SINI
  regression_test.go           loop + assert
  export.go                    API untuk paket ai/

ai/
  buyerflow_bridge.go          type alias + delegate ke buyerflow
  order_flow_handler.go        handleOrderFlow → AdvanceOrderFlow (production)
```

**Satu implementasi** untuk simulator (test) dan production (`handleOrderFlow` memanggil `AdvanceOrderFlow`).

## Menambah regression test baru

1. Tambah entry di `regression_cases.go`
2. Lokal: `go test ./internal/buyerflow/ -run TestRegression/nama_case -v`
3. PR: check **AI Regression** (fast) wajib hijau

Triage loop (`scripts/triage-apply.go`) menulis `internal/buyerflow/regression_autogen_<8hex>_test.go` (satu file per job, simbol unik). Jangan merge PR `test(ai-triage)` buta jika ada diff routing.

## CI

| Job | Kapan | Perintah |
|-----|-------|----------|
| `regression-fast` | Setiap PR | `go test ./internal/buyerflow/` |
| `regression-encore-smoke` | Push master | `encore test ./ai/` subset |
