package tenant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"encore.app/wabantu/shared/tenantschema"
)

const trgmQuotedTestSchema = `trgm"x`

// encoreTestDBNameRe matches Encore's per-app test databases ("<db>-<localAppID>-test",
// e.g. tenant-xt8v2-test). The QA stack uses "*-run" (tenant-a25f8-run), e2e uses
// wabantu_e2e — both must be refused.
var encoreTestDBNameRe = regexp.MustCompile(`^tenant-[a-z0-9]{4,}-test$`)

func checkEncoreTestDBName(name string) error {
	if !encoreTestDBNameRe.MatchString(name) {
		return fmt.Errorf("refusing destructive test DDL on database %q: not an Encore test DB (want tenant-<id>-test)", name)
	}
	return nil
}

// requireEncoreTestDB hard-fails (never skips) unless connected to the Encore test DB.
func requireEncoreTestDB(t *testing.T) {
	t.Helper()
	var name string
	if err := DataDB.Stdlib().QueryRowContext(context.Background(), `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatalf("current_database(): %v", err)
	}
	if err := checkEncoreTestDBName(name); err != nil {
		t.Fatal(err)
	}
}

func TestCheckEncoreTestDBName(t *testing.T) {
	for _, ok := range []string{"tenant-xt8v2-test", "tenant-a25f8-test"} {
		if err := checkEncoreTestDBName(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"wabantu_e2e", "tenant-a25f8-run", "system-a25f8-run", "tenant", "postgres",
		"random_db_name", "tenant-xt8v2-test-prod", "x-tenant-xt8v2-test", "tenant-xt8v2-test\n"} {
		if err := checkEncoreTestDBName(bad); err == nil {
			t.Errorf("%q accepted; must be refused", bad)
		}
	}
}

func TestCatalogTrigramIndexSQL_QualifiesAndQuotesSchema(t *testing.T) {
	cases := map[string]string{
		"public":                     `(name "public".gin_trgm_ops)`,
		"t_acme":                     `(name "t_acme".gin_trgm_ops)`,
		`trgm"x`:                     `(name "trgm""x".gin_trgm_ops)`,
		`x"; DROP TABLE contact; --`: `(name "x""; DROP TABLE contact; --".gin_trgm_ops)`,
	}
	for schema, want := range cases {
		got := catalogTrigramIndexSQL(schema)
		if !strings.Contains(got, want) {
			t.Errorf("catalogTrigramIndexSQL(%q) = %q; want %q", schema, got, want)
		}
		if !strings.Contains(got, "CREATE INDEX IF NOT EXISTS idx_catalog_name_trgm") {
			t.Errorf("not idempotent: %q", got)
		}
	}
	if !strings.Contains(createTrgmExtensionSQL, "SCHEMA public") {
		t.Errorf("createTrgmExtensionSQL must pin pg_trgm to public: %q", createTrgmExtensionSQL)
	}
	if strings.Contains(tenantSchemaPatchSQL, "gin_trgm_ops") || strings.Contains(strings.ToUpper(tenantSchemaPatchSQL), "CREATE EXTENSION") {
		t.Error("tenantSchemaPatchSQL must not create pg_trgm / unqualified gin_trgm_ops")
	}
}

// setTestTrgmState puts pg_trgm in extSchema ("" = absent) — Encore test DB only.
func setTestTrgmState(t *testing.T, extSchema string) {
	t.Helper()
	requireEncoreTestDB(t)
	ctx := context.Background()
	stmts := []string{`DROP EXTENSION IF EXISTS pg_trgm CASCADE`}
	if extSchema != "" {
		q := quoteIdent(extSchema)
		stmts = append(stmts, `CREATE SCHEMA IF NOT EXISTS `+q, `CREATE EXTENSION pg_trgm SCHEMA `+q)
	}
	for _, s := range stmts {
		if _, err := DataDB.Stdlib().ExecContext(ctx, s); err != nil {
			t.Fatalf("test DB %q: %v", s, err)
		}
	}
}

