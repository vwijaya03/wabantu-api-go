package buyerflow

import (
	"strings"
	"testing"
)

func recipientStepState() OrderState {
	return OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "samsung-a14-128",
		ProductName:   "Samsung Galaxy A14 128GB",
		Qty:           1,
		UnitPrice:     2199000,
		SellUnit:      "pcs",
	}
}

func advanceRecipient(text string) OrderFlowResult {
	st := recipientStepState()
	return AdvanceOrderFlow(OrderFlowInput{
		UserText: text,
		State:    &st,
		Catalog:  electronicsCatalog(),
		Profile:  electronicsProfile(),
	}, nil)
}

func TestAskRecipientTemplatesAskNamePhoneAndAddress(t *testing.T) {
	for name, tm := range map[string]orderFlowTemplates{
		"id-formal": defaultOrderTemplates(true),
		"id-casual": defaultOrderTemplates(false),
		"en-formal": englishDefaultOrderTemplates(true),
		"en-casual": englishDefaultOrderTemplates(false),
	} {
		low := strings.ToLower(tm.AskRecipient)
		if !strings.Contains(low, "alamat") && !strings.Contains(low, "address") {
			t.Errorf("%s: AskRecipient must also ask the address: %q", name, tm.AskRecipient)
		}
		if !strings.Contains(tm.AskRecipient, "Jl. Melati 12") {
			t.Errorf("%s: AskRecipient needs an address example: %q", name, tm.AskRecipient)
		}
	}
}

func TestLegacyRecipientAndAddressInOneMessageID(t *testing.T) {
	res := advanceRecipient("Nama: Budi\nHP: 081234567890\nAlamat: Jl. Melati 12, Surabaya, Jawa Timur, 60111")
	if !res.Completed {
		t.Fatalf("name+phone+address in one message must complete, got step=%v reply=%q", res.State, res.Reply)
	}
}

func TestLegacyRecipientAndAddressInOneMessageEN(t *testing.T) {
	res := advanceRecipient("Name: Budi\nPhone: 081234567890\nAddress: Jl. Melati 12, Surabaya, Jawa Timur, 60111")
	if !res.Completed {
		t.Fatalf("name+phone+address in one message must complete, got reply=%q", res.Reply)
	}
}

func TestLegacyRecipientPartialNamePhoneAsksAddressOnly(t *testing.T) {
	res := advanceRecipient("Nama: Budi\nHP: 081234567890")
	if res.Completed || res.State == nil || res.State.Step != "ask_address_full" {
		t.Fatalf("name+phone must move to the address step, got %+v reply=%q", res.State, res.Reply)
	}
	if strings.Contains(strings.ToLower(res.Reply), "nama penerima") {
		t.Fatalf("must not ask the name again: %q", res.Reply)
	}
}

func TestLegacyRecipientAddressOnlyKeepsAsking(t *testing.T) {
	res := advanceRecipient("Alamat: Jl. Melati 12, Surabaya, Jawa Timur, 60111")
	if res.Completed || res.State == nil || res.State.Step != "ask_recipient" {
		t.Fatalf("address only must stay on ask_recipient, got %+v", res.State)
	}
	if res.State.Street == "" || res.State.PostalCode != "60111" {
		t.Fatalf("address must be kept for later: %+v", *res.State)
	}
}
