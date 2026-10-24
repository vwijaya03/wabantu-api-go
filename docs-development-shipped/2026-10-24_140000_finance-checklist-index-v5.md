# Finance checklist index: log sebelum dedupe, backup, schema v5 (2026-10-24)

Branch `fix/finance-checklist-index` (lanjutan M1).

- `logFinChecklistDuplicates` mengembalikan jumlah duplikat dan memanggil `rlog.Warn` (schema + jumlah) sebelum DDL dedupe di `RunSchemaPatches`, `RunTenantDDL`, `runFinanceSchemaAndSeed`, dan `applyCloudAdminTenantDDL`. Hanya kalau tabel ada dan index belum ada.
- `FinChecklistIndexSQL`: kalau duplikat > 0, buat `fin_checklist_item_dedupe_bak (LIKE fin_checklist_item)` + `deleted_at`, salin baris yang kalah, lalu DELETE, semuanya dalam satu blok DO. Tanpa duplikat, tabel backup tidak dibuat.
- `CurrentSchemaPatchVersion` 4 -> 5.
- Test: warn terpanggil dengan count=2 saat 4 baris masih ada; backup berisi 2 baris yang dihapus; rerun 2x dengan index ada -> 0 terhapus, tanpa warn, tanpa tabel backup.
- Backlog (tidak di commit ini): sisa `appErrs.Internal(err.Error())` di `finance/checklist.go` & `checklist_billing.go`.
