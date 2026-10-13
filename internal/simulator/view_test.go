package simulator

import "testing"

func TestBuildOrderView(t *testing.T) {
	v := BuildOrderView("1a2b3c4d-0000-0000-0000-000000000000", "confirmed", "proof_submitted", 144000,
		[]byte(`[{"name":"Kopi Gayo 250g","qty":2,"unitPrice":72000,"sellUnit":"pcs"}]`),
		[]byte(`{"amount":144000,"flags":["ai_review_required"]}`), "t")
	if v.OrderNumber != "WB-1A2B3C4D" || len(v.Items) != 1 || v.Items[0].UnitPrice != 72000 || v.ProofCheck != "cocok" {
		t.Fatalf("%+v", v)
	}
	v = BuildOrderView("x", "confirmed", "proof_submitted", 1, []byte(`bad`), []byte(`{"amount":5,"flags":["mismatch_amount"]}`), "")
	if v.ProofCheck != "perlu_dicek" || v.ProofCheckReasons[0] != "mismatch_amount" || v.Items == nil {
		t.Fatalf("%+v", v)
	}
	if BuildOrderView("x", "draft", "unpaid", 0, nil, []byte(`{}`), "").ProofCheck != "" {
		t.Fatal("no proof → empty")
	}
	if ImageURL("m1") != "/api/v1/inbox/messages/m1/media" {
		t.Fatal()
	}
}
