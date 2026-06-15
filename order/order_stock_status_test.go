package order

import (
	"testing"

	"encore.app/wabantu/inventory"
)

// Status operasional UI (orders/page.tsx) yang harus mengeluarkan stok.
func TestOpsFlowStatusesCommitStock(t *testing.T) {
	cases := []struct {
		code  string
		label string
	}{
		{"processing", "Sedang diproses"},
		{"shipped", "Dalam pengiriman"},
		{"completed", "Selesai"},
	}
	for _, c := range cases {
		if !inventory.IsCommittedOrderStatus(c.code) {
			t.Fatalf("status %q (%s) harus committed (mengurangi stok)", c.code, c.label)
		}
	}
}

func TestNonCommittedStatusesDoNotIssueStock(t *testing.T) {
	for _, status := range []string{"draft", "cancelled", ""} {
		if inventory.IsCommittedOrderStatus(status) {
			t.Fatalf("%q tidak boleh committed", status)
		}
	}
}

// Legacy status tetap committed agar pesanan lama tetap sinkron stok.
func TestLegacyCommittedStatuses(t *testing.T) {
	for _, status := range []string{"confirmed", "paid"} {
		if !inventory.IsCommittedOrderStatus(status) {
			t.Fatalf("legacy %q harus committed", status)
		}
	}
}
