package ai

import (
	"strings"
	"testing"
)

// --- parseOrderQty (~28 cases) ---

func TestMatrix_ParseOrderQty(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"eh mau beli 3 paket bang", 3},
		{"3 pakettt", 3},
		{"revisi jadi 10 pakettt", 10},
		{"loh, ubah jadi 10 paket", 10},
		{"gw mau ubah jadi 10 paket bisa ?", 10},
		{"mau 1 paket ya boxer mono spot nya", 1},
		{"bukan revisi, saya order 3 paket bukan 1 paket", 3},
		{"2 biji", 2},
		{"5 pcs", 5},
		{"1 piece", 1},
		{"dua pcs", 2},
		{"sepuluh paket", 10},
		{"mau beli abon sapi 1 lusin", 12},
		{"3", 3},
		{"qty 4", 4},
		{"jumlah: 6", 6},
		{"mau order\n1PCS CELANA DALAM - L\n\n2 piece bisa?", 2},
		{"1PCS CELANA DALAM BOXER ANAK PEREMPUAN MOTIF HELLO KITTY - L\n\n2 biji", 2},
		{"beli 7 buah", 7},
		{"order 15 unit", 15},
		{"satu paket", 1},
		{"tiga paket dong", 3},
		{"10 paket ya", 10},
		{"mau pesan 2 lusin", 24},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			q, ok := parseOrderQty(tc.in)
			if !ok || q != tc.want {
				t.Fatalf("parseOrderQty(%q) = %d ok=%v, want %d", tc.in, q, ok, tc.want)
			}
		})
	}
}

func TestMatrix_ParseOrderQty_negative(t *testing.T) {
	neg := []string{
		"1PCS CELANA DALAM BOXER",
		"boxer mono spot ada ga",
		"harga berapa",
		"",
	}
	for _, msg := range neg {
		t.Run(msg, func(t *testing.T) {
			if _, ok := parseOrderQty(msg); ok {
				t.Fatalf("should not parse qty from %q", msg)
			}
		})
	}
}

// --- order revision (~14 cases) ---

func TestMatrix_IsOrderRevision(t *testing.T) {
	yes := []string{
		"bukan revisi, saya order 3 paket bukan 1 paket",
		"revisi jadi 10 pakettt",
		"loh, ubah jadi 10 paket",
		"gw mau ubah jadi 10 paket bisa ?",
		"ganti jadi 5 paket",
		"order 3 paket bukan 1",
		"bukan 1 paket mau 3 paket",
	}
	for _, msg := range yes {
		t.Run(msg, func(t *testing.T) {
			if !IsOrderRevisionMessage(msg) {
				t.Fatal("expected revision")
			}
		})
	}
}

func TestMatrix_IsNotOrderRevision(t *testing.T) {
	no := []string{
		"boxer bisa beli per biji ga ?",
		"harga per paket berapa",
		"satu paket isi berapa pcs",
		"mau beli boxer cd pria bisa ?",
	}
	for _, msg := range no {
		t.Run(msg, func(t *testing.T) {
			if IsOrderRevisionMessage(msg) {
				t.Fatal("should not be order revision")
			}
		})
	}
}

func TestMatrix_PricingVsRevision(t *testing.T) {
	cases := []struct {
		msg      string
		pricing  bool
		revision bool
	}{
		{"boxer pria mono spot bisa beli per biji ga ?", true, false},
		{"bukan revisi, saya order 3 paket bukan 1 paket", false, true},
		{"revisi jadi 10 pakettt", false, true},
		{"harga per paket atau per biji", true, false},
		{"paket isi berapa", true, false},
		{"loh, ubah jadi 10 paket", false, true},
		{"ga jadi mau dirubah menjadi 10 biji ya", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			gotP := IsPricingUnitClarification(tc.msg)
			gotR := IsOrderRevisionMessage(tc.msg)
			if gotP != tc.pricing {
				t.Fatalf("pricing=%v want %v", gotP, tc.pricing)
			}
			if gotR != tc.revision {
				t.Fatalf("revision=%v want %v", gotR, tc.revision)
			}
		})
	}
}

// --- qty revision helper (~6 cases) ---

func TestMatrix_TryApplyQtyRevision(t *testing.T) {
	cases := []struct {
		msg      string
		before   int
		after    int
		changed  bool
	}{
		{"revisi jadi 10 pakettt", 1, 10, true},
		{"loh, ubah jadi 10 paket", 3, 10, true},
		{"gw mau ubah jadi 10 paket bisa ?", 1, 10, true},
		{"Nama: Budi\nHP: 081234567890", 1, 1, false},
		{"boxer mono spot ada ga", 1, 1, false},
		{"ganti jadi 5 paket", 10, 5, true},
		{"ga jadi mau dirubah menjadi 10 biji ya", 1, 10, true},
	}
	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			st := orderState{Qty: tc.before, ProductName: "[3 PCS] BOXER - L", UnitPrice: 56900}
			changed := tryApplyQtyRevision(&st, tc.msg)
			if changed != tc.changed {
				t.Fatalf("changed=%v want %v", changed, tc.changed)
			}
			if st.Qty != tc.after {
				t.Fatalf("qty=%d want %d", st.Qty, tc.after)
			}
		})
	}
}

