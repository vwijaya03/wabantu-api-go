package buyerflow

import "testing"

func TestTryAppendSkipsOrderRevision(t *testing.T) {
	catalog := omahCatalog()
	st := OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "abon-500g",
		ProductName:   "Abon Sapi 500G",
		Qty:           1,
		UnitPrice:     35000,
		SellUnit:      "pcs",
	}
	tmpl := orderTemplatesFromKB(nil, false)
	msg := "ga jadi ganti 3 pcs bang"
	handled, _ := TryAppendItemsDuringCheckout(&st, msg, catalog, tmpl, false, nil)
	if handled {
		t.Fatal("revision message must not trigger append during checkout")
	}
	if !tryApplyQtyRevision(&st, msg) || st.Qty != 3 {
		t.Fatalf("qty revision should still work, got qty=%d", st.Qty)
	}
}
