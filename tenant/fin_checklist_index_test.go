package tenant

import (
	"context"
	"testing"

	"encore.app/wabantu/shared/tenantschema"
)

const finChkTestSchema = "t_finchk_idx_test"

func finChkSetup(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("requires tenant database")
	}
	freshTestTenant(t, finChkTestSchema)
	t.Cleanup(func() {
		_, _ = DataDB.Stdlib().ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+quoteIdent(finChkTestSchema)+` CASCADE`)
	})
}

func finChkSQL(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := DataDB.Stdlib().ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func finChkIndex(t *testing.T) bool {
	t.Helper()
	ok, err := tenantschema.IndexExists(context.Background(), DataDB.Stdlib(), finChkTestSchema, "idx_fin_checklist_tpl_date")
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

const finChkT = `"t_finchk_idx_test".`

// insertTemplate returns a new template id.
func finChkTemplate(t *testing.T) string {
	t.Helper()
	var id string
	if err := DataDB.Stdlib().QueryRowContext(context.Background(),
		`INSERT INTO `+finChkT+`fin_checklist_template (title, created_by) VALUES ('t', gen_random_uuid()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestFinChecklistIndex_NewTenantAndOnConflict(t *testing.T) {
	finChkSetup(t)
	if !finChkIndex(t) {
		t.Fatal("new tenant missing idx_fin_checklist_tpl_date")
	}
	ready, err := tenantschema.FinanceModuleReady(context.Background(), DataDB.Stdlib(), finChkTestSchema)
	if err != nil || !ready {
		t.Fatalf("FinanceModuleReady=%v err=%v", ready, err)
	}
	finChkTemplate(t)
	ins := `INSERT INTO ` + finChkT + `fin_checklist_item (template_id, due_date)
		SELECT id, CURRENT_DATE FROM ` + finChkT + `fin_checklist_template WHERE is_active
		ON CONFLICT (template_id, due_date) DO NOTHING`
	finChkSQL(t, ins)
	finChkSQL(t, ins)
	var n int
	_ = DataDB.Stdlib().QueryRowContext(context.Background(), `SELECT count(*) FROM `+finChkT+`fin_checklist_item`).Scan(&n)
	if n != 1 {
		t.Fatalf("want 1 item, got %d", n)
	}
}

func TestFinanceModuleReady_FalseWithoutChecklistIndex(t *testing.T) {
	finChkSetup(t)
	finChkSQL(t, `DROP INDEX `+finChkT+`idx_fin_checklist_tpl_date`)
	ready, err := tenantschema.FinanceModuleReady(context.Background(), DataDB.Stdlib(), finChkTestSchema)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("FinanceModuleReady must be false without idx_fin_checklist_tpl_date")
	}
}

// Drop index + duplicates, run patches: index restored, keep rule applied.
func TestRunSchemaPatches_RestoresChecklistIndexWithKeepRule(t *testing.T) {
	finChkSetup(t)
	finChkSQL(t, `DROP INDEX `+finChkT+`idx_fin_checklist_tpl_date`)
	a, b := finChkTemplate(t), finChkTemplate(t)
	// a: done (older) must beat pending (newer).
	finChkSQL(t, `INSERT INTO `+finChkT+`fin_checklist_item (template_id, due_date, status, completed_at, created_at) VALUES
		($1, '2026-10-01', 'done', now(), now() - interval '2 hour'),
		($1, '2026-10-01', 'pending', NULL, now())`, a)
	// b: both pending, newest wins.
	finChkSQL(t, `INSERT INTO `+finChkT+`fin_checklist_item (template_id, due_date, status, note, created_at) VALUES
		($1, '2026-10-01', 'pending', 'old', now() - interval '1 hour'),
		($1, '2026-10-01', 'pending', 'new', now())`, b)
	// Warn must fire before deletion: at hook time all 4 rows still exist.
	var warned, rowsAtWarn int64 = -1, -1
	prev := warnFinChecklistDuplicates
	warnFinChecklistDuplicates = func(sch string, n int64) {
		if sch == finChkTestSchema {
			warned = n
			_ = DataDB.Stdlib().QueryRowContext(context.Background(), `SELECT count(*) FROM `+finChkT+`fin_checklist_item`).Scan(&rowsAtWarn)
		}
		prev(sch, n)
	}
	t.Cleanup(func() { warnFinChecklistDuplicates = prev })
	if err := RunSchemaPatches(context.Background(), finChkTestSchema); err != nil {
		t.Fatalf("RunSchemaPatches: %v", err)
	}
	if !finChkIndex(t) {
		t.Fatal("index not restored")
	}
	var n int
	_ = DataDB.Stdlib().QueryRowContext(context.Background(), `SELECT count(*) FROM `+finChkT+`fin_checklist_item`).Scan(&n)
	if n != 2 {
		t.Fatalf("want 2 rows after dedupe, got %d", n)
	}
	var st, note string
	_ = DataDB.Stdlib().QueryRowContext(context.Background(), `SELECT status FROM `+finChkT+`fin_checklist_item WHERE template_id=$1`, a).Scan(&st)
	if st != "done" {
		t.Errorf("template a kept %q, want done", st)
	}
	_ = DataDB.Stdlib().QueryRowContext(context.Background(), `SELECT note FROM `+finChkT+`fin_checklist_item WHERE template_id=$1`, b).Scan(&note)
	if note != "new" {
		t.Errorf("template b kept %q, want newest", note)
	}
	if warned != 2 || rowsAtWarn != 4 {
		t.Fatalf("duplicate warn: count=%d rowsAtWarn=%d, want 2 and 4 (logged before delete)", warned, rowsAtWarn)
	}
	// Backup holds exactly the deleted losers.
	var bak int
	var bakA, bakB string
	_ = DataDB.Stdlib().QueryRowContext(context.Background(), `SELECT count(*) FROM `+finChkT+`fin_checklist_item_dedupe_bak WHERE deleted_at IS NOT NULL`).Scan(&bak)
	_ = DataDB.Stdlib().QueryRowContext(context.Background(), `SELECT status FROM `+finChkT+`fin_checklist_item_dedupe_bak WHERE template_id=$1`, a).Scan(&bakA)
	_ = DataDB.Stdlib().QueryRowContext(context.Background(), `SELECT note FROM `+finChkT+`fin_checklist_item_dedupe_bak WHERE template_id=$1`, b).Scan(&bakB)
	if bak != 2 || bakA != "pending" || bakB != "old" {
		t.Fatalf("backup rows=%d a=%q b=%q, want 2 / pending / old", bak, bakA, bakB)
	}
}

// Rerun with index present (v5): patches twice -> no deletion, no warn, no backup table.
func TestRunSchemaPatches_RerunNoopWithIndex(t *testing.T) {
	finChkSetup(t)
	if CurrentSchemaPatchVersion != 5 {
		t.Fatalf("CurrentSchemaPatchVersion=%d want 5", CurrentSchemaPatchVersion)
	}
	a := finChkTemplate(t)
	finChkSQL(t, `INSERT INTO `+finChkT+`fin_checklist_item (template_id, due_date) VALUES ($1, '2026-10-01'), ($1, '2026-10-02')`, a)
	called := false
	prev := warnFinChecklistDuplicates
	warnFinChecklistDuplicates = func(sch string, n int64) {
		if sch == finChkTestSchema {
			called = true
		}
		prev(sch, n)
	}
	t.Cleanup(func() { warnFinChecklistDuplicates = prev })
	for i := 0; i < 2; i++ {
		if err := RunSchemaPatches(context.Background(), finChkTestSchema); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	var n int
	_ = DataDB.Stdlib().QueryRowContext(context.Background(), `SELECT count(*) FROM `+finChkT+`fin_checklist_item`).Scan(&n)
	if n != 2 {
		t.Fatalf("rows changed: %d", n)
	}
	ok, err := tenantschema.TableExists(context.Background(), DataDB.Stdlib(), finChkTestSchema, "fin_checklist_item_dedupe_bak")
	if err != nil || ok {
		t.Fatalf("backup table exists=%v err=%v, want not created", ok, err)
	}
	if called {
		t.Fatal("duplicate warn fired with index present")
	}
}

// Index present: FinChecklistIndexSQL must not delete anything.
func TestFinChecklistIndexSQL_NoopWhenIndexExists(t *testing.T) {
	finChkSetup(t)
	a := finChkTemplate(t)
	finChkSQL(t, `INSERT INTO `+finChkT+`fin_checklist_item (template_id, due_date) VALUES ($1, '2026-10-01'), ($1, '2026-10-02')`, a)
	ctx := context.Background()
	conn, err := DataDB.Stdlib().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SET search_path TO `+quoteIdent(finChkTestSchema)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, tenantschema.FinChecklistIndexSQL); err != nil {
		t.Fatal(err)
	}
	_, _ = conn.ExecContext(ctx, `RESET search_path`)
	var n int
	_ = DataDB.Stdlib().QueryRowContext(ctx, `SELECT count(*) FROM `+finChkT+`fin_checklist_item`).Scan(&n)
	if n != 2 {
		t.Fatalf("rows changed: %d", n)
	}
}
