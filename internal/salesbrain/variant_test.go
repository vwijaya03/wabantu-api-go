package salesbrain

import (
	"reflect"
	"strings"
	"testing"
)

// A boxer in three sizes (sibling rows), a T-shirt that needs a size or colour, and a plain product.
func variantFacts() []CatalogFact {
	return []CatalogFact{
		{No: 1, ID: "boxer-l", Name: "Boxer Pria Mono - L", BaseName: "Boxer Pria Mono", Size: "L", NeedsVariant: true, Price: 56900, Stock: StockIn, StockQty: 5},
		{No: 2, ID: "boxer-m", Name: "Boxer Pria Mono - M", BaseName: "Boxer Pria Mono", Size: "M", NeedsVariant: true, Price: 54900, Stock: StockIn, StockQty: 3},
		{No: 3, ID: "boxer-xl", Name: "Boxer Pria Mono - XL", BaseName: "Boxer Pria Mono", Size: "XL", NeedsVariant: true, Price: 58900, Stock: StockOut},
		{No: 4, ID: "kaos-hitam", Name: "Kaos Polos Premium - Hitam", BaseName: "Kaos Polos Premium", NeedsVariant: true, Price: 89000, Stock: StockIn, StockQty: 10},
		{No: 7, ID: "kaos-putih", Name: "Kaos Polos Premium - Putih", BaseName: "Kaos Polos Premium", NeedsVariant: true, Price: 89000, Stock: StockIn, StockQty: 10},
		{No: 5, ID: "salmon", Name: "Salmon Steak 100 Gram", BaseName: "Salmon Steak 100 Gram", Price: 75000},
		{No: 6, ID: "tuna", Name: "Tuna Portion", BaseName: "Tuna Portion", Price: 350000, Stock: StockOut},
	}
}

func resolve(cart []OrderLine, ops []ItemOp, texts ...string) CartResolution {
	return ResolveCartOps(ResolveInput{Cart: cart, Ops: ops, Catalog: variantFacts(), BuyerTexts: texts})
}

func TestSiblingsAreOtherSizesOfTheSameProduct(t *testing.T) {
	f := variantFacts()
	sibs := Siblings(f, f[0])
	if len(sibs) != 2 || sibs[0].No != 2 || sibs[1].No != 3 {
		t.Fatalf("boxer L siblings = %+v", sibs)
	}
	if Siblings(f, f[3]) != nil || Siblings(f, f[4]) != nil {
		t.Fatal("rows without a size have no siblings")
	}
}

func TestResolveSameProductInTwoSizesMakesTwoLines(t *testing.T) {
	res := resolve(nil, []ItemOp{{No: 1, Qty: 2}, {No: 2, Qty: 1}}, "boxer ukuran L 2 pcs dan M 1 pcs")
	if !res.Changed || len(res.Issues) != 0 || len(res.Cart) != 2 {
		t.Fatalf("got %+v", res)
	}
	l, m := res.Cart[0], res.Cart[1]
	if l.ID != "boxer-l" || l.Size != "L" || l.Qty != 2 || l.UnitPrice != 56900 ||
		m.ID != "boxer-m" || m.Size != "M" || m.Qty != 1 || m.UnitPrice != 54900 {
		t.Fatalf("lines = %+v", res.Cart)
	}
	if got := CartSubtotal(res.Cart); got != 2*56900+54900 {
		t.Fatalf("subtotal %d", got)
	}
}

func TestResolveRedirectsToTheSiblingTheBuyerNamed(t *testing.T) {
	// The model picked row 1 (L) but the buyer said M: the grounded size selects the sibling row, at its own price.
	res := resolve(nil, []ItemOp{{No: 1, Qty: 1, Size: "M"}}, "boxer ukuran M satu")
	if len(res.Issues) != 0 || len(res.Cart) != 1 || res.Cart[0].ID != "boxer-m" || res.Cart[0].UnitPrice != 54900 {
		t.Fatalf("got %+v", res)
	}
}

