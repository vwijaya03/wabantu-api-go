package business

import (
	"strings"
	"testing"
)

func TestProfileColsIncludesPaymentFields(t *testing.T) {
	for _, col := range []string{
		"payment_verification_mode",
		"payment_auto_verify_min_confidence",
	} {
		if !strings.Contains(profileCols, col) {
			t.Fatalf("profileCols missing %q", col)
		}
	}
}