func TestMatrix_QtyRevisionUpdatesSummary(t *testing.T) {
	st := orderState{
		ProductName: "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L",
		Qty:         1,
		UnitPrice:   56900,
		SellUnit:    "pcs",
		Size:        "L",
	}
	if !tryApplyQtyRevision(&st, "revisi jadi 10 pakettt") {
		t.Fatal("expected revision applied")
	}
	summary := formatOrderSummary(st)
	if !strings.Contains(summary, "Qty: 10") || !strings.Contains(summary, "569000") {
		t.Fatalf("expected qty 10 subtotal 569000: %s", summary)
	}
}

// --- ResolveSalesIntent (~22 cases) ---

func TestMatrix_ResolveSalesIntent(t *testing.T) {
	p := omahProfile()
	cat := omahCatalog()
	hist := boxerHistory()

	cases := []struct {
		msg   string
		state string
		topic string
	}{
		{"jualan apa aja lu", SalesStateBrowsing, SalesTopicList},
		{"boxer cowok ada ga", SalesStateProductSelected, SalesTopicProduct},
		{"boxer pria mono spot bisa beli per biji ga ?", SalesStateConsulting, SalesTopicRetailPolicy},
		{"nah pinter lu udah", SalesStateConsulting, SalesTopicGeneral},
		{"mau beli boxer monospot tadi dong", SalesStateCartReady, SalesTopicProduct},
		{"eh mau beli 3 paket bang", SalesStateCartReady, SalesTopicProduct},
		{"bukan revisi, saya order 3 paket bukan 1 paket", SalesStateCheckout, SalesTopicProduct},
		{"revisi jadi 10 pakettt", SalesStateCheckout, SalesTopicProduct},
		{"loh saya masih tanya jangan di checkoutkan dulu", SalesStateCorrection, SalesTopicGeneral},
		{"boleh beli 1 pcs ?", SalesStateConsulting, SalesTopicRetailPolicy},
		{"ini tokonya dimananya ?", SalesStateConsulting, SalesTopicLocation},
		{"lalu minta tolong hitungkan ongkir ke jakarta", SalesStateConsulting, SalesTopicShipping},
		{"kak", SalesStateGreeting, ""},
		{"minta list produk", SalesStateBrowsing, SalesTopicList},
		{"mau 1 paket ya boxer mono spot nya", SalesStateCartReady, SalesTopicProduct},
	}

	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			intent := ResolveSalesIntent(tc.msg, hist, false, true, p, cat)
			if intent.State != tc.state {
				t.Fatalf("state=%q want %q (full=%+v)", intent.State, tc.state, intent)
			}
			if tc.topic != "" && intent.Topic != tc.topic {
				t.Fatalf("topic=%q want %q", intent.Topic, tc.topic)
			}
		})
	}
}

// --- ShouldBreakOrderFlow (~14 cases) ---

func TestMatrix_ShouldBreakOrderFlow(t *testing.T) {
	cases := []struct {
		msg    string
		step   string
		break_ bool
	}{
		{"jualan apa aja", "ask_variant", true},
		{"mau tanya produk lain", "ask_variant", true},
		{"loh saya masih tanya", "ask_variant", true},
		{"revisi jadi 10 pakettt", "ask_recipient", false},
		{"loh, ubah jadi 10 paket", "ask_recipient", false},
		{"3 paket", "ask_variant", false},
		{"L", "ask_variant", false},
		{"ini tokonya dimananya ?", "ask_product", true},
		{"kak", "ask_variant", true},
		{"berapa harga nya ?", "ask_variant", true},
		{"Nama: Budi\nHP: 0812", "ask_recipient", false},
		{"Jl Sudirman no 1", "ask_address_full", false},
	}
	for _, tc := range cases {
		t.Run(tc.step+"|"+tc.msg, func(t *testing.T) {
			got := ShouldBreakOrderFlow(tc.msg, tc.step)
			if got != tc.break_ {
				t.Fatalf("break=%v want %v", got, tc.break_)
			}
		})
	}
}

// --- purchase vs consulting (~10 cases) ---

func TestMatrix_PurchaseVsConsulting(t *testing.T) {
	consulting := []string{
		"boleh beli 1 pcs ?",
		"mau beli boxer cd pria bisa ?",
		"kalau order satu bisa?",
		"boleh eceran ga",
		"boxer pria mono spot bisa beli per biji ga ?",
	}
	for _, m := range consulting {
		t.Run("consult_"+m, func(t *testing.T) {
			if !IsConsultingPurchaseQuestion(m) {
				t.Fatal("expected consulting")
			}
			if HasPurchaseIntent(m) {
				t.Fatal("should not be purchase intent")
			}
		})
	}

	purchase := []string{
		"saya jadi beli jeans katun 1 pcs",
		"mau beli boxer monospot tadi dong",
		"mau 1 paket ya boxer mono spot nya",
	}
	for _, m := range purchase {
		t.Run("buy_"+m, func(t *testing.T) {
			if !HasPurchaseIntent(m) {
				t.Fatal("expected purchase intent")
			}
		})
	}
}

