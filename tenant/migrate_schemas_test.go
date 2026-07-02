package tenant

import (
	"context"
	"testing"
)

type fakeMigrationRows struct {
	rows []SchemaMigrationTarget
	i    int
	err  error
}

func (f *fakeMigrationRows) Next() bool {
	if f.err != nil {
		return false
	}
	return f.i < len(f.rows)
}

func (f *fakeMigrationRows) Scan(dest ...any) error {
	if f.i >= len(f.rows) {
		return f.err
	}
	*(dest[0].(*string)) = f.rows[f.i].TenantID
	*(dest[1].(*string)) = f.rows[f.i].SchemaName
	f.i++
	return nil
}

func (f *fakeMigrationRows) Err() error {
	return f.err
}

func TestScanSchemaMigrationTargets(t *testing.T) {
	rows := &fakeMigrationRows{
		rows: []SchemaMigrationTarget{
			{TenantID: "a", SchemaName: "t_one"},
			{TenantID: "b", SchemaName: "t_two"},
		},
	}
	got, err := scanSchemaMigrationTargets(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SchemaName != "t_one" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestResolveSchemaMigrationTargetsEmptyIDs(t *testing.T) {
	// listAll requires DB — only test partial path validation
	_, err := listSchemaMigrationTargetsByTenantIDs(context.Background(), []string{"", "  "})
	if err == nil {
		t.Fatal("expected error for empty ids")
	}
}
