package tenant

import (
	"context"
	"fmt"
	"testing"

	"encore.app/wabantu/shared/tenantschema"
)

func blockIndex(t *testing.T, label string) int {
	t.Helper()
	for i, b := range cloudAdminTenantDDLBlocks() {
		if b.label == label {
			return i
		}
	}
	t.Fatalf("cloudAdminTenantDDLBlocks has no %q block", label)
	return -1
}

// Regression: PIISchemaPatchSQL skips evt_* tables that do not exist yet, so on cloud
// it must run after the events (and finance) DDL in the same admin transaction.
func TestCloudAdminDDL_PIIRunsAfterEventsAndFinance(t *testing.T) {
	pii := blockIndex(t, "pii patch")
	for _, before := range []string{"events patch", "finance patch"} {
		if idx := blockIndex(t, before); pii < idx {
			t.Errorf("pii patch (#%d) runs before %s (#%d)", pii, before, idx)
		}
	}
	if last := len(cloudAdminTenantDDLBlocks()) - 1; pii != last {
		t.Errorf("pii patch must be the last cloud admin DDL block, got #%d of %d", pii, last)
	}
}

// Regression: signup (runTenantBootstrapPatches) and migrate (RunSchemaPatches) create
// evt_* inside runFinanceSchemaAndSeed; the PII step must come after it.
func TestTenantPatchSteps_PIIIsLast(t *testing.T) {
	steps := tenantPatchSteps()
	idx := map[string]int{}
	for i, s := range steps {
		idx[s.name] = i
		if s.run == nil {
			t.Errorf("step %q has nil run", s.name)
		}
	}
	pii, ok := idx["pii"]
	if !ok {
		t.Fatal("tenantPatchSteps has no pii step")
	}
	events, ok := idx["finance+events+inventory"]
	if !ok {
		t.Fatal("tenantPatchSteps has no finance+events+inventory step")
	}
	if pii < events || pii != len(steps)-1 {
		t.Fatalf("pii step at #%d, events at #%d, total %d: pii must be last", pii, events, len(steps))
	}
}

func evtPersonHasPIIColumns(ctx context.Context, t *testing.T, schema string) bool {
	t.Helper()
	db := DataDB.Stdlib()
	for _, tbl := range []string{"evt_event_person", "evt_staff_roster"} {
		ok, err := tenantschema.ColumnExists(ctx, db, schema, tbl, "full_name_enc")
		if err != nil {
			t.Fatalf("ColumnExists %s: %v", tbl, err)
		}
		if !ok {
			return false
		}
	}
	return true
}

// DB: new tenant via the signup DDL path ends with evt_* PII columns and PIIReady=true.
func TestRunTenantDDL_NewTenantHasEventPersonPIIColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("requires tenant database")
	}
	ctx := context.Background()
	schema := "t_test_pii_after_events"
	if err := RunTenantDDL(ctx, schema); err != nil {
		t.Fatalf("RunTenantDDL: %v", err)
	}
	if !evtPersonHasPIIColumns(ctx, t, schema) {
		t.Fatal("new tenant: evt_event_person/evt_staff_roster missing full_name_enc after signup DDL")
	}
	ready, err := tenantschema.PIIReady(ctx, DataDB.Stdlib(), schema)
	if err != nil || !ready {
		t.Fatalf("PIIReady after signup = %v, %v; want true", ready, err)
	}
}

// DB: an old tenant already hit by the bug (evt_* without PII columns) is repaired by
// re-running the patches, and re-running again is a no-op (idempotent).
func TestRunSchemaPatches_RepairsAffectedTenantIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("requires tenant database")
	}
	ctx := context.Background()
	schema := "t_test_pii_repair"
	if err := RunTenantDDL(ctx, schema); err != nil {
		t.Fatalf("RunTenantDDL: %v", err)
	}
	// Simulate the pre-fix state: evt_* created without PII columns.
	if _, err := DataDB.Stdlib().ExecContext(ctx, fmt.Sprintf(`
		DROP INDEX IF EXISTS %[1]s.idx_evt_staff_roster_active;
		ALTER TABLE %[1]s.evt_event_person DROP COLUMN IF EXISTS full_name_enc, DROP COLUMN IF EXISTS normalized_name;
		ALTER TABLE %[1]s.evt_staff_roster DROP COLUMN IF EXISTS full_name_enc, DROP COLUMN IF EXISTS normalized_name_idx;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_evt_staff_roster_active
		    ON %[1]s.evt_staff_roster(normalized_name, person_type) WHERE deleted_at IS NULL;`,
		quoteIdent(schema))); err != nil {
		t.Fatalf("simulate affected tenant: %v", err)
	}
	ready, err := tenantschema.PIIReady(ctx, DataDB.Stdlib(), schema)
	if err != nil || ready {
		t.Fatalf("PIIReady on affected tenant = %v, %v; want false", ready, err)
	}
	for i := 1; i <= 2; i++ {
		if err := RunSchemaPatches(ctx, schema); err != nil {
			t.Fatalf("RunSchemaPatches run %d: %v", i, err)
		}
		if !evtPersonHasPIIColumns(ctx, t, schema) {
			t.Fatalf("run %d: evt_* PII columns not restored", i)
		}
		ready, err = tenantschema.PIIReady(ctx, DataDB.Stdlib(), schema)
		if err != nil || !ready {
			t.Fatalf("run %d: PIIReady = %v, %v; want true", i, ready, err)
		}
	}
}
