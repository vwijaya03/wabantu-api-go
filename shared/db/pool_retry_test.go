package db

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPoolQueryRowRetryOnStalePreparedStatement(t *testing.T) {
	err := &pgconn.PgError{Code: "08P01"}
	if !IsStalePreparedStatement(err) {
		t.Fatal("expected 08P01 to be stale prepared statement")
	}
}

func TestRetryRowImplementsScannable(t *testing.T) {
	var _ Scannable = retryRow{}
}
