# Fix: GET /simulator/messages selalu `pricing: null`

Catatan rilis (branch `be/sim-autoreply`, belum di-merge). Severity Medium.

## Gejala
Order reseller ada (`order.total` 144000), tapi `pricing` null; tidak ada log error.

## Akar masalah
`internal/simulator.LoadState` dengan qualifier nil meng-quote semua nama tabel (`"conversation"`). `appdb.TenantScope` lalu menjalankan `QualifySQL`, yang menulis ulang nama di dalam tanda kutip → `""t_toko_demo"."conversation""` (syntax error). Error di-swallow → `pricing` null. Query order selamat karena `QualifySQL` punya penanganan khusus untuk `"order"`. Kena juga di mirror Firestore (ai/simulator_send, inbox, webhook) yang memakai qualifier nil.

## Perbaikan
- `DefaultQualifier`: nama tabel polos (search_path / QualifySQL), hanya `"order"` di-quote.
- `OnLoadError` → `rlog.Warn("simulator state query failed", part, convoId, err)`; `sql.ErrNoRows` tidak dilog.
- Test `TestLoadStatePricingResellerViaTenantScope` memakai `QualifySQL` asli (gagal di kode lama: SQL rusak; lulus sekarang: tier `reseller`), `TestLoadStateReportsQueryErrors`.
