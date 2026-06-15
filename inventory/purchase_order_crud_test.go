package inventory

import "testing"

func TestPurchaseOrderDeletable(t *testing.T) {
	cases := []struct {
		status      string
		hasReceipts bool
		hasBills    bool
		want        bool
	}{
		{"open", false, false, true},
		{"cancelled", false, false, true},
		{"open", true, false, false},
		{"open", false, true, false},
		{"partial", false, false, false},
		{"received", false, false, false},
		{"closed", false, false, false},
	}
	for _, c := range cases {
		if got := purchaseOrderDeletable(c.status, c.hasReceipts, c.hasBills); got != c.want {
			t.Fatalf("purchaseOrderDeletable(%q,%v,%v) = %v, want %v",
				c.status, c.hasReceipts, c.hasBills, got, c.want)
		}
	}
}

func TestPurchaseOrderEditable(t *testing.T) {
	if purchaseOrderEditable("open", false) != true {
		t.Fatal("open without receipts should be editable")
	}
	if purchaseOrderEditable("open", true) != false {
		t.Fatal("open with receipts not editable")
	}
	if purchaseOrderEditable("partial", false) != false {
		t.Fatal("partial not editable")
	}
}
