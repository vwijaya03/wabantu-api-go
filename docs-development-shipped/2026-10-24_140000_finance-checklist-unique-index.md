# 2026-10-24 — fix(finance): index unik checklist tanpa tergantung guard (M1)

Masalah: `GET /finance/checklist/today` (dan billing bulanan) 500 SQLSTATE 42P10 di tenant lokal baru, karena `idx_fin_checklist_tpl_date` hanya ada di patch yang dilewati guard `FinanceModuleReady` (sejak c52e856).

Perbaikan:
- Index ditambahkan ke `tenantDDL` via `tenantschema.FinChecklistIndexSQL` (dedupe + CREATE UNIQUE INDEX dalam satu blok DO, hanya bila index belum ada).
- `FinanceModuleReady` mensyaratkan index tersebut.
- Patch finance/tenant/cloud memakai SQL yang sama.
- Error DB di checklist/billing tidak lagi dikirim mentah ke klien; detail dicatat via rlog.

Aturan simpan dedupe `fin_checklist_item` per `(template_id, due_date)` (deterministik, tanpa ctid): 1) `status <> 'pending'` (done) dulu, 2) `completed_at` terisi, 3) `transaction_id` terisi, 4) `created_at` terbaru, 5) `id` terbesar.
