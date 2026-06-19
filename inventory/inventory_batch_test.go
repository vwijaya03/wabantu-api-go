package inventory

import "testing"

func TestIsInvoiceEligibleStatus(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"shipped", true},
		{"completed", true},
		{"SHIPPED", true},
		{" Completed ", true},
		{"processing", false},
		{"draft", false},
		{"cancelled", false},
	}
	for _, c := range cases {
		if got := isInvoiceEligibleStatus(c.in); got != c.want {
			t.Errorf("isInvoiceEligibleStatus(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestReturnableQtyNeverNegative(t *testing.T) {
	if got := returnableQty(5, 10); got != 0 {
		t.Fatalf("returnableQty = %v, want 0", got)
	}
	if got := returnableQty(5, 2); got != 3 {
		t.Fatalf("returnableQty = %v, want 3", got)
	}
}
