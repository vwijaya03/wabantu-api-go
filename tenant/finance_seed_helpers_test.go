package tenant

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsPgUniqueViolation(t *testing.T) {
	t.Parallel()
	if !isPgUniqueViolation(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("expected 23505 pg error")
	}
	if isPgUniqueViolation(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("unexpected foreign key as unique")
	}
	if !isPgUniqueViolation(errors.New(`ERROR: duplicate key value violates unique constraint "idx_fin_cat_sys_child_name_parent" (SQLSTATE 23505)`)) {
		t.Fatal("expected duplicate key message")
	}
	if isPgUniqueViolation(nil) {
		t.Fatal("nil is not unique violation")
	}
}
