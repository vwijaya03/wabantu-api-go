package buyerflow

import (
	"strings"
	"testing"
)

func newMaggiAskRecipientSim(savedLang string) *Simulator {
	sim := newOmahSimulator()
	sim.Catalog = maggiCatalog()
	sim.KB = omahKB()
	sim.SavedBuyerLang = savedLang
	sim.Order = &OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "m2",
		ProductName:   "Maggi Kari",
		Qty:           1,
		UnitPrice:     65000,
		SellUnit:      "pcs",
		Items: []OrderLineState{{
			CatalogItemID: "m2",
			ProductName:   "Maggi Kari",
			Qty:           1,
			UnitPrice:     65000,
			SellUnit:      "pcs",
		}},
	}
	syncOrderStateFromItems(sim.Order)
	return sim
}

func TestChattyShippingFAQAtAskRecipientKeepsCart(t *testing.T) {
	cases := []struct {
		name       string
		msg        string
		kb         []KBEntry
		savedEN    bool
		wantNotSub []string
		wantSub    []string
	}{
		{
			name: "id_berapa_lama",
			msg:  "berapa lama pengiriman?",
			wantSub: []string{
				"nama penerima",
			},
		},
		{
			name:    "en_how_long_no_kb",
			msg:     "how long is shipping?",
			kb:      nil,
			savedEN: true,
			wantNotSub: []string{
				"2-3", "2–3",
			},
			wantSub: []string{
				"recipient", "phone",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sim := newWB239488Sim()
			if tc.kb != nil {
				sim.KB = tc.kb
			} else {
				sim.KB = nil
			}
			if tc.savedEN {
				sim.SavedBuyerLang = LangEN
			}
			before := len(sim.Order.Items)
			out := sim.Turn(tc.msg)
			if out.Path != PathShippingFAQ {
				t.Fatalf("path=%q want %s reply=%q", out.Path, PathShippingFAQ, out.Reply)
			}
			if out.Order == nil || len(out.Order.Items) != before {
				t.Fatal("cart must stay during shipping FAQ at ask_recipient")
			}
			low := strings.ToLower(out.Reply)
			for _, sub := range tc.wantSub {
				if !strings.Contains(low, strings.ToLower(sub)) {
					t.Fatalf("reply missing %q: %q", sub, out.Reply)
				}
			}
			for _, sub := range tc.wantNotSub {
				if strings.Contains(out.Reply, sub) {
					t.Fatalf("reply must not contain %q: %q", sub, out.Reply)
				}
			}
		})
	}
}

func TestChattyPaymentAtAskRecipientFromKB(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		lang string
	}{
		{name: "id_cara_bayar", msg: "cara bayar", lang: LangID},
		{name: "en_i_want_to_pay", msg: "I want to pay", lang: LangEN},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sim := newWB239488Sim()
			sim.KB = omahKB()
			sim.SavedBuyerLang = tc.lang
			before := len(sim.Order.Items)
			out := sim.Turn(tc.msg)
			if out.Path != PathPaymentFAQ {
				t.Fatalf("path=%q want %s reply=%q", out.Path, PathPaymentFAQ, out.Reply)
			}
			if out.Path == PathHumanHandoff {
				t.Fatal("payment must not hand off to human")
			}
			low := strings.ToLower(out.Reply)
			if strings.Contains(low, "lunas") {
				t.Fatalf("must not claim paid: %q", out.Reply)
			}
			if !strings.Contains(strings.ToUpper(out.Reply), "BCA") {
				t.Fatalf("expected KB payment info: %q", out.Reply)
			}
			if out.Order == nil || len(out.Order.Items) != before {
				t.Fatal("cart must stay when asking payment mid-checkout")
			}
		})
	}
}

func TestChattyQtyRevisionKeepsSummaryLocale(t *testing.T) {
	t.Run("en_make_it_3", func(t *testing.T) {
		sim := newMaggiAskRecipientSim(LangEN)
		out := sim.Turn("make it 3 pcs")
		if out.Path != PathOrderFlow {
			t.Fatalf("path=%q reply=%q", out.Path, out.Reply)
		}
		if qtyOf(sim.Order, "m2") != 3 {
			t.Fatalf("qty want 3 got %d", qtyOf(sim.Order, "m2"))
		}
		if strings.Contains(out.Reply, "Ringkasan Pesanan") {
			t.Fatalf("EN buyer must not get ID summary: %q", out.Reply)
		}
		if !strings.Contains(out.Reply, "Order Summary") {
			t.Fatalf("missing EN summary: %q", out.Reply)
		}
	})
	t.Run("id_revisi_3", func(t *testing.T) {
		sim := newMaggiAskRecipientSim(LangID)
		out := sim.Turn("revisi jadi 3 pcs")
		if out.Path != PathOrderFlow {
			t.Fatalf("path=%q reply=%q", out.Path, out.Reply)
		}
		if qtyOf(sim.Order, "m2") != 3 {
			t.Fatalf("qty want 3 got %d", qtyOf(sim.Order, "m2"))
		}
		if !strings.Contains(out.Reply, "Ringkasan Pesanan") {
			t.Fatalf("missing ID summary: %q", out.Reply)
		}
	})
}

func TestChattyCancelMaggiLineNotFullCancel(t *testing.T) {
	sim := newWB239488Sim()
	out := sim.Turn("cancel the maggi")
	if out.Canceled || out.Path == PathOrderCancel {
		t.Fatalf("line cancel must not void draft: path=%s canceled=%v", out.Path, out.Canceled)
	}
	if cartHasSKU(out.Order, "maggi-berempah") {
		t.Fatalf("Maggi line should be removed, cart=%+v", out.Order.Items)
	}
	if !cartHasSKU(out.Order, "cad-bar") || !cartHasSKU(out.Order, "durian-biscuit") {
		t.Fatalf("other lines must stay, cart=%+v", out.Order.Items)
	}
}

func TestChattyRecipientPolicyAndHandoffWithCart(t *testing.T) {
	t.Run("buy_for_someone_else", func(t *testing.T) {
		sim := newWB239488Sim()
		sim.SavedBuyerLang = LangEN
		before := len(sim.Order.Items)
		out := sim.Turn("buy for someone else")
		if out.Path != PathRecipientPolicy {
			t.Fatalf("path=%q want %s reply=%q", out.Path, PathRecipientPolicy, out.Reply)
		}
		if out.Order == nil || len(out.Order.Items) != before {
			t.Fatal("cart must remain for recipient policy during checkout")
		}
		if !strings.Contains(strings.ToLower(out.Reply), "someone else") {
			t.Fatalf("expected EN recipient policy: %q", out.Reply)
		}
	})
	t.Run("talk_to_human", func(t *testing.T) {
		sim := newWB239488Sim()
		before := len(sim.Order.Items)
		out := sim.Turn("talk to a human")
		if out.Path != PathHumanHandoff {
			t.Fatalf("path=%q want %s", out.Path, PathHumanHandoff)
		}
		if out.Order == nil || len(out.Order.Items) != before {
			t.Fatal("cart must remain on human handoff")
		}
	})
}
