package inventory

import "testing"

func TestCoalesceStr(t *testing.T) {
	if coalesceStr("a", "b") != "a" {
		t.Fatal("non-empty first")
	}
	if coalesceStr("", "b") != "b" {
		t.Fatal("fallback second")
	}
	if coalesceStr("", "") != "" {
		t.Fatal("both empty")
	}
}

func TestTxnKindRefType(t *testing.T) {
	if got := txnKindRefType(TxnKindAdjustment); got != TxnKindAdjustment {
		t.Fatalf("adjustment ref = %q", got)
	}
	if got := txnKindRefType("unknown"); got != "unknown" {
		t.Fatalf("passthrough = %q", got)
	}
}

func TestRefTypeLabel(t *testing.T) {
	cases := map[string]string{
		"bill":            "Penerimaan (Bill)",
		TxnKindTransfer:   "Transfer",
		"sales_return":    "Retur Penjualan",
		"":                "",
		"custom":          "custom",
	}
	for in, want := range cases {
		if got := refTypeLabel(in); got != want {
			t.Fatalf("refTypeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
