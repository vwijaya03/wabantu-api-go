package buyerflow

import (
	"strings"
	"testing"
)

func TestShouldBreakOrderFlowImplicitAppend(t *testing.T) {
	catalog := omahCatalog()
	if ShouldBreakOrderFlow("cadbury mini 1 pcs", "ask_recipient", catalog) {
		t.Fatal("named product+qty at ask_recipient must not break flow")
	}
	food := omahFoodCatalog()
	if ShouldBreakOrderFlow("nutella", "ask_recipient", food) {
		t.Fatal("bare unique SKU append must not break flow (conjunction not required)")
	}
	if ShouldBreakOrderFlow("xiaomi redmi 13 128gb 1 pcs", "ask_recipient", electronicsCatalog()) {
		t.Fatal("electronics append must not break checkout")
	}
	if ShouldBreakOrderFlow("hello kitty L 1 pcs", "ask_recipient", apparelCatalog()) {
		t.Fatal("apparel second SKU must not break checkout")
	}
	if !ShouldBreakOrderFlow("berapa ongkir ke bandung?", "ask_recipient", catalog) {
		t.Fatal("shipping question should break flow at ask_recipient")
	}
}

func TestIsCartRecapOrComplaint(t *testing.T) {
	catalog := omahCatalog()
	cases := []struct {
		text string
		want bool
	}{
		{"pesanan saya ada 2 loh ya", true},
		{"abon nutela ga masuk", true},
		{"gimana status pesanan WB-58D662BC", false},
		{"pesanan saya pending ga ya?", false},
	}
	for _, tc := range cases {
		got := IsCartRecapOrComplaint(tc.text, catalog)
		if got != tc.want {
			t.Fatalf("IsCartRecapOrComplaint(%q) = %v want %v", tc.text, got, tc.want)
		}
	}
}

func TestIsOrderStatusInquirySkipsCartComplaint(t *testing.T) {
	if IsOrderStatusInquiry("pesanan saya ada 2 loh ya") {
		t.Fatal("cart complaint must not match order status inquiry")
	}
}

func TestExtractSizeIgnoresWeight(t *testing.T) {
	if sz := extractSizeFromProductName("Durian Musang King Biskuit 240G"); sz != "" {
		t.Fatalf("240G must not be apparel size, got %q", sz)
	}
	if sz := extractSizeFromProductName("Abon Sapi 130 gram"); sz != "" {
		t.Fatalf("130 gram must not be apparel size, got %q", sz)
	}
	if sz := extractSizeFromProductName("BOXER MONO SPOT - L"); sz != "L" {
		t.Fatalf("apparel L suffix want L, got %q", sz)
	}
}

func TestCatalogItemNeedsVariantFoodVsApparel(t *testing.T) {
	food := &CatalogItem{Name: "Durian Musang King Biskuit 240G"}
	if catalogItemNeedsVariant(food) {
		t.Fatal("food with weight suffix must not need variant")
	}
	apparel := &CatalogItem{Name: "BOXER MONO SPOT - M"}
	if !catalogItemNeedsVariant(apparel) {
		t.Fatal("apparel with size suffix must need variant")
	}
	boxerNoSize := &CatalogItem{Name: "celana dalam boxer mono spot"}
	if !catalogItemNeedsVariant(boxerNoSize) {
		t.Fatal("boxer keyword must need variant")
	}
}

func TestIsAddMoreItemsPolicyQuestion(t *testing.T) {
	if !IsAddMoreItemsPolicyQuestion("masih mau order item yang lain?") {
		t.Fatal("expected policy question")
	}
	if IsCatalogBrowsingIntent("masih mau order item yang lain?") {
		t.Fatal("policy question must not be catalog browsing")
	}
}

