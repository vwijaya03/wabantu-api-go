# RAG staging rollout — hotfix deploy (PR #146–#150)

## Masalah / Kebutuhan

Setelah merge RAG v1 (PR [#145](https://github.com/vwijaya03/wabantu-api-go/pull/145)), deploy ke **Encore Cloud staging** gagal atau indexing tidak pernah selesai:

| PR | Masalah |
|----|---------|
| [#146](https://github.com/vwijaya03/wabantu-api-go/pull/146) | Build gagal: `db already declared` — konflik import `encore.dev/storage/sqldb` vs alias `shared/db` di `kb/tx_scope_test.go` |
| [#147](https://github.com/vwijaya03/wabantu-api-go/pull/147) | Deploy gagal: secret `OpenAIApiKey`, `PineconeApiKey`, `PineconeIndexHost` tidak terdefinisi di env **staging** (script hanya set `type:local`) |
| [#148](https://github.com/vwijaya03/wabantu-api-go/pull/148) | Lazy migrate tenant gagal: duplicate key `idx_fin_cat_sys_child_name_parent` saat seed kategori finance; race bootstrap migrasi |
| [#149](https://github.com/vwijaya03/wabantu-api-go/pull/149) | RAG rollout gagal: `column "embedding_version" does not exist` — patch retrieval tidak dijalankan di cloud (app role tidak bisa DDL) |
| [#150](https://github.com/vwijaya03/wabantu-api-go/pull/150) | FAQ backfill stuck `failed` + `invalid embedding version` — outbox/reindex mengirim `embedding_version=0`; indexer mensyaratkan `version >= 1` |

**Kasus nyata (Omah Apparel, staging):** 9 FAQ `failed`, katalog 17/17 OK, `isComplete: false` (65%) sampai deploy #150 + rollout ulang.

## Perubahan

### PR #146 — fix build test KB

- `kb/tx_scope_test.go`: import `encore.app/wabantu/shared/db` sebagai `appdb` (hindari bentrok dengan variabel package `db` di test lain).

### PR #147 — secrets RAG ke staging

- `scripts/setup-secrets-for-cloud.sh`: tambah `OpenAIApiKey`, `PineconeApiKey`, `PineconeIndexHost`; fallback `WebhookVerifyToken` dari env.
- `scripts/setup-secrets-from-env.sh`: flag `--env staging` (atau nama env lain) agar secret tidak hanya `type:local`.

```bash
cd api-go
REDIS_URL='rediss://...' ./scripts/setup-secrets-for-cloud.sh staging
# atau
./scripts/setup-secrets-from-env.sh --env staging
```

### PR #148 — finance seed idempotent + lock migrasi

- `tenant/finance_seed.go`: `seedFinanceCategories` idempotent (`ON CONFLICT DO NOTHING` / skip jika sudah ada).
- `tenant/migration_lock.go`: advisory lock per schema saat bootstrap/lazy migrate — hindari dua worker seed bersamaan.

### PR #149 — DDL retrieval otomatis di cloud

- `tenant/cloud_admin_ddl.go`: blok **retrieval patch** (`retrieval_outbox`, kolom embedding KB/katalog) di registry `cloudAdminTenantDDLBlocks`.
- `shared/tenantschema/ready.go`: check `RetrievalReady` masuk `CloudTenantReady`.
- `tenant/migrate_jobs.go`: worker migrasi tenant memanggil `EnsureRetrievalSchema` / `EnsureCloudAdminTenantDDL`.

**Operasi:** tidak perlu `apply-tenant-schema-cloud.sh` manual untuk kolom RAG — cukup **deploy** + `POST /api/v1/admin/migrate-tenant-schemas` atau rollout RAG (worker apply DDL via role admin).

### PR #150 — backfill FAQ bump `embedding_version`

- `kb/index_hooks.go` — `enqueueKBIndexOutbox`: panggil `bumpKBEmbeddingPendingTx` (sama seperti create/update FAQ) sebelum insert outbox.
- `kb/retrieval_outbox.go` — `supersedeStaleKBOutboxTx`: tandai outbox KB `pending`/`failed` lama sebagai `done` agar retry tidak memblokir `isComplete`.
- `kb/retrieval_worker.go` — `Reindex` memakai `enqueueKBIndexOutbox` (DRY).

**Aturan runtime:**

| Path | `embedding_version` |
|------|---------------------|
| Create/update FAQ (`kb/kb.go`) | Bump ke ≥ 1 |
| Backfill / reindex / rollout (`EnqueueRAGBackfillForTenant`) | Bump ke ≥ 1 (setelah #150) |
| Katalog backfill | Tetap pakai version dari DB (tanpa guard ≥ 1 di indexer) |

Indexer KB (`shared/retrieval/indexer.go`): `IndexKBEntry` menolak `Version < 1` dengan error `invalid embedding version`.

## File utama

| Area | File |
|------|------|
| Test build | `kb/tx_scope_test.go` |
| Secrets ops | `scripts/setup-secrets-for-cloud.sh`, `scripts/setup-secrets-from-env.sh` |
| Finance migrate | `tenant/finance_seed.go`, `tenant/migration_lock.go` |
| Cloud DDL RAG | `tenant/cloud_admin_ddl.go`, `tenant/schema_patch_retrieval.go`, `shared/tenantschema/ready.go` |
| KB backfill | `kb/index_hooks.go`, `kb/retrieval_outbox.go`, `kb/retrieval_worker.go` |

## Testing

```bash
cd api-go
encore test ./kb/...
encore test ./tenant/...
encore check
```

## Catatan deploy

### Urutan staging RAG (setelah PR #146–#150)

1. Set secrets staging (termasuk RAG) — **sebelum** deploy.
2. Push / merge ke `master` → pantau rollout Encore sukses.
3. `POST /api/v1/admin/migrate-tenant-schemas` (superadmin) jika tenant tertinggal patch.
4. **Trigger indexing ulang** — merge/deploy **tidak** otomatis retry FAQ yang sudah `failed`:
   - Superadmin: `/dashboard/admin/ai-retrieval` → rollout **Semua tenant aktif**, atau
   - Owner tenant: `POST /api/v1/knowledge-base/reindex`, atau
   - Toggle mode: Lexical → Vector (simpan) untuk satu tenant.
5. Verifikasi: `GET /api/v1/flags/retrieval-indexing/:tenantId` → `isComplete: true`, `kb.failed: 0`.

> Scope rollout **Hanya tenant lexical** tidak menyentuh tenant yang sudah `shadow`/`vector`. Untuk retry tenant yang sudah vector, pakai **Semua tenant aktif** atau reindex per tenant.

### Troubleshooting cepat

| Gejala | Penyebab | Tindakan |
|--------|----------|----------|
| Deploy gagal: secret RAG tidak terdefinisi | Belum set env staging | `setup-secrets-for-cloud.sh staging` |
| `embedding_version does not exist` | DDL retrieval belum di cloud | Deploy #149+; migrate tenant |
| KB `failed`, `invalid embedding version` | Backfill version 0 (pre-#150) | Deploy #150; rollout/reindex ulang |
| `isComplete: false`, outbox `failed` > 0 | Outbox lama + entitas belum re-index | Rollout `all_active` atau reindex |
| Duplicate key finance seed | Seed non-idempotent (pre-#148) | Deploy #148+; retry migrate |

### Verifikasi DB (TablePlus / `encore db conn-uri tenant --env=staging --admin`)

```sql
-- Ganti schema tenant
SELECT embedding_status, embedding_version, COUNT(*)
FROM t_omah_apparel.knowledge_base_entry
WHERE deleted_at IS NULL AND is_active
GROUP BY 1, 2;

SELECT status, entity_type, version, COUNT(*)
FROM t_omah_apparel.retrieval_outbox
GROUP BY 1, 2, 3;
```

Harapan setelah fix + retry: KB `indexed` dengan `embedding_version >= 1`; outbox KB terbaru `done` version ≥ 1.

## PR

| PR | Judul |
|----|-------|
| [#146](https://github.com/vwijaya03/wabantu-api-go/pull/146) | fix(kb): alias import shared/db di tx_scope_test |
| [#147](https://github.com/vwijaya03/wabantu-api-go/pull/147) | fix(ops): set secret RAG ke staging |
| [#148](https://github.com/vwijaya03/wabantu-api-go/pull/148) | fix(tenant): seed fin_category idempotent + lock bootstrap |
| [#149](https://github.com/vwijaya03/wabantu-api-go/pull/149) | fix(tenant): apply DDL RAG otomatis di cloud |
| [#150](https://github.com/vwijaya03/wabantu-api-go/pull/150) | fix(kb): bump embedding_version saat backfill/reindex FAQ |

Lihat juga: [RAG_VECTOR_RETRIEVAL.md](../docs/RAG_VECTOR_RETRIEVAL.md) · [DEPLOY_ENCORE_CLOUD.md](../docs/DEPLOY_ENCORE_CLOUD.md)