func TestResolveUngroundedSizeIsIgnoredNotInvented(t *testing.T) {
	// The model claims size M, the buyer never typed it: no redirect, and without a grounded size the
	// product is not added, the sizes are asked.
	res := resolve(nil, []ItemOp{{No: 1, Qty: 1, Size: "M"}}, "mau boxer 1 pcs")
	if res.Changed || len(res.Cart) != 0 || len(res.Issues) != 1 {
		t.Fatalf("got %+v", res)
	}
	is := res.Issues[0]
	if is.Kind != IssueChooseVariant || is.Name != "Boxer Pria Mono" || !reflect.DeepEqual(is.Options, []string{"L", "M"}) {
		t.Fatalf("issue = %+v (XL is sold out and must not be offered)", is)
	}
}

func TestResolveUnknownSizeListsWhatExists(t *testing.T) {
	res := resolve(nil, []ItemOp{{No: 1, Qty: 1, Size: "XXL"}}, "boxer XXL 1")
	if len(res.Cart) != 0 || len(res.Issues) != 1 || res.Issues[0].Kind != IssueVariantUnknown ||
		!reflect.DeepEqual(res.Issues[0].Options, []string{"L", "M"}) {
		t.Fatalf("got %+v", res)
	}
}

func TestDeclaredColorsFromDescription(t *testing.T) {
	row := CatalogFact{Description: "Warna: hitam, putih, navy"}
	got := colorsDeclaredOnRow(row)
	if len(got) != 3 || got[0] != "hitam" || got[1] != "putih" || got[2] != "navy" {
		t.Fatalf("colors = %v", got)
	}
	row2 := CatalogFact{Description: "Warna: putih bahan katun adem"}
	got2 := colorsDeclaredOnRow(row2)
	if len(got2) != 1 || got2[0] != "putih" {
		t.Fatalf("colors = %v", got2)
	}
	row3 := CatalogFact{Description: "Warna: nyaman tidak luntur"}
	if c := colorsDeclaredOnRow(row3); len(c) != 0 {
		t.Fatalf("non-color words must not become colors: %v", c)
	}
}

func TestResolveColorAcrossSKUsWithoutBaseName(t *testing.T) {
	catalog := []CatalogFact{
		{No: 1, ID: "kaos-hitam", Name: "Kaos Polos Premium - Hitam", NeedsVariant: true, Price: 89000, Stock: StockIn, StockQty: 5},
		{No: 2, ID: "kaos-putih", Name: "Kaos Polos Premium - Putih", NeedsVariant: true, Price: 89000, Stock: StockIn, StockQty: 5},
	}
	res := ResolveCartOps(ResolveInput{
		Cart: nil, Ops: []ItemOp{{No: 1, Qty: 1, Color: "putih"}},
		Catalog: catalog, BuyerTexts: []string{"kaos putih 1"}, VariantTexts: []string{"kaos putih 1"},
	})
	if len(res.Issues) != 0 || len(res.Cart) != 1 || res.Cart[0].ID != "kaos-putih" || res.Cart[0].Color != "putih" {
		t.Fatalf("must redirect to putih SKU: %+v", res)
	}
}

func TestResolveColorAbuMudaAndSage(t *testing.T) {
	catalog := []CatalogFact{
		{No: 1, ID: "tote-abu", Name: "Tote Bag - Abu Muda", NeedsVariant: true, Price: 50000, Stock: StockIn, StockQty: 3},
		{No: 2, ID: "tote-sage", Name: "Tote Bag", Description: "Warna: sage, hitam", NeedsVariant: true, Price: 50000, Stock: StockIn, StockQty: 3},
	}
	res := ResolveCartOps(ResolveInput{
		Cart: nil, Ops: []ItemOp{{No: 1, Qty: 1, Color: "abu muda"}},
		Catalog: catalog, BuyerTexts: []string{"abu muda 1"}, VariantTexts: []string{"abu muda 1"},
	})
	if len(res.Issues) != 0 || len(res.Cart) != 1 || res.Cart[0].ID != "tote-abu" {
		t.Fatalf("abu muda: %+v", res)
	}
	res = ResolveCartOps(ResolveInput{
		Cart: nil, Ops: []ItemOp{{No: 2, Qty: 1, Color: "sage"}},
		Catalog: catalog, BuyerTexts: []string{"sage 1"}, VariantTexts: []string{"sage 1"},
	})
	if len(res.Issues) != 0 || len(res.Cart) != 1 || res.Cart[0].Color != "sage" {
		t.Fatalf("sage: %+v", res)
	}
}

