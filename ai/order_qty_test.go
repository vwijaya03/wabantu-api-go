package ai

import "testing"

func TestParseOrderQtyPaket(t *testing.T) {
	cases := map[string]int{
		"eh mau beli 3 paket bang":  3,
		"3 pakettt":                 3,
		"bukan revisi, saya order 3 paket bukan 1 paket": 3,
	}
	for msg, want := range cases {
		q, ok := parseOrderQty(msg)
		if !ok || q != want {
			t.Fatalf("parseOrderQty(%q) = %d ok=%v, want %d", msg, q, ok, want)
		}
	}
}

func TestIsOrderRevisionMessage(t *testing.T) {
	if !IsOrderRevisionMessage("bukan revisi, saya order 3 paket bukan 1 paket") {
		t.Fatal("expected order revision")
	}
	if IsPricingUnitClarification("bukan revisi, saya order 3 paket bukan 1 paket") {
		t.Fatal("revision should not be pricing clarification")
	}
	for _, msg := range []string{
		"revisi jadi 10 pakettt",
		"loh, ubah jadi 10 paket",
		"gw mau ubah jadi 10 paket bisa ?",
	} {
		if !IsOrderRevisionMessage(msg) {
			t.Fatalf("expected revision: %q", msg)
		}
	}
}

func TestIsCasualPraiseInScope(t *testing.T) {
	scope := ExtractScopeKeywords("Omah Apparel boxer jeans")
	if !IsWithinBusinessScope("nah pinter lu udah", scope, nil) {
		t.Fatal("praise should stay in business scope")
	}
}

func TestInferVariantFromProductName(t *testing.T) {
	st := orderState{ProductName: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L"}
	inferVariantFromProductName(&st)
	if st.Size != "L" {
		t.Fatalf("expected size L, got %q", st.Size)
	}
}