func TestRegressionCheckoutContinuityMatrix(t *testing.T) {
	type step struct {
		input    string
		wantPath string
		check    func(t *testing.T, out TurnOutcome, sim *Simulator)
	}
	type archetype struct {
		name   string
		newSim func() *Simulator
		steps  []step
	}
	archetypes := []archetype{
		{
			name:   "mixed_omah_implicit_append",
			newSim: NewOmahSimulator,
			steps: []step{
				{input: "mau beli abon sapi 500g 2 pcs", wantPath: PathOrderFlow},
				{
					input:    "cadbury mini 1 pcs",
					wantPath: PathOrderFlow,
					check: func(t *testing.T, out TurnOutcome, sim *Simulator) {
						if sim.Order == nil || !sim.Order.HasMultiItems() || len(sim.Order.Items) != 2 {
							t.Fatalf("want 2 cart lines, got order=%+v", sim.Order)
						}
					},
				},
			},
		},
		{
			name:   "apparel_second_sku_at_recipient",
			newSim: newApparelSimulator,
			steps: []step{
				{input: "boxer mono spot L 1 pcs", wantPath: PathOrderFlow},
				{
					input:    "hello kitty L 1 pcs",
					wantPath: PathOrderFlow,
					check: func(t *testing.T, out TurnOutcome, sim *Simulator) {
						if sim.Order == nil || !sim.Order.HasMultiItems() {
							t.Fatal("expected multi-item apparel cart")
						}
					},
				},
			},
		},
		{
			name:   "food_no_variant_durian",
			newSim: newFoodSimulator,
			steps: []step{
				{
					input:    "durian musang king 1",
					wantPath: PathOrderFlow,
					check: func(t *testing.T, out TurnOutcome, sim *Simulator) {
						if sim.Order != nil && sim.Order.Step == "ask_variant" {
							t.Fatalf("food must not ask variant: reply=%q", out.Reply)
						}
						if strings.Contains(strings.ToLower(out.Reply), "ukuran") {
							t.Fatalf("food must not ask size: %q", out.Reply)
						}
					},
				},
			},
		},
		{
			name:   "food_cart_complaint_recap",
			newSim: newFoodSimulator,
			steps: []step{
				{input: "maggi percik 1 pcs", wantPath: PathOrderFlow},
				{input: "nutella 1", wantPath: PathOrderFlow},
				{
					input:    "pesanan saya ada 2 loh ya",
					wantPath: PathOrderFlow,
					check: func(t *testing.T, out TurnOutcome, sim *Simulator) {
						if out.Path == PathOrderStatus {
							t.Fatal("cart complaint must route to order_flow recap, not order_status")
						}
						if !strings.Contains(strings.ToLower(out.Reply), "ringkasan") {
							t.Fatalf("expected cart recap: %q", out.Reply)
						}
					},
				},
			},
		},
		{
			name:   "mixed_add_more_policy",
			newSim: NewOmahSimulator,
			steps: []step{
				{input: "mau beli abon sapi 2 pcs", wantPath: PathOrderFlow},
				{
					input:    "masih mau order item yang lain?",
					wantPath: PathOrderFlow,
					check: func(t *testing.T, out TurnOutcome, sim *Simulator) {
						if out.BrokeFlow || sim.Order == nil {
							t.Fatal("policy question must not clear active checkout cart")
						}
						if out.Path == PathCatalogDB {
							t.Fatal("policy question must not trigger full catalog")
						}
						if !strings.Contains(strings.ToLower(out.Reply), "boleh tambah") {
							t.Fatalf("expected CS add-more reply: %q", out.Reply)
						}
					},
				},
			},
		},
		{
			name:   "electronics_append_other_brand",
			newSim: newElectronicsSimulator,
			steps: []step{
				{input: "samsung a14 128gb 1 pcs", wantPath: PathOrderFlow},
				{
					input:    "xiaomi redmi 13 128gb 1 pcs",
					wantPath: PathOrderFlow,
					check: func(t *testing.T, out TurnOutcome, sim *Simulator) {
						if sim.Order == nil || !sim.Order.HasMultiItems() || len(sim.Order.Items) != 2 {
							t.Fatalf("gadget cart must hold two brands, got %+v", sim.Order)
						}
					},
				},
			},
		},
		{
			name:   "beauty_append_other_line",
			newSim: newBeautySimulator,
			steps: []step{
				{input: "wardah lip cream 01 1 pcs", wantPath: PathOrderFlow},
				{
					input:    "wardah crystal secret serum 1 pcs",
					wantPath: PathOrderFlow,
					check: func(t *testing.T, out TurnOutcome, sim *Simulator) {
						if sim.Order == nil || !sim.Order.HasMultiItems() || len(sim.Order.Items) != 2 {
							t.Fatalf("lip + serum must both stay in cart, got %+v", sim.Order)
						}
					},
				},
			},
		},
		{
			name:   "apparel_cart_complaint_recap",
			newSim: newApparelSimulator,
			steps: []step{
				{input: "boxer mono spot L 1 pcs", wantPath: PathOrderFlow},
				{input: "hello kitty L 1 pcs", wantPath: PathOrderFlow},
				{
					input:    "pesanan saya ada 2 loh ya",
					wantPath: PathOrderFlow,
					check: func(t *testing.T, out TurnOutcome, sim *Simulator) {
						if out.Path == PathOrderStatus {
							t.Fatal("apparel cart complaint must recap, not DB status")
						}
						if !strings.Contains(strings.ToLower(out.Reply), "ringkasan") {
							t.Fatalf("expected apparel cart recap: %q", out.Reply)
						}
					},
				},
			},
		},
	}
	for _, arch := range archetypes {
		t.Run(arch.name, func(t *testing.T) {
			sim := arch.newSim()
			for i, step := range arch.steps {
				out := sim.Turn(step.input)
				if step.wantPath != "" && out.Path != step.wantPath {
					t.Fatalf("step %d path=%q want %q reply=%q", i, out.Path, step.wantPath, out.Reply)
				}
				if step.check != nil {
					step.check(t, out, sim)
				}
			}
		})
	}
}
