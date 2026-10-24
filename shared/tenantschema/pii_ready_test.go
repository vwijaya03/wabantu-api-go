package tenantschema

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	appdb "encore.app/wabantu/shared/db"
)

// fakeCatalog answers TableExists / ColumnExists from an in-memory schema
// (table -> columns). Pure: no Postgres or Encore runtime needed.
type fakeCatalog struct {
	tables map[string][]string
}

type fakeRow struct {
	v   bool
	err error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*bool)) = r.v
	return nil
}

func (f fakeCatalog) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected exec")
}

func (f fakeCatalog) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (f fakeCatalog) QueryRowContext(_ context.Context, query string, args ...any) appdb.Scannable {
	switch {
	case strings.Contains(query, "pg_attribute") && len(args) == 3:
		cols, ok := f.tables[args[1].(string)]
		if !ok {
			return fakeRow{v: false}
		}
		for _, c := range cols {
			if c == args[2].(string) {
				return fakeRow{v: true}
			}
		}
		return fakeRow{v: false}
	case strings.Contains(query, "pg_class") && len(args) == 2:
		_, ok := f.tables[args[1].(string)]
		return fakeRow{v: ok}
	}
	return fakeRow{err: errors.New("unexpected query")}
}

func basePIITables() map[string][]string {
	return map[string][]string{
		"contact": {"id", "phone_number", "phone_number_enc", "phone_number_idx"},
		"lead":    {"id", "phone_number_enc"},
	}
}

func TestPIIReady_NoEventsModule_ContactLeadOnly(t *testing.T) {
	ok, err := PIIReady(context.Background(), fakeCatalog{tables: basePIITables()}, "t_x")
	if err != nil || !ok {
		t.Fatalf("PIIReady without evt_event_person = %v, %v; want true", ok, err)
	}
}

// Regression: tenant registered with PII patch before evt_* existed (evt_event_person
// without full_name_enc) must be reported NOT ready so the PII patch re-runs.
func TestPIIReady_EventPersonMissingEncColumns(t *testing.T) {
	tables := basePIITables()
	tables["evt_event_person"] = []string{"id", "full_name", "counts_toward_meals"}
	ok, err := PIIReady(context.Background(), fakeCatalog{tables: tables}, "t_x")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("PIIReady = true for evt_event_person without full_name_enc; want false")
	}

	tables["evt_event_person"] = append(tables["evt_event_person"], "full_name_enc")
	ok, _ = PIIReady(context.Background(), fakeCatalog{tables: tables}, "t_x")
	if ok {
		t.Fatal("PIIReady = true without evt_event_person.normalized_name; want false")
	}
}

func TestPIIReady_EventPersonPatched(t *testing.T) {
	tables := basePIITables()
	tables["evt_event_person"] = []string{"id", "full_name", "full_name_enc", "normalized_name"}
	ok, err := PIIReady(context.Background(), fakeCatalog{tables: tables}, "t_x")
	if err != nil || !ok {
		t.Fatalf("PIIReady = %v, %v; want true", ok, err)
	}
}

func TestPIIReady_ContactMissing(t *testing.T) {
	tables := basePIITables()
	tables["contact"] = []string{"id", "phone_number"}
	tables["evt_event_person"] = []string{"full_name_enc", "normalized_name"}
	ok, err := PIIReady(context.Background(), fakeCatalog{tables: tables}, "t_x")
	if err != nil || ok {
		t.Fatalf("PIIReady = %v, %v; want false", ok, err)
	}
}

// The patch PIIReady relies on must actually add the columns it checks.
func TestPIISchemaPatchSQL_AddsEventPersonColumnsChecked(t *testing.T) {
	for _, col := range eventPersonPIIColumns {
		want := "ALTER TABLE evt_event_person ADD COLUMN IF NOT EXISTS " + col
		if !strings.Contains(PIISchemaPatchSQL, want) {
			t.Errorf("PIISchemaPatchSQL missing %q", want)
		}
	}
}
