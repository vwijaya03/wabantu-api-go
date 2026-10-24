package apitest

import (
	"context"
	"fmt"
	"testing"

	"encore.app/wabantu/events"
	appdb "encore.app/wabantu/shared/db"
	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/system"
	"encore.app/wabantu/tenant"
)

// tables whose rows (incl. encrypted columns) must be byte-identical across the v4 migration.
var v4SnapshotTables = []string{"evt_event", "evt_event_person", "evt_staff_roster", "evt_volunteer_role", "contact", "business_profile"}

func snapshotTenantRows(ctx context.Context, t *testing.T, schema string) map[string]string {
	t.Helper()
	sch := appdb.SchemaSQL{Schema: schema}
	out := make(map[string]string, len(v4SnapshotTables))
	for _, tbl := range v4SnapshotTables {
		var rows string
		var n int
		if err := tenant.DataDB.Stdlib().QueryRowContext(ctx, fmt.Sprintf(
			`SELECT count(*), COALESCE(string_agg(row_to_json(x)::text, E'\n' ORDER BY row_to_json(x)::text), '')
			 FROM %s x`, sch.T(tbl)),
		).Scan(&n, &rows); err != nil {
			t.Fatalf("snapshot %s: %v", tbl, err)
		}
		out[tbl] = fmt.Sprintf("%d\n%s", n, rows)
	}
	return out
}

func tenantPatchVersion(ctx context.Context, t *testing.T, tenantID string) int {
	t.Helper()
	var v int
	if err := system.DB.QueryRow(ctx,
		`SELECT COALESCE(schema_patch_version,0) FROM tenant_company WHERE tenant_id = $1::uuid`, tenantID,
	).Scan(&v); err != nil {
		t.Fatalf("read schema_patch_version: %v", err)
	}
	return v
}

// v4 (fix/tenant-pii-after-events) re-runs tenant patches via lazy migrate. On an old,
// healthy tenant (v3, PII columns present, encrypted data written) running the migration
// twice must succeed and leave every row — including *_enc ciphertext — unchanged.
func TestSchemaV4Migration_IdempotentOnHealthyOldTenant(t *testing.T) {
	if tenant.CurrentSchemaPatchVersion < 4 {
		t.Fatalf("CurrentSchemaPatchVersion = %d, want >= 4", tenant.CurrentSchemaPatchVersion)
	}
	resetTestTrgm(t)
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)
	ctx := t.Context()

	ev, err := events.CreateEvent(ctx, &events.UpsertEventParams{
		EventName: "V4 Migrate Event",
		EventSlug: "v4-migrate-event",
		StartDate: "2026-12-02",
		EndDate:   "2026-12-02",
		StartTime: "09:00",
		EndTime:   "17:00",
		Status:    "PUBLISHED",
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	sch := appdb.SchemaSQL{Schema: fx.SchemaName}
	var roleID string
	if err := tenant.DataDB.Stdlib().QueryRowContext(ctx, fmt.Sprintf(
		`SELECT id::text FROM %s ORDER BY id LIMIT 1`, sch.T("evt_volunteer_role")),
	).Scan(&roleID); err != nil {
		t.Fatalf("seeded volunteer role: %v", err)
	}
	if _, err := events.CreateEventPerson(ctx, ev.ID, &events.UpsertPersonParams{
		FullName: "Relawan Sintetis V4", Role: "relawan", VolunteerRoleID: &roleID, Notes: "data sintetis",
	}); err != nil {
		t.Fatalf("create person: %v", err)
	}
	if _, err := events.SyncStaffRosterFromEvent(ctx, ev.ID); err != nil {
		t.Fatalf("sync roster: %v", err)
	}

	var encPerson, encRoster int
	_ = tenant.DataDB.Stdlib().QueryRowContext(ctx, fmt.Sprintf(
		`SELECT (SELECT count(*) FROM %s WHERE full_name_enc <> ''), (SELECT count(*) FROM %s WHERE full_name_enc <> '')`,
		sch.T("evt_event_person"), sch.T("evt_staff_roster")),
	).Scan(&encPerson, &encRoster)
	if encPerson == 0 || encRoster == 0 {
		t.Fatalf("fixture must contain encrypted rows (person=%d roster=%d)", encPerson, encRoster)
	}
	if ok, err := tenantschema.PIIReady(ctx, tenant.DataDB.Stdlib(), fx.SchemaName); err != nil || !ok {
		t.Fatalf("healthy tenant PIIReady = %v, %v", ok, err)
	}

	before := snapshotTenantRows(ctx, t, fx.SchemaName)

	// Pretend the tenant is an old v3 tenant so the v4 migration actually runs.
	if _, err := system.DB.Exec(ctx,
		`UPDATE tenant_company SET schema_patch_version = 3 WHERE tenant_id = $1::uuid`, fx.TenantID,
	); err != nil {
		t.Fatalf("set v3: %v", err)
	}
	for i := 1; i <= 2; i++ {
		if err := tenant.ProcessTenantSchemaMigration(ctx, fx.TenantID, fx.SchemaName, ""); err != nil {
			t.Fatalf("v4 migration run %d: %v", i, err)
		}
		if v := tenantPatchVersion(ctx, t, fx.TenantID); v != tenant.CurrentSchemaPatchVersion {
			t.Fatalf("run %d: schema_patch_version = %d, want %d", i, v, tenant.CurrentSchemaPatchVersion)
		}
		after := snapshotTenantRows(ctx, t, fx.SchemaName)
		for _, tbl := range v4SnapshotTables {
			if before[tbl] != after[tbl] {
				t.Errorf("run %d: rows of %s changed by migration", i, tbl)
			}
		}
	}
}

// resetTestTrgm: the Encore test cluster has no pg_trgm preinstalled and the test role
// cannot CREATE in public, so the first tenant schema running the always-apply patch
// (CREATE EXTENSION IF NOT EXISTS pg_trgm) owns the extension and later schemas fail with
// "gin_trgm_ops does not exist" (known limitation, see internal/apitest/README.md).
// Test-only, throwaway test DB: drop it so the schema under test re-creates and owns it.
func resetTestTrgm(t *testing.T) {
	t.Helper()
	if _, err := tenant.DataDB.Stdlib().ExecContext(context.Background(),
		`DROP EXTENSION IF EXISTS pg_trgm CASCADE`); err != nil {
		t.Fatalf("reset pg_trgm (test DB): %v", err)
	}
}
