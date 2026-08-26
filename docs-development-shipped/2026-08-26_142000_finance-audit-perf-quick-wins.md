# Finance audit: test HPP wallet & quick wins performa

**Tanggal:** 2026-08-26  
**PR:** [#129](https://github.com/vwijaya03/wabantu-api-go/pull/129)  
**Tipe:** fix, test, perf  
**Status:** Dalam PR (belum merge ke `master`)

## Masalah / Kebutuhan

Item partial dari rencana perbaikan finance & audit di api-go belum selesai:

- Belum ada test yang meng-assert HPP/COGS memakai wallet yang sama dengan `order.income_wallet_id`.
- HTTP client eksternal (Midtrans, RajaOngkir, Anthropic) tanpa timeout — risiko hang.
- Query inbox memakai subquery berkorelasi tanpa index `payment_proof_message_id`.
- Lazy schema migration masih dedupe 5 menit per koneksi (kurang efisien).

## Perubahan

### Test HPP wallet

- `inventory/order_sync_wallet_test.go` — integrasi `resyncOrderCOGS` assert `fin_transaction.wallet_id` = `order.income_wallet_id`.

### HTTP timeout

| Layanan | Timeout | File |
|---------|---------|------|
| Midtrans | 15s | `payment/payment.go` |
| RajaOngkir | 15s | `shipping/shipping.go` |
| Anthropic | 20s | `ai/anthropic.go` |

### Index & query inbox

- Index baru `idx_order_payment_proof_message` di `tenant/tenant.go` + `tenant/schema_patch.go`.
- Subquery berkorelasi diganti `LEFT JOIN "order" o` di `inbox/messages_query.go`.
- Test QualifySQL di `inbox/messages_query_test.go` dan `shared/db/tenant_scope_test.go`.

### Lazy migrate

- `sync.Once` per schema di `tenant/migrate_jobs.go` (reset jika publish gagal).
- Unit test di `tenant/migrate_jobs_lazy_test.go`.

## File utama

- `inventory/order_sync_wallet_test.go`
- `inbox/messages_query.go`, `inbox/messages_query_test.go`
- `tenant/migrate_jobs.go`, `tenant/migrate_jobs_lazy_test.go`
- `tenant/tenant.go`, `tenant/schema_patch.go`
- `payment/payment.go`, `shipping/shipping.go`, `ai/anthropic.go`

## Testing

- [x] `encore test ./inbox/... ./shared/db/... ./tenant/... ./ai/... ./inventory/...`
- [ ] Manual: buka inbox percakapan dengan bukti transfer — `linkedOrderId` tetap muncul
- [ ] Manual: order dengan wallet non-default → HPP masuk wallet yang sama

## Catatan deploy

- Index `idx_order_payment_proof_message` akan dibuat via lazy migrate / schema patch — tidak perlu migrasi manual jika pipeline tenant patch sudah jalan.
- Tidak menyentuh `shared/db/tenant_scope.go` regex, `pool_retry.go`, atau `OpenTenantScope` (sengaja terpisah dari batch #121–#127).
- §6 consent (tenant access request) sengaja tidak disertakan — dikerjakan di PR #130.
