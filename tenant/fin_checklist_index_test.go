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
