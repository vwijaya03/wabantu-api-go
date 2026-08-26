# internal/buyerflow — rencana regresi AI <10 detik

## Tujuan

Jalankan **golden regression** percakapan WA (simulator, tanpa LLM/DB) dengan:

```bash
go test ./internal/buyerflow/ -count=1
```

Target CI: **<10 detik** (vs ~60–90s `encore test` karena compile app + Postgres Docker).

## Kenapa belum di sini?

Logika routing buyer (`ConversationSimulator.Turn`) masih di paket `ai/` yang terikat `encore test` lewat:

- `sqldb` di `autoreply.go`
- Import `order`, `inventory`, `usage`, dll. yang memicu init Encore

Memisahkan dengan build tag `airegress` di seluruh `ai/` terlalu rapuh (banyak coupling).

## Arsitektur target (production-ready)

```
internal/buyerflow/          # pure Go, no Encore
  simulator.go                 # Turn() — sumber kebenaran routing
  order_fsm.go                 # AdvanceOrderFlow (tanpa persist DB)
  intent.go, greeting.go, ...  # helper routing
  fixtures_omah.go
  regression_cases.go          # ← TAMBAH CASE BARU DI SINI
  regression_test.go           # loop + assert
  regression_autogen_test.go   # output triage loop

ai/
  autoreply.go                 # panggil buyerflow.Turn / AdvanceOrderFlow
  conversation_sim.go          # thin wrapper / type alias
```

**Aturan tambah regression test baru:**

1. Tambah entry di `regression_cases.go` (golden) atau terima auto-gen dari triage.
2. Jalankan lokal: `go test ./internal/buyerflow/ -run TestRegression -v`
3. PR: check **AI Regression** (fast) wajib hijau.

Triage loop (`scripts/triage-apply.go`) akan ditulis ke `internal/buyerflow/regression_autogen_test.go`.

## Fase implementasi

| Fase | Isi | Durasi estimasi |
|------|-----|-----------------|
| **1 (sekarang)** | PR #138 cache CI + doc ini | selesai |
| **2** | Ekstrak tipe + `AdvanceOrderFlow` + `Turn` ke `buyerflow` | 1–2 hari |
| **3** | Wire `ai/autoreply` → `buyerflow`, hapus duplikasi | 0.5 hari |
| **4** | Pindah test + update triage-apply + CI `go test` | 0.5 hari |
| **5** | `encore test` smoke hanya di push master (sudah ada di workflow) | selesai |

## Paritas production

Simulator **harus** memanggil fungsi yang sama dengan `autoreply.go` (bukan copy logic).
Setelah fase 3, satu implementasi `buyerflow.Turn` dipakai production + test.

Sampai fase 4 selesai, gate PR tetap `encore test` via `scripts/run-ai-regression-tests.sh` (sudah dioptimasi cache).
