package tenant

// CurrentSchemaPatchVersion is bumped whenever tenant/schema_patch.go (or related
// always-apply patches) changes. Workers only process tenants below this version.
//
// v4 (2026-10-24, fix/tenant-pii-after-events): re-run patches so tenants whose PII
// patch ran before evt_* existed get evt_event_person / evt_staff_roster PII columns.
//
// v5 (2026-10-24, fix/finance-checklist-index): re-run patches so v4 tenants missing
// idx_fin_checklist_tpl_date get it (dedupe + backup) via lazy migrate.
const CurrentSchemaPatchVersion = 5
