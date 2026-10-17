package salesbrain

import "testing"

func omahFacts(cart []OrderLine, subtotal int64, buyer ...string) Facts {
	return BuildFacts(FactsInput{
		Catalog: []CatalogFact{
			{No: 1, Name: "Australian Beef 500 Gram", Price: 450000, Code: "AUSTRALIAN_BEEF_500_GRAM"},
			{No: 3, Name: "Salmon Steak 100 Gram", Price: 75000, Code: "SALMON_STEAK_100_GRAM"},
			{No: 5, Name: "Yellow Tail Tuna Portion 100 Gram", Price: 350000, Code: "YELLOW_TAIL_TUNA_PORTION_100_GRAM"},
		},
		KB:           []KBFact{{Question: "Metode bayar?", Answer: "Transfer Bank dan QRIS.\nBCA - 000111222 - Omah Apparel\nMandiri - 333111222 - Omah Apparel"}},
		Cart:         cart,
		CartSubtotal: subtotal,
		BuyerTexts:   buyer,
	})
}

func codes(vs []Violation) []string {
	var out []string
	for _, v := range vs {
		out = append(out, v.Code)
	}
	return out
}

func TestValidateAcceptsGroundedReply(t *testing.T) {
	cart := []OrderLine{{Name: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 75000}, {Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000}}
	f := omahFacts(cart, 425000, "Name: Ana\nPhone: 0872839472828")
	d := Decision{Reply: "You can transfer to BCA 000111222 or Mandiri 333111222. Your order is Rp425.000 (Rp75.000 + Rp350.000), shipping not included. I've noted 0872839472828 as the phone.", Intent: IntentQuestion}
	if vs := Validate(d, f, LangEN); len(vs) != 0 {
		t.Fatalf("grounded reply rejected: %+v", vs)
	}
	two := Decision{Reply: "2x Salmon Steak 100 Gram would be Rp150.000.", Intent: IntentQuestion}
	if vs := Validate(two, f, LangEN); len(vs) != 0 {
		t.Fatalf("qty x price must be allowed: %+v", vs)
	}
}

func TestValidateRejectsInventedFacts(t *testing.T) {
	f := omahFacts(nil, 0)
	cases := map[string]Decision{
		VAmount:       {Reply: "The tuna is Rp325.000 today only."},
		VNumber:       {Reply: "Please transfer to BCA 1234567890."},
		VInternalCode: {Reply: "Sure, SALMON_STEAK_100_GRAM is available."},
		VLanguage:     {Reply: "Siap kak, mau pesan yang mana dulu ya?"},
		VEmpty:        {Reply: "  "},
	}
	for want, d := range cases {
		vs := Validate(d, f, LangEN)
		found := false
		for _, v := range vs {
			if v.Code == want {
				found = true
			}
		}
		if !found {
			t.Errorf("reply %q: want violation %s, got %v", d.Reply, want, codes(vs))
		}
	}
}

func TestValidateTooLong(t *testing.T) {
	long := make([]rune, MaxReplyRunes+1)
	for i := range long {
		long[i] = 'a'
	}
	vs := Validate(Decision{Reply: string(long)}, omahFacts(nil, 0), LangEN)
	if len(vs) == 0 || vs[0].Code != VTooLong {
		t.Fatalf("got %v", codes(vs))
	}
}

func TestExtractAmountsFormats(t *testing.T) {
	got := extractAmounts("Rp350.000, Rp 1.750.000, Rp425.000,00, Rp75000/pcs and Rp350,000")
	want := []int64{350000, 1750000, 425000, 75000, 350000}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestViolationNote(t *testing.T) {
	note := ViolationNote([]Violation{{Code: VAmount, Detail: "Rp300.000"}, {Code: VLanguage, Detail: "id"}})
	if note == "" {
		t.Fatal("note must describe violations")
	}
}
