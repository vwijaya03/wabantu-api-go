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
		Message:      "how do I pay?", // these tests are about grounding, not about premature payment
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

func TestValidateAllowsDigitOnlyOrderRefsAndDates(t *testing.T) {
	f := BuildFacts(FactsInput{OpenOrders: []OrderSnapshot{{Ref: "WB-12345678", Status: StatusDraft, Subtotal: 75000}}})
	d := Decision{Reply: "Your order WB-12345678 from 17-10-2026 is Rp75.000, shipping not included."}
	if vs := Validate(d, f, LangEN); len(vs) != 0 {
		t.Fatalf("digit-only order ref and dates must be allowed, got %+v", vs)
	}
	bad := Decision{Reply: "Please transfer to 1234-5678-90."}
	if vs := Validate(bad, f, LangEN); len(vs) == 0 || vs[0].Code != VNumber {
		t.Fatalf("an invented account number must still be rejected, got %+v", vs)
	}
}

func TestValidatePrematurePayment(t *testing.T) {
	kb := []KBFact{{Answer: "Transfer ke BCA - 000111222 - Omah Apparel"}}
	facts := func(msg string, orders ...OrderSnapshot) Facts {
		return BuildFacts(FactsInput{KB: kb, Message: msg, BuyerTexts: []string{msg}, OpenOrders: orders})
	}
	bad := []string{
		"Your order is now ready for payment; please transfer Rp0 to BCA.",
		"Silakan transfer ke rekening BCA, lalu kirim bukti transfer.",
		"Pesanan Kakak siap dibayar ya.",
		"Please send your payment proof afterwards.",
		"Pay to 000111222 please.",
		"I have updated your address. Please proceed with the payment.",
	}
	for _, reply := range bad {
		vs := Validate(Decision{Reply: reply, Intent: IntentFieldAnswer}, facts("Name: Anastasi\nPhone: 081222000111"), LangEN)
		found := false
		for _, v := range vs {
			found = found || v.Code == VPrematurePayment
		}
		if !found {
			t.Errorf("%q must be premature_payment, got %v", reply, codes(vs))
		}
	}
	ok := Decision{Reply: "You can pay by transfer to BCA 000111222 or QRIS.", Intent: IntentQuestion}
	if vs := Validate(ok, facts("how do I pay?"), LangEN); len(vs) != 0 {
		t.Errorf("a buyer asking about payment is answered: %+v", vs)
	}
	if vs := Validate(ok, facts("ke rekening mana?"), LangEN); len(vs) != 0 {
		t.Errorf("rekening question: %+v", vs)
	}
	waiting := OrderSnapshot{Ref: "WB-1", Status: StatusAwaitingPayment, Stage: StagePayment}
	if vs := Validate(ok, facts("ok thanks", waiting), LangEN); len(vs) != 0 {
		t.Errorf("an order awaiting payment may be discussed: %+v", vs)
	}
	// An ordinary answer is untouched.
	plain := Decision{Reply: "It is sold frozen and vacuum-sealed.", Intent: IntentQuestion}
	if vs := Validate(plain, facts("is it frozen?"), LangEN); len(vs) != 0 {
		t.Errorf("plain reply rejected: %+v", vs)
	}
	// The confirm intent at the confirm step may mention payment.
	conf := BuildFacts(FactsInput{KB: kb, Message: "OK", AtConfirmStep: true})
	if vs := Validate(Decision{Reply: "Thanks! Please transfer to BCA 000111222.", Intent: IntentConfirm}, conf, LangEN); len(vs) != 0 {
		t.Errorf("confirm step: %+v", vs)
	}
	if vs := Validate(Decision{Reply: "Thanks! Please transfer to BCA 000111222.", Intent: IntentConfirm}, facts("OK"), LangEN); len(vs) == 0 {
		t.Error("a confirm outside the confirm step is premature")
	}
}
