package flag

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

func TestLoadFlagTreatsWrappedErrNoRowsAsMissing(t *testing.T) {
	wrapped := fmt.Errorf("encore db: %w", sql.ErrNoRows)
	if wrapped == sql.ErrNoRows {
		t.Fatal("direct equality must not match wrapped ErrNoRows")
	}
	if !errors.Is(wrapped, sql.ErrNoRows) {
		t.Fatal("errors.Is must detect wrapped ErrNoRows")
	}
}