// --- catalog routing (~10 cases) ---

func TestMatrix_CatalogRouting(t *testing.T) {
	p := omahProfile()
	cat := omahCatalog()

	listMsgs := []string{"jualan apa aja lu", "minta list produk", "katalog dong"}
	for _, m := range listMsgs {
		t.Run("list_"+m, func(t *testing.T) {
			reply, ok := replyFromBusinessCatalog(m, p, cat, nil)
			if !ok || strings.Contains(reply, "belum ketemu") {
				t.Fatalf("expected list reply, got ok=%v body=%s", ok, reply)
			}
		})
	}

	t.Run("revision_skips_catalog", func(t *testing.T) {
		if _, ok := replyFromBusinessCatalog("revisi jadi 10 pakettt", p, cat, boxerHistory()); ok {
			t.Fatal("revision should not hit catalog_db")
		}
	})

	t.Run("match_boxer", func(t *testing.T) {
		m := matchCatalogItem("boxer mono spot", cat)
		if m == nil || !strings.Contains(m.Name, "MONO SPOT") {
			t.Fatal("expected mono spot match")
		}
	})
}

// --- pack pricing (~8 cases) ---

func TestMatrix_PackPricing(t *testing.T) {
	it := &dbCatalogItem{
		Name:      "[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L",
		SellPrice: 56900,
		SellUnit:  "pcs",
	}
	if bracketPackCount(it.Name) != 3 {
		t.Fatal("expected pack count 3")
	}
	info := parseCatalogPriceInfo(it)
	if info.packCount != 3 || info.listPrice != 56900 || !info.isPackListing {
		t.Fatalf("unexpected price info: %+v", info)
	}
	price := formatCatalogPrice(it)
	if !strings.Contains(price, "paket") || !strings.Contains(price, "56900") {
		t.Fatalf("unexpected price line: %s", price)
	}
	clarify := buildPricingClarificationReply(false, it)
	if strings.Contains(clarify, "170700") || strings.Contains(clarify, "× 3") {
		t.Fatalf("should not multiply pack price: %s", clarify)
	}
}

// --- variant inference (~6 cases) ---

func TestMatrix_VariantInference(t *testing.T) {
	sizes := map[string]string{
		"[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - L":  "L",
		"[3 PCS] CELANA DALAM BOXER PRIA MOTIF MONO SPOT - M":  "M",
		"1PCS CELANA DALAM BOXER ANAK - XL":                    "XL",
		"Jeans Highwaist - S":                                  "S",
	}
	for name, want := range sizes {
		t.Run(want, func(t *testing.T) {
			st := orderState{ProductName: name}
			inferVariantFromProductName(&st)
			if st.Size != want {
				t.Fatalf("size=%q want %q", st.Size, want)
			}
		})
	}
}

// --- scope & praise (~8 cases) ---

func TestMatrix_ScopeAndPraise(t *testing.T) {
	scope := ExtractScopeKeywords("Omah Apparel boxer jeans celana")
	inScope := []string{
		"nah pinter lu udah",
		"boxer cowok ada ga",
		"1 pcs saja",
		"oke terima kasih",
	}
	for _, m := range inScope {
		t.Run(m, func(t *testing.T) {
			if !IsWithinBusinessScope(m, scope, nil) {
				t.Fatal("expected in scope")
			}
		})
	}
	if !IsCasualPraiseLike("nah pinter lu udah") {
		t.Fatal("expected praise")
	}
}

// --- order continuation (~6 cases) ---

func TestMatrix_OrderContinuation(t *testing.T) {
	yes := []string{"1 pcs saja", "3 paket", "ukuran L", "10 pakettt", "revisi jadi 10 pakettt"}
	for _, m := range yes {
		t.Run(m, func(t *testing.T) {
			if !IsOrderContinuationMessage(m) {
				t.Fatal("expected continuation")
			}
		})
	}
}

// --- classifier mapping (~5 cases) ---

func TestMatrix_SalesIntentClassifier(t *testing.T) {
	cases := []struct {
		state string
		label string
	}{
		{SalesStateCartReady, "order_intent"},
		{SalesStateCheckout, "order_intent"},
		{SalesStateBrowsing, "in_scope_question"},
		{SalesStateOutOfScope, "out_of_scope"},
		{SalesStateGreeting, "in_scope_non_question"},
	}
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			cr := salesIntentToClassifier(SalesIntent{State: tc.state, Confidence: 0.9})
			if cr.Label != tc.label {
				t.Fatalf("label=%q want %q", cr.Label, tc.label)
			}
		})
	}
}
