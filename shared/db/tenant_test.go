package db

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsStalePreparedStatement(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "08P01"}
	if !IsStalePreparedStatement(pgErr) {
		t.Fatal("expected pg 08P01 to match")
	}
	if IsStalePreparedStatement(errors.New("other")) {
		t.Fatal("unexpected match")
	}
	if !IsStalePreparedStatement(errors.New("prepared statement did not exist")) {
		t.Fatal("expected message match")
	}
}
