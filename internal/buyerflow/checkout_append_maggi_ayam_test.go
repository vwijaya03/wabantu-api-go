package buyerflow

import (
	"strings"
	"testing"
)

func TestCheckoutAppendMaggiAyamPartialNameNotVariantBrowse(t *testing.T) {
	sim := newMaggiAskRecipientSim(LangID)
	out := sim.Turn("tambah maggi ayam 1 pcs")
	if out.Path != PathOrderFlow {
		t.Fatalf("path=%q want %s reply=%q", out.Path, PathOrderFlow, out.Reply)
	}
	low := strings.ToLower(out.Reply)
	if strings.Contains(out.Reply, "varian yang ada") || strings.Contains(low, "ya kak") {
		t.Fatalf("must not be browse-style variant list: %q", out.Reply)
	}
	if !strings.Contains(out.Reply, "Ringkasan Pesanan") {
		t.Fatalf("expected order summary in ID: %q", out.Reply)
	}
	if out.Order == nil || len(out.Order.Items) < 2 {
		t.Fatalf("expected Maggi Kari + Maggi Ayam lines, order=%+v", out.Order)
	}
	foundAyam := false
	for _, ln := range out.Order.Items {
		if ln.CatalogItemID == "m1" && ln.Qty == 1 {
			foundAyam = true
		}
	}
	if !foundAyam {
		t.Fatalf("Maggi Ayam line missing: %+v", out.Order.Items)
	}
}
