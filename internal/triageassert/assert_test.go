package triageassert

import (
	"testing"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/shared/interactionevidence"
	"encore.app/wabantu/shared/kbcontext"
)

func TestCheckTurnWrongSKU(t *testing.T) {
	out := bf.TurnOutcome{
		Path:  "order_flow",
		Reply: "oke oatlife",
		Order: &bf.OrderState{
			Step:  "cart",
			Items: []bf.OrderLineState{{ProductName: "Oatlife White Coffee", Qty: 1}},
		},
	}
	err := CheckTurn(out, CommerceSpec{
		WantPath:     "order_flow",
		Include:      []CartItem{{NameContains: "durian", Qty: 1}},
		ExcludeNames: []string{"White Coffee"},
	})
	if err == nil {
		t.Fatal("same path wrong SKU must fail")
	}
}

func TestCheckPayloadWrongPrice(t *testing.T) {
	ev := interactionevidence.TurnEvidence{
		FinalText:    "Durian Rp 10.000",
		ProductCards: []interactionevidence.ProductCard{{CatalogItemID: "d1", Name: "Durian", Price: 10000}},
	}
	if err := CheckPayload(ev, PayloadSpec{CardIDs: []string{"d1"}, ForbiddenClaims: []string{"10.000"}}); err == nil {
		t.Fatal("wrong price claim must fail even if SKU card exists")
	}
}

func TestCheckSearchHiddenItem(t *testing.T) {
	err := CheckSearch([]SearchHit{{CatalogItemID: "x", StorefrontVisible: false}}, SearchSpec{ForbidHidden: true})
	if err == nil {
		t.Fatal("hidden catalog must not appear")
	}
}

func TestCheckTraceParity(t *testing.T) {
	tr := kbcontext.Trace{KBEntryIDs: []string{"a"}, CatalogIDs: []string{"c"}}
	if err := CheckTrace(tr, RetrievalSpec{KBEntryIDs: []string{"a"}, CatalogIDs: []string{"c"}}); err != nil {
		t.Fatal(err)
	}
}
