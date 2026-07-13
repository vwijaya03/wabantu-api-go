package inbox

import (
	"errors"
	"testing"

	"encore.dev/beta/errs"
)

func TestIsNoRow(t *testing.T) {
	if isNoRow(nil) {
		t.Fatal("nil should not be no row")
	}
	if !isNoRow(&errs.Error{Code: errs.NotFound, Message: "not_found: sql: no rows in result set"}) {
		t.Fatal("expected encore NotFound to be no row")
	}
	if isNoRow(errors.New("connection reset")) {
		t.Fatal("unrelated error should not be no row")
	}
}
