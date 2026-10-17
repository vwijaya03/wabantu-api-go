package buyerflow

import "testing"

func TestRecipientNameRejectsQuestions(t *testing.T) {
	for _, s := range []string{
		"delivered today or tomorrow woi",
		"Is it going to be delivered today or tomorrow?",
		"is it frozen?",
		"Where do you ship from?",
		"how can i pay",
		"Heloo \nI want to ask about your tuna steak product",
		"kapan dikirim kak?",
	} {
		if name, _ := parseRecipientLine(s); name != "" {
			t.Errorf("parseRecipientLine(%q) name = %q, want empty", s, name)
		}
	}
	if name, phone := parseRecipientLine("Name: Anastasia\nPhone: 081234567890"); name != "Anastasia" || phone == "" {
		t.Fatalf("labelled data must still parse, got (%q,%q)", name, phone)
	}
	if name, _ := parseRecipientLine("Budi Santoso 081234567890"); name != "Budi Santoso" {
		t.Fatalf("plain name + phone must still parse, got %q", name)
	}
}

func TestQuestionsBreakCheckoutSteps(t *testing.T) {
	for _, step := range []string{"ask_recipient", "ask_address_full"} {
		for _, s := range []string{"delivered today or tomorrow woi", "is it frozen?", "what time do you start shipping"} {
			if !ShouldBreakOrderFlow(s, step, omahSteakCatalog()) {
				t.Errorf("ShouldBreakOrderFlow(%q, %s) = false, want true", s, step)
			}
		}
	}
	if ShouldBreakOrderFlow("Jl. Melati No 12, Malang, Jawa Timur 65145", "ask_address_full", omahSteakCatalog()) {
		t.Error("a real address must stay in the checkout step")
	}
}

func TestAddressHintNeedsWordBoundaries(t *testing.T) {
	for _, s := range []string{"what time do you start shipping", "is it for a party?", "smart choice"} {
		if orderAddrHintRe.MatchString(s) {
			t.Errorf("orderAddrHintRe matched %q", s)
		}
	}
	for _, s := range []string{"Jl. Melati 12", "RT 03 RW 05", "Kec. Sukolilo", "Kota Surabaya", "kode pos 60111"} {
		if !orderAddrHintRe.MatchString(s) {
			t.Errorf("orderAddrHintRe missed %q", s)
		}
	}
}

func TestProductQuestionIsNotCartAdd(t *testing.T) {
	for _, s := range []string{"how much is the tuna?", "is the tuna frozen?", "how much weight is the tuna?", "what is the price of the beef 500 gram?", "tunanya ready kak?"} {
		if !isProductQuestionNotCartAdd(s) {
			t.Errorf("isProductQuestionNotCartAdd(%q) = false, want true", s)
		}
	}
	if isProductQuestionNotCartAdd("tambah 1 tuna") {
		t.Error("explicit add is not a question")
	}
}