func freshTestTenant(t *testing.T, schema string) {
	t.Helper()
	requireEncoreTestDB(t)
	if !schemaNameRe.MatchString(schema) {
		t.Fatalf("invalid tenant schema %q", schema)
	}
	ctx := context.Background()
	if _, err := DataDB.Stdlib().ExecContext(ctx, `DROP SCHEMA IF EXISTS `+quoteIdent(schema)+` CASCADE`); err != nil {
		t.Fatalf("drop test schema: %v", err)
	}
	if err := RunTenantDDL(ctx, schema); err != nil {
		t.Fatalf("RunTenantDDL: %v", err)
	}
}

func catalogTrgmIndexDef(t *testing.T, schema string) string {
	t.Helper()
	var def string
	err := DataDB.Stdlib().QueryRowContext(context.Background(),
		`SELECT indexdef FROM pg_indexes WHERE schemaname = $1 AND indexname = 'idx_catalog_name_trgm'`, schema,
	).Scan(&def)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("read indexdef: %v", err)
	}
	return def
}

// pg_trgm in a non-public schema whose name needs quoting: patches succeed (twice) and
// the index uses the schema-qualified, correctly quoted opclass.
func TestRunSchemaPatches_TrgmInQuotedNonPublicSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("requires tenant database")
	}
	setTestTrgmState(t, trgmQuotedTestSchema)
	schema := "t_test_trgm_qualified"
	freshTestTenant(t, schema)
	ctx := context.Background()
	for i := 1; i <= 2; i++ {
		if err := RunSchemaPatches(ctx, schema); err != nil {
			t.Fatalf("RunSchemaPatches run %d: %v", i, err)
		}
	}
	def := catalogTrgmIndexDef(t, schema)
	if !strings.Contains(def, `"trgm""x".gin_trgm_ops`) {
		t.Fatalf("idx_catalog_name_trgm = %q; want opclass \"trgm\"\"x\".gin_trgm_ops", def)
	}
	if ok, err := tenantschema.PIIReady(ctx, DataDB.Stdlib(), schema); err != nil || !ok {
		t.Fatalf("PIIReady = %v, %v", ok, err)
	}
}

// pg_trgm absent and cannot be created: trigram index skipped with a warning, PII
// columns of an affected tenant are still added, no error.
func TestRunSchemaPatches_TrgmAbsentSkipsIndexKeepsPII(t *testing.T) {
	if testing.Short() {
		t.Skip("requires tenant database")
	}
	setTestTrgmState(t, "")
	orig := createTrgmExtension
	createTrgmExtension = func(context.Context, *sql.Conn) error {
		return errors.New("permission denied to create extension \"pg_trgm\" (simulated)")
	}
	t.Cleanup(func() { createTrgmExtension = orig })

	schema := "t_test_trgm_absent"
	freshTestTenant(t, schema)
	ctx := context.Background()
	if _, err := DataDB.Stdlib().ExecContext(ctx, fmt.Sprintf(`
		ALTER TABLE %[1]s.evt_event_person DROP COLUMN IF EXISTS full_name_enc, DROP COLUMN IF EXISTS normalized_name;`,
		quoteIdent(schema))); err != nil {
		t.Fatalf("simulate affected tenant: %v", err)
	}
	for i := 1; i <= 2; i++ {
		if err := RunSchemaPatches(ctx, schema); err != nil {
			t.Fatalf("RunSchemaPatches run %d with pg_trgm absent: %v", i, err)
		}
	}
	if def := catalogTrgmIndexDef(t, schema); def != "" {
		t.Fatalf("trigram index created without pg_trgm: %q", def)
	}
	for _, col := range []string{"full_name_enc", "normalized_name"} {
		ok, err := tenantschema.ColumnExists(ctx, DataDB.Stdlib(), schema, "evt_event_person", col)
		if err != nil || !ok {
			t.Fatalf("evt_event_person.%s = %v, %v; PII patch must not depend on pg_trgm", col, ok, err)
		}
	}
	if ok, err := tenantschema.PIIReady(ctx, DataDB.Stdlib(), schema); err != nil || !ok {
		t.Fatalf("PIIReady = %v, %v", ok, err)
	}
}