func TestDeclaredColorsMultipleWarnaHints(t *testing.T) {
	row := CatalogFact{Description: "Warna: hitam. Warna tersedia: navy dan sage"}
	got := colorsDeclaredOnRow(row)
	keys := map[string]bool{}
	for _, c := range got {
		keys[normWords(c)] = true
	}
	for _, want := range []string{"hitam", "navy", "sage"} {
		if !keys[want] {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
}

func TestColorSuffixFromNameWithSize(t *testing.T) {
	if got := colorSuffixFromName("Kaos Polos - Hitam - L", "L"); got != "Hitam" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveUnknownColorListsOnlySoldColours(t *testing.T) {
	res := resolve(nil, []ItemOp{{No: 4, Qty: 1, Color: "hijau"}}, "kaos polos hijau 1")
	if len(res.Cart) != 0 || len(res.Issues) != 1 || res.Issues[0].Kind != IssueColorUnknown {
		t.Fatalf("got %+v", res)
	}
	opts := res.Issues[0].Options
	if len(opts) != 2 || !reflect.DeepEqual([]string{normWords(opts[0]), normWords(opts[1])}, []string{"hitam", "putih"}) {
		t.Fatalf("options must be product colours only, got %v", opts)
	}
	if got := RenderIssue(res.Issues[0], LangID); !strings.Contains(got, "warna") || strings.Contains(got, "ukuran") {
		t.Fatalf("color rejection copy: %q", got)
	}
}

func TestResolveMissingVariantKeepsTheLineAndAsksOnce(t *testing.T) {
	res := resolve(nil, []ItemOp{{No: 4, Qty: 2}}, "mau kaos polos 2")
	if !res.Changed || len(res.Cart) != 1 || !res.Cart[0].VariantMissing || res.Cart[0].Qty != 2 {
		t.Fatalf("the quantity must be kept while the variant is open: %+v", res.Cart)
	}
	if len(res.Issues) != 1 || res.Issues[0].Kind != IssueVariantMissing || res.Issues[0].Name != "Kaos Polos Premium" {
		t.Fatalf("issues = %+v", res.Issues)
	}
	if got := RenderIssue(res.Issues[0], LangID); got != "Untuk Kaos Polos Premium, Kakak mau ukuran atau warna apa?" {
		t.Fatalf("ask = %q", got)
	}
	if got := RenderIssue(res.Issues[0], LangEN); got != "Which size or color would you like for Kaos Polos Premium?" {
		t.Fatalf("ask = %q", got)
	}

	// The answer fills the open line instead of adding a second one.
	next := resolve(res.Cart, []ItemOp{{No: 4, Qty: 2, Size: "M", Color: "hitam"}}, "M hitam")
	if len(next.Issues) != 0 || !next.Changed || len(next.Cart) != 1 {
		t.Fatalf("got %+v", next)
	}
	if l := next.Cart[0]; l.Size != "M" || l.Color != "hitam" || l.VariantMissing || l.Qty != 2 {
		t.Fatalf("line = %+v", l)
	}
}

func TestResolveGenericVariantMustBeGroundedInBuyerText(t *testing.T) {
	res := resolve(nil, []ItemOp{{No: 4, Qty: 1, Size: "XL", Color: "merah"}}, "mau kaos polos satu")
	if len(res.Cart) != 1 || res.Cart[0].Size != "" || res.Cart[0].Color != "" || !res.Cart[0].VariantMissing {
		t.Fatalf("invented variant accepted: %+v", res.Cart)
	}
	// Only the part the buyer typed is kept.
	res = resolve(nil, []ItemOp{{No: 4, Qty: 1, Size: "XL", Color: "merah"}}, "kaos polos XL satu")
	if len(res.Cart) != 1 || res.Cart[0].Size != "XL" || res.Cart[0].Color != "" || res.Cart[0].VariantMissing || len(res.Issues) != 0 {
		t.Fatalf("got %+v", res)
	}
}

func TestResolveVariantGroundsOnlyFromVariantTexts(t *testing.T) {
	// A house number in an earlier address must not become a size on this turn.
	res := ResolveCartOps(ResolveInput{
		Cart: nil, Ops: []ItemOp{{No: 4, Qty: 1, Size: "42"}},
		Catalog:      variantFacts(),
		BuyerTexts:   []string{"kaos satu", "Jl. Melati No 42 Surabaya"},
		VariantTexts: []string{"kaos satu"},
	})
	if len(res.Cart) != 1 || res.Cart[0].Size != "" || !res.Cart[0].VariantMissing {
		t.Fatalf("size 42 from address must not apply: %+v", res.Cart)
	}
	// Same turn text still grounds the variant.
	res = ResolveCartOps(ResolveInput{
		Cart: nil, Ops: []ItemOp{{No: 4, Qty: 1, Size: "L", Color: "putih"}},
		Catalog:      variantFacts(),
		BuyerTexts:   []string{"ukuran L warna putih"},
		VariantTexts: []string{"ukuran L warna putih"},
	})
	if len(res.Issues) != 0 || res.Cart[0].Size != "L" || res.Cart[0].Color != "putih" {
		t.Fatalf("got %+v", res)
	}
}

func TestResolveSameGenericProductInTwoVariants(t *testing.T) {
	texts := []string{"kaos M hitam 1 dan kaos L putih 2"}
	res := resolve(nil, []ItemOp{{No: 4, Qty: 1, Size: "M", Color: "hitam"}, {No: 7, Qty: 2, Size: "L", Color: "putih"}}, texts...)
	if len(res.Issues) != 0 || len(res.Cart) != 2 {
		t.Fatalf("got %+v", res)
	}
	if res.Cart[0].Size != "M" || res.Cart[1].Size != "L" || res.Cart[1].Qty != 2 {
		t.Fatalf("lines = %+v", res.Cart)
	}

	// Changing one variant leaves the other alone.
	next := resolve(res.Cart, []ItemOp{{No: 4, Qty: 5, Size: "M", Color: "hitam"}}, "kaos M hitam jadi 5")
	if len(next.Cart) != 2 || next.Cart[0].Qty != 5 || next.Cart[1].Qty != 2 || !next.Changed {
		t.Fatalf("got %+v", next.Cart)
	}
	// Removing one variant keeps the other.
	next = resolve(res.Cart, []ItemOp{{No: 7, Qty: 0, Size: "L", Color: "putih"}}, "kaos L putih batal")
	if len(next.Cart) != 1 || next.Cart[0].Size != "M" {
		t.Fatalf("got %+v", next.Cart)
	}
	// "Remove the kaos" without a variant removes every line of it.
	next = resolve(res.Cart, []ItemOp{{No: 4, Qty: 0}, {No: 7, Qty: 0}}, "kaos batal semua")
	if len(next.Cart) != 0 || !next.Changed {
		t.Fatalf("got %+v", next.Cart)
	}
	// A new quantity without a variant is ambiguous between two lines: asked, nothing changes.
	next = resolve(res.Cart, []ItemOp{{No: 4, Qty: 9}}, "kaos jadi 9")
	if next.Changed || len(next.Issues) != 1 || next.Issues[0].Kind != IssueVariantMissing {
		t.Fatalf("got %+v", next)
	}
}

func TestResolveSoldOutIsRefusedHonestlyWithAlternatives(t *testing.T) {
	res := resolve(nil, []ItemOp{{No: 3, Qty: 1}}, "boxer XL 1")
	if res.Changed || len(res.Cart) != 0 || len(res.Issues) != 1 {
		t.Fatalf("got %+v", res)
	}
	is := res.Issues[0]
	if is.Kind != IssueSoldOut || is.Name != "Boxer Pria Mono - XL" || !reflect.DeepEqual(is.Options, []string{"L", "M"}) {
		t.Fatalf("issue = %+v", is)
	}
	id := RenderIssue(is, LangID)
	if !strings.Contains(id, "Boxer Pria Mono - XL sedang habis") || !strings.Contains(id, "Yang masih tersedia: L, M") {
		t.Fatalf("ID = %q", id)
	}
	en := RenderIssue(is, LangEN)
	if !strings.Contains(en, "Boxer Pria Mono - XL is out of stock") || !strings.Contains(en, "Still available: L, M") {
		t.Fatalf("EN = %q", en)
	}

	// A plain product without alternatives: honest, no second sentence.
	res = resolve(nil, []ItemOp{{No: 6, Qty: 1}}, "tuna 1")
	if len(res.Cart) != 0 || len(res.Issues) != 1 || res.Issues[0].Kind != IssueSoldOut || len(res.Issues[0].Options) != 0 {
		t.Fatalf("got %+v", res)
	}
	if got := RenderIssue(res.Issues[0], LangEN); got != "Sorry, Tuna Portion is out of stock." {
		t.Fatalf("got %q", got)
	}
}

func TestResolveStockLimit(t *testing.T) {
	res := resolve(nil, []ItemOp{{No: 2, Qty: 5}}, "boxer M 5")
	if len(res.Cart) != 0 || len(res.Issues) != 1 || res.Issues[0].Kind != IssueStockLimit || res.Issues[0].Available != 3 {
		t.Fatalf("got %+v", res)
	}
	if got := RenderIssue(res.Issues[0], LangEN); !strings.Contains(got, "only 3 of Boxer Pria Mono left") {
		t.Fatalf("got %q", got)
	}
	// Raising an existing line past the stock is refused too, and the line stays as it was.
	cart := []OrderLine{{ID: "boxer-m", Name: "Boxer Pria Mono - M", Qty: 2, UnitPrice: 54900, Size: "M"}}
	res = resolve(cart, []ItemOp{{No: 2, Qty: 4}}, "boxer M jadi 4")
	if res.Changed || res.Cart[0].Qty != 2 || len(res.Issues) != 1 {
		t.Fatalf("got %+v", res)
	}
	// Lowering is always fine.
	res = resolve(cart, []ItemOp{{No: 2, Qty: 1}}, "boxer M jadi 1")
	if !res.Changed || res.Cart[0].Qty != 1 || len(res.Issues) != 0 {
		t.Fatalf("got %+v", res)
	}
}

func TestResolveStockPooledAcrossVariantLines(t *testing.T) {
	catalog := []CatalogFact{
		{No: 1, ID: "kaos", Name: "Kaos Polos Premium", BaseName: "Kaos Polos Premium", NeedsVariant: true, Price: 89000, Stock: StockIn, StockQty: 4},
	}
	cart := []OrderLine{{ID: "kaos", Name: "Kaos Polos Premium", Qty: 3, UnitPrice: 89000, Size: "M"}}
	res := ResolveCartOps(ResolveInput{
		Cart: cart, Ops: []ItemOp{{No: 1, Qty: 3, Size: "L"}},
		Catalog: catalog, BuyerTexts: []string{"3 L"}, VariantTexts: []string{"3 L"},
	})
	if res.Changed || len(res.Issues) != 1 || res.Issues[0].Kind != IssueStockLimit || res.Issues[0].Available != 1 {
		t.Fatalf("3+3 must exceed pool stock 4: %+v", res)
	}
}

func TestResolveRejectsUnknownColor(t *testing.T) {
	res := ResolveCartOps(ResolveInput{
		Cart: nil, Ops: []ItemOp{{No: 4, Qty: 1, Size: "M", Color: "ungu neon"}},
		Catalog: variantFacts(), BuyerTexts: []string{"M ungu neon"}, VariantTexts: []string{"M ungu neon"},
	})
	if len(res.Cart) != 0 || len(res.Issues) != 1 || res.Issues[0].Kind != IssueColorUnknown {
		t.Fatalf("got %+v", res)
	}
}

func TestResolvePriceComesFromTheCatalogRow(t *testing.T) {
	stale := []OrderLine{{ID: "salmon", Name: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 70000}}
	res := resolve(stale, []ItemOp{{No: 5, Qty: 2}}, "salmon 2")
	if res.Cart[0].UnitPrice != 75000 || res.Cart[0].Qty != 2 {
		t.Fatalf("got %+v", res.Cart)
	}
}

func TestResolveDoesNotMutateInputAndReportsUnknownRows(t *testing.T) {
	in := []OrderLine{{ID: "salmon", Name: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 75000}}
	res := resolve(in, []ItemOp{{No: 5, Qty: 0}, {No: 99, Qty: 1}}, "x")
	if len(res.Cart) != 0 || len(res.Unknown) != 1 || res.Unknown[0] != 99 {
		t.Fatalf("got %+v", res)
	}
	if len(in) != 1 || in[0].Qty != 1 {
		t.Fatalf("input mutated: %+v", in)
	}
}

func TestResolveWithoutVariantDataBehavesLikeApplyItemOps(t *testing.T) {
	ops := []ItemOp{{No: 5, Qty: 1}, {No: 3, Qty: 2}, {No: 9, Qty: 1}}
	steak := steakFacts
	want, wantChanged, wantUnknown := ApplyItemOps(nil, ops, steak)
	got := ResolveCartOps(ResolveInput{Ops: ops, Catalog: steak, BuyerTexts: []string{"x"}})
	if !reflect.DeepEqual(got.Cart, want) || got.Changed != wantChanged || !reflect.DeepEqual(got.Unknown, wantUnknown) || len(got.Issues) != 0 {
		t.Fatalf("got %+v, want cart=%+v changed=%v unknown=%v", got, want, wantChanged, wantUnknown)
	}
}

func TestLineLabelAndSummaryShowTheVariant(t *testing.T) {
	cases := []struct {
		line OrderLine
		lang Lang
		want string
	}{
		{OrderLine{Name: "Kaos Polos Premium", Size: "M", Color: "hitam"}, LangID, "Kaos Polos Premium (ukuran M, warna hitam)"},
		{OrderLine{Name: "Kaos Polos Premium", Size: "M", Color: "black"}, LangEN, "Kaos Polos Premium (size M, color black)"},
		{OrderLine{Name: "Boxer Pria Mono - L", Size: "L"}, LangID, "Boxer Pria Mono - L"}, // the name already says L
		{OrderLine{Name: "Kaos Polos Premium", VariantMissing: true}, LangID, "Kaos Polos Premium (ukuran/warna belum dipilih)"},
		{OrderLine{Name: "Kaos Polos Premium", VariantMissing: true}, LangEN, "Kaos Polos Premium (size/color not chosen yet)"},
		{OrderLine{Name: "Salmon"}, LangEN, "Salmon"},
	}
	for _, c := range cases {
		if got := LineLabel(c.line, c.lang); got != c.want {
			t.Errorf("LineLabel(%+v, %s) = %q, want %q", c.line, c.lang, got, c.want)
		}
	}
	sum := RenderCartSummary([]OrderLine{
		{ID: "kaos", Name: "Kaos Polos Premium", Qty: 1, UnitPrice: 89000, Size: "M", Color: "hitam"},
		{ID: "kaos", Name: "Kaos Polos Premium", Qty: 2, UnitPrice: 89000, Size: "L", Color: "putih"},
	}, LangEN)
	for _, want := range []string{"1. 1x Kaos Polos Premium (size M, color hitam)", "2. 2x Kaos Polos Premium (size L, color putih)", "Subtotal: Rp267.000"} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary missing %q:\n%s", want, sum)
		}
	}
	if ResumeLineFor("variant", LangEN) != T(TplResumeVariant, LangEN) || ResumeLineFor("variant", LangID) == "" {
		t.Fatal("variant must have a resume line")
	}
}

func TestPromptShowsVariantRowsAndCartVariants(t *testing.T) {
	system, user := BuildPrompt(PromptInput{
		Catalog: variantFacts(),
		State: TurnState{
			Lang: LangID, PendingField: "variant", Cart: []OrderLine{
				{ID: "kaos", Name: "Kaos Polos Premium", Qty: 2, UnitPrice: 89000, VariantMissing: true},
				{ID: "boxer-m", Name: "Boxer Pria Mono - M", Qty: 1, UnitPrice: 54900, Size: "M"},
			},
			CartSubtotal: 2*89000 + 54900,
		},
		Message: "M hitam",
	})
	for _, want := range []string{
		"1. Boxer Pria Mono - L | Rp56.900 | stok: tersedia | ukuran: L | ukuran lain: no 2 (M, tersedia); no 3 (XL, habis)",
		"4. Kaos Polos Premium - Hitam | Rp89.000 | stok: tersedia | wajib pilih ukuran/warna",
		"7. Kaos Polos Premium - Putih | Rp89.000 | stok: tersedia | wajib pilih ukuran/warna",
		`"size": ""`, "Ukuran/warna:",
	} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	if strings.Contains(strings.SplitN(system, "[KATALOG]", 2)[1], "| wajib pilih ukuran/warna | ukuran") {
		t.Error("a plain row must not carry variant text")
	}
	for _, want := range []string{"2x Kaos Polos Premium (ukuran/warna belum dipilih) @Rp89.000", "1x Boxer Pria Mono - M @Rp54.900", "kolom_tertunda: variant"} {
		if !strings.Contains(user, want) {
			t.Errorf("state block missing %q:\n%s", want, user)
		}
	}
}

func TestParseDecisionKeepsGroundableVariantFields(t *testing.T) {
	d, err := ParseDecision(`{"intent":"order","items":[{"no":4,"qty":2,"size":" M ","color":"hitam"},{"no":2,"qty":1,"size":38}],"reply":"ok"}`)
	if err != nil || len(d.Items) != 2 {
		t.Fatalf("err=%v items=%+v", err, d.Items)
	}
	if d.Items[0].Size != "M" || d.Items[0].Color != "hitam" || d.Items[1].Size != "38" {
		t.Fatalf("items = %+v", d.Items)
	}
	// Items without variant fields still parse exactly as before.
	d, err = ParseDecision(`{"intent":"order","items":[{"no":1,"qty":1}],"reply":"ok"}`)
	if err != nil || len(d.Items) != 1 || d.Items[0].Size != "" || d.Items[0].Color != "" {
		t.Fatalf("err=%v items=%+v", err, d.Items)
	}
}

func TestVariantTemplatesExistInBothLanguages(t *testing.T) {
	for _, k := range []TemplateKey{TplConfirmHold, TplResumeVariant, TplAskVariant, TplAskVariantChoices, TplVariantNotFound, TplVariantSoldOut, TplVariantAlternatives, TplStockLimit} {
		for _, l := range []Lang{LangID, LangEN} {
			if strings.TrimSpace(templates[k][l]) == "" {
				t.Errorf("template %s has no %s text", k, l)
			}
		}
	}
	if got := T(TplConfirmHold, LangEN); !strings.Contains(got, "has not been saved or changed") {
		t.Errorf("confirm hold EN = %q", got)
	}
}
