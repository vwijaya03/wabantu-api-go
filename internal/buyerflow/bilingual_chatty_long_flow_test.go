package buyerflow

import (
	"strings"
	"testing"
)

// TestChattyBilingualLongFlowMultiTurn — satu skrip QA percakapan panjang (EN→ID) pada fixture Maggi.
func TestChattyBilingualLongFlowMultiTurn(t *testing.T) {
	sim := newOmahSimulator()
	sim.Catalog = maggiCatalog()
	sim.KB = omahPaymentKB() // shipping FAQ tanpa angka hari di checkout aktif

	type step struct {
		msg        string
		wantPath   string
		wantSub    []string
		wantNotSub []string
		afterEN    bool // sebelum switch ke ID, balasan harus EN (bukan template ID)
		afterID    bool
		checkQty   int  // 0 = skip
		wantCart   bool // cart harus ada isi
	}

	steps := []step{
		{msg: "hi", wantPath: PathBuyerLanguage, wantSub: []string{"English", "Indonesia"}},
		{msg: "1", wantPath: PathBuyerLanguage, wantSub: []string{"English"}},
		{
			msg:      "how many maggi variants do you have?",
			wantPath: PathCatalogDB,
			afterEN:  true,
			wantSub:  []string{"variant"},
			wantNotSub: []string{
				"ya kak", "varian yang ada", "Ringkasan Pesanan",
			},
		},
		{
			msg:      "maggi kari 1 pcs",
			wantPath: PathOrderFlow,
			afterEN:  true,
			wantCart: true,
			wantSub:  []string{"Order Summary"},
			wantNotSub: []string{
				"Ringkasan Pesanan", "Siap kak",
			},
		},
		{
			msg:      "how long is shipping?",
			wantPath: PathShippingFAQ,
			afterEN:  true,
			wantCart: true,
			wantNotSub: []string{
				"2-3", "2–3", "Ringkasan Pesanan",
			},
			wantSub: []string{"recipient", "phone"},
		},
		{
			msg:      "make it 2 pcs",
			wantPath: PathOrderFlow,
			afterEN:  true,
			wantCart: true,
			wantSub:  []string{"Order Summary"},
			wantNotSub: []string{
				"Ringkasan Pesanan",
			},
			checkQty: 2,
		},
		{
			msg:      "please use bahasa indonesia",
			wantPath: PathBuyerLanguage,
			wantSub:  []string{"Bahasa Indonesia"},
		},
		{
			msg:      "Maggi Ayam Berempah 1 pcs",
			wantPath: PathOrderFlow,
			afterID:  true,
			wantCart: true,
			wantSub:  []string{"Ringkasan Pesanan"},
			wantNotSub: []string{
				"Order Summary",
			},
		},
		{
			msg:      "cara bayar",
			wantPath: PathPaymentFAQ,
			afterID:  true,
			wantCart: true,
			wantSub:  []string{"BCA"},
			wantNotSub: []string{
				"lunas", "Order Summary",
			},
		},
	}

	for i, st := range steps {
		out := sim.Turn(st.msg)
		if out.Path != st.wantPath {
			t.Fatalf("turn %d path=%q want %s reply=%q", i+1, out.Path, st.wantPath, out.Reply)
		}
		low := strings.ToLower(out.Reply)
		for _, sub := range st.wantSub {
			if !strings.Contains(low, strings.ToLower(sub)) && !strings.Contains(out.Reply, sub) {
				t.Fatalf("turn %d missing %q in reply: %q", i+1, sub, out.Reply)
			}
		}
		for _, sub := range st.wantNotSub {
			if strings.Contains(out.Reply, sub) || strings.Contains(low, strings.ToLower(sub)) {
				t.Fatalf("turn %d must not contain %q: %q", i+1, sub, out.Reply)
			}
		}
		if st.afterEN && sim.SavedBuyerLang != LangEN {
			t.Fatalf("turn %d: expected saved EN, got %q", i+1, sim.SavedBuyerLang)
		}
		if st.afterID && sim.SavedBuyerLang != LangID {
			t.Fatalf("turn %d: expected saved ID, got %q", i+1, sim.SavedBuyerLang)
		}
		if st.checkQty > 0 {
			if sim.Order == nil || qtyOf(sim.Order, "m2") != st.checkQty {
				t.Fatalf("turn %d: qty want %d order=%+v", i+1, st.checkQty, sim.Order)
			}
		}
		if st.wantCart && !simHasDraftCart(out.Order) {
			t.Fatalf("turn %d: cart must not be empty path=%s reply=%q order=%v", i+1, out.Path, out.Reply, out.Order)
		}
		t.Logf("turn %d path=%s reply=%q", i+1, out.Path, truncateReply(out.Reply, 200))
	}
}

func simHasDraftCart(o *OrderState) bool {
	if o == nil {
		return false
	}
	if len(o.Items) > 0 {
		return true
	}
	return o.Qty > 0 && o.ProductName != ""
}

func truncateReply(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
