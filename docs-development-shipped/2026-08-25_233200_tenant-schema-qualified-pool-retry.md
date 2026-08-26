# Tenant schema-qualified SQL & hardening pool pgx (08P01)

**Tanggal:** 2026-08-25  
**PR:** [#121](https://github.com/vwijaya03/wabantu-api-go/pull/121), [#122](https://github.com/vwijaya03/wabantu-api-go/pull/122), [#123](https://github.com/vwijaya03/wabantu-api-go/pull/123), [#124](https://github.com/vwijaya03/wabantu-api-go/pull/124), [#125](https://github.com/vwijaya03/wabantu-api-go/pull/125), [#126](https://github.com/vwijaya03/wabantu-api-go/pull/126), [#127](https://github.com/vwijaya03/wabantu-api-go/pull/127)  
**Tipe:** fix, feat, perf  
**Status:** Sudah merge ke `master`

## Masalah / Kebutuhan

Superadmin yang sering berganti tenant di staging memicu error PostgreSQL `08P01: prepared statement did not exist`. Akar masalahnya bukan hanya `SET search_path`, melainkan kombinasi:

- pgx meng-cache prepared statement per koneksi pool, sementara OID tabel berubah saat `search_path` berganti antar schema tenant.
- Banyak modul masih memakai `TenantConn` + tabel tanpa prefix schema pada setiap request.
- Runtime schema guard (`ensureContactRuntimeSchema`, `ensurePricingSchema`, dll.) masih memanggil `TenantConn` di hot path.
- Setelah migrasi ke schema-qualified SQL, error `08P01` tetap muncul di koneksi pool karena statement stale tidak ter-deallocate dengan andal.

Batch ini melanjutkan perbaikan signup/cloud DDL (#112–#120) dengan fokus ke **isolasi query runtime** dan **reliability pool**.

## Perubahan

### PR #121 — Migrasi ke schema-qualified SQL

- Tambah helper `shared/db/qualify.go`, `tenant_scope.go`: `Qualify`, `TenantScope`, `QualifySQL` untuk rewrite `"schema"."table"`.
- Tambah `tenant.PrepareTenantAccess()` — lazy migrate tanpa `search_path`.
- Update `shared/tenantschema`: introspeksi pakai `pg_catalog` + schema eksplisit; wrapper `*Conn` untuk DDL.
- Migrasi **semua modul service** (inbox, business, analytics, kb, leads, branch, billing, workflow, whatsappapi, webhook, inventory, finance, events, order, ai) dari `TenantConn` + unqualified table ke schema-qualified DML.
- `TenantConn` / `CloseTenantConn` hanya tersisa di jalur DDL (schema patch, admin migrate, `EnsureSchema`).
- Fix minor AI: pertahankan intent `consulting` untuk pertanyaan beli tanpa spesifikasi.

### PR #122 — Hapus TenantConn dari runtime schema guard

- `inbox/ensureContactRuntimeSchema` → pool + `ContactRuntimeReady(ctx, pool, schema)`.
- `inbox/ensurePIISchema` → `ContactPIIActive` tanpa conn `search_path`.
- `business/ensurePricingSchema` + `shared/pricing.EnsureSchema` → pool + schema eksplisit.
- `tenant/EnsureKnowledgeBaseSchema` → pool + `TableExists` eksplisit.
- `order/normalizeOrderItems` → `pricing.EnsureSchema` tanpa `TenantConn`.
- `tenantschema.contactPIIActiveUncached` → `ColumnExists` dengan schema eksplisit.

### PR #123 — Pool retry 08P01 (lapisan pertama)

- Tambah `poolRetryQuerier` / `connRetryQuerier`: retry sekali dengan `DEALLOCATE ALL` saat `08P01`.
- `OpenTenantScope` dan `tenantschema.Q()` otomatis wrap `*sql.DB` / `*sql.Conn` agar DML + pg_catalog guard aman di pool.

### PR #124 — Readiness gate & migration lock

- Endpoint baru `GET /api/v1/tenant/readiness` — sinyal `{ ready, cloudReady, migrating, ... }` untuk gate frontend saat superadmin switch tenant.
- Serialize migrasi dengan `pg_try_advisory_lock(hashtext(schema))` — hindari deadlock lazy migrate + batch migrate.
- `tenantSchemaBaseProvisioned` tanpa `SET search_path` (pakai `TableExists` qualified).
- Hapus `EnsureTenantSchemaProvisioned` dari `analytics.Overview` (hot path).
- Seed `evt_*` pakai nama tabel schema-qualified; `evt_therapy` masuk `EventsModuleReady`.
- Pool retry: double-attempt pada `08P01` jika koneksi pertama masih rusak.

### PR #125 — QualifySQL order fix & events OpenTenantScope

- Perbaiki regex tabel `order` (quoted vs unquoted) di `shared/db/tenant_scope.go` — hilangkan double quote (`"schema"."order""`).
- `shared/db/pool_retry.go`: retry hingga 5x lewat dedicated `pool.Conn()` + `DEALLOCATE ALL` setelah `08P01` (termasuk `QueryContext`).
- `events/tenant_db.go`: ganti custom `tenantScope` dengan `appdb.OpenTenantScope` (schema-qualified + pool retry).
- `events/helpers.go`: fast path `EventsModuleReady` sebelum `TenantConn` / `SET search_path`.
- `inventory/schema_guard.go`: fast path `InventoryModuleReady` sebelum `TenantConn`.

### PR #126 & #127 — Pool retry di finance, inventory, ai

- Extend pola pool retry ke modul `finance`, `inventory`, dan `ai` (`*/tenant_db.go`).
- PR #127 cherry-pick ke `master` karena #126 ter-merge ke branch stacked, bukan mainline.

## File utama

| Area | File kunci |
|------|------------|
| Helper DB | `shared/db/qualify.go`, `shared/db/tenant_scope.go`, `shared/db/pool_retry.go` |
| Tenant infra | `tenant/readiness.go`, `tenant/migrate_jobs.go`, `shared/tenantschema/` |
| Per-modul scope | `*/tenant_db.go` (ai, business, events, finance, inbox, inventory, …) |
| Schema guard | `inbox/contact_store.go`, `business/pricing.go`, `events/helpers.go`, `inventory/schema_guard.go` |

## Testing

- [x] `encore test ./shared/db/...` — termasuk `TestQualifySQLOrderTableQuoted`
- [x] `encore test ./tenant/... ./events/... ./inventory/... ./inbox/...`
- [x] `encore test ./...` (full suite pada PR #125)
- [ ] Staging: superadmin rapid switch `t_kraveli` ↔ `t_chloe_fashion` ↔ `t_omah_apparel`
- [ ] Verifikasi trace bersih: `ListContacts`, `GetMessages`, `GetUnreadSummary`, `ListPriceTypes`, `ListEvents`, `analytics.Overview`
- [ ] `POST /api/v1/admin/migrate-tenant-schemas` untuk tenant schema belum lengkap

## Catatan deploy

- Deploy backend **sebelum atau bersamaan** dengan PR frontend readiness poll (`GET /tenant/readiness`).
- Monitor Encore traces 15 menit pertama post-deploy — fokus endpoint inbox, business pricing, events.
- Pola referensi yang sudah stabil sejak awal: modul `broadcast/` (sudah schema-qualified).
- Tidak ada breaking change API publik; perubahan internal akses database.
