# AI regression fast path + API endpoint registry struktural

**Tanggal:** 2026-08-26  
**PR:** [#139](https://github.com/vwijaya03/wabantu-api-go/pull/139) (`feat/ai-regression-fast-gotest`)  
**Tipe:** feat / perf(ci)  
**Status:** Merged ke `master`

## Masalah / Kebutuhan

1. **AI regression lambat** — `encore test ./ai/` ~60–90s per PR; simulator dan production FSM routing buyer duplikat.
2. **335+ endpoint tanpa inventaris** — tidak ada gate struktural saat menambah/mengubah `//encore:api`; risiko duplikat route atau drift tanpa terdeteksi di CI.
3. **Triage autogen di paket `ai/`** — file auto-gen regression masih di `ai/`, tidak selaras dengan ekstraksi buyerflow.

## Perubahan

### 1. `internal/buyerflow` — routing AI pure Go

| Sebelum | Sesudah |
|---------|---------|
| `encore test ./ai/` ~60–90s | `go test ./internal/buyerflow/` **<1s** |
| Simulator & production duplikat FSM | `handleOrderFlow` → `AdvanceOrderFlow` |

- `Turn`, `AdvanceOrderFlow`, intent detectors, catalog, FSM di `internal/buyerflow/`
- `ai/buyerflow_bridge.go` — type alias + delegate (layer Encore tetap di `ai/`)
- `ai/order_flow_handler.go` — production memanggil `AdvanceOrderFlow` (~485 baris duplikat dihapus)
- Golden cases di `regression_cases.go`; triage autogen di `regression_autogen_test.go`

### 2. `internal/apiregistry` — golden snapshot endpoint

- Scan semua `//encore:api` di repo → `catalog_snapshot.json` + `service_counts.json`
- Baseline awal: **335 endpoint, 28 service**
- Test struktural: duplikat route, drift snapshot, path well-formed, route kritis (health/auth/webhook), counts per service

Regenerasi katalog:

```bash
go run scripts/gen-api-catalog.go
# atau
./scripts/gen-apiregistry-catalog.sh
```

### 3. Triage autogen → buyerflow

- `scripts/triage-apply.go` menulis ke `internal/buyerflow/regression_autogen_test.go` (bukan `ai/conversation_regression_auto_gen_test.go`)
- `ai/conversation_regression_auto_gen_test.go` — stub redirect ke buyerflow

### 4. CI

| Job | Workflow | Perintah |
|-----|----------|----------|
| `regression-fast` | `.github/workflows/ai-regression.yml` | `./scripts/run-ai-regression-tests.sh` → buyerflow + apiregistry |
| `regression-encore-smoke` | push `master` saja | autogen buyerflow + subset `encore test ./ai/` |

## File utama

| Area | Path |
|------|------|
| Buyerflow FSM | `internal/buyerflow/simulator.go`, `order_fsm.go`, `regression_cases.go` |
| Bridge produksi | `ai/buyerflow_bridge.go`, `ai/order_flow_handler.go` |
| API registry | `internal/apiregistry/discover.go`, `catalog_snapshot.json`, `service_counts.json` |
| Generator | `scripts/gen-api-catalog.go`, `scripts/gen-apiregistry-catalog.sh` |
| Triage apply | `scripts/triage-apply.go` |
| CI | `.github/workflows/ai-regression.yml`, `scripts/run-ai-regression-tests.sh` |
| Runtime docs | `docs/API_ENDPOINT_REGISTRY.md`, `internal/buyerflow/README.md` |

## Testing

```bash
# Gate cepat PR (<10s, tanpa Encore/Postgres)
./scripts/run-ai-regression-tests.sh

# Atau terpisah
go test ./internal/buyerflow/ -count=1
go test ./internal/apiregistry/ -count=1
```

Menambah regression AI: tambah case di `internal/buyerflow/regression_cases.go`, lalu jalankan script di atas.

## Catatan deploy

- Tidak ada migrasi DB atau secret baru.
- Setelah menambah endpoint `//encore:api` baru, wajib regenerate `internal/apiregistry/catalog_snapshot.json` — CI `regression-fast` gagal jika drift.
- PR [#138](https://github.com/vwijaya03/wabantu-api-go/pull/138) ditutup — digabung ke #139.
