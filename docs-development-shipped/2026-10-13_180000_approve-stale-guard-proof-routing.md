# Fix: Setujui menyetujui pesanan lama (WB-07B19997) saat buyer mulai pesanan baru

Catatan rilis (branch `pr216/bugb-fix` → PR #216 `be/h1-llm-master`, belum di-push). QA spec 05, 2026-10-13 17:53 WIB.

## Akar masalah
- Buyer mengirim "mau pesan Kopi…" + gambar dalam satu burst. `resolvePaymentTargetOrder` (`ai/payment_proof.go`) memilih pesanan payable terbaru di percakapan = pesanan lama yang buktinya pernah **ditolak** (duplikat) → gambar jadi "bukti baru" pesanan lama (`proof_submitted`). Pesanan baru baru dibuat ±2,5 detik kemudian.
- Panel simulator masih menampilkan pesanan lama → Setujui mengirim id pesanan lama (backend menyetujui persis id dari path; benar).

## Perbaikan
1. Routing bukti (`ai/payment_proof_route.go`): pesanan dengan bukti ditolak **tidak** jadi target implisit bila ada pesanan lebih baru atau burst (≤2 menit sebelum gambar) berisi niat pesan baru → bot bertanya "bukti transfer ini untuk pesanan yang mana ya? (WB-… / WB-…)". Jalur normal tidak berubah (referensi eksplisit, pesanan unpaid/proof_submitted, kirim ulang bukti untuk pesanan yang sama).
2. Approve (`order/approve.go`):
   - query opsional `?expectedUpdatedAt=` (RFC3339Nano, URL-encoded; bukan body JSON agar klien lama yang POST tanpa body tetap jalan). Guard atomik di UPDATE (`WHERE id=$1 AND updated_at=$3`); 0 baris → 409 "pesanan berubah, muat ulang" (`orderapprove.ErrStale`).
   - Pantau (impersonation) → 403.
   - Id hanya dari path dan schema tenant sesi.
   - 409 bukti duplikat tetap.
3. Audit `order.approve` via `audit.Log` yang sudah ada (tabel `audit_log`, tanpa migrasi baru): account_id, session_kind, order_id, status/payment_status sebelum/sesudah, proof_id. Tanpa gambar/bank/nominal/PII buyer.

## Test
`ai.TestProofRouting*`, `ai.TestNewOrderIntentText`, `order.TestApproveGateImpersonation403`, `order.TestExpectedUpdatedAtRoundTripMicroseconds`, `order.TestCompleteVerifiedSQLIsGuardedAndIDScoped`, `order.TestApproveAuditFieldsNoPII`, `orderapprove.TestRunStaleGuardIs409AndRollsBack`.

## Dampak FE
Kirim `?expectedUpdatedAt=` + encodeURIComponent(`order.updatedAt`) persis dari respons GET; tangani 409 "pesanan berubah, muat ulang" dengan refetch.
