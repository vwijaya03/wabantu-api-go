package ai

import "testing"

func TestOrderAbon3BijiNotConsultingOnly(t *testing.T) {
	msg := "order abon 3 biji"
	if IsConsultingPurchaseQuestion(msg, nil) {
		t.Fatalf("explicit order line should not be consulting-only: %q", msg)
	}
	if !hasPurchaseIntent(msg, omahCatalog()) {
		t.Fatalf("expected purchase intent for %q", msg)
	}
}

func TestMau1BijiAbonCartReady(t *testing.T) {
	msg := "mau 1 biji abon"
	if IsConsultingPurchaseQuestion(msg, nil) {
		t.Fatalf("explicit qty order should not be MOQ consult: %q", msg)
	}
	if !hasPurchaseIntent(msg, omahCatalog()) {
		t.Fatalf("expected purchase intent for %q", msg)
	}
}

func TestMinimumOrderNotGreeting(t *testing.T) {
	for _, msg := range []string{"min order", "min pesan berapa", "bisa order 1 pcs"} {
		if IsGreetingLike(msg) {
			t.Fatalf("%q should not be greeting", msg)
		}
		if !IsMinimumOrderQuestion(msg) && msg != "bisa order 1 pcs" {
			continue
		}
	}
}

func TestProductSearchNotPurchase(t *testing.T) {
	msg := "mau cari hello kitty"
	if hasPurchaseIntent(msg, omahCatalog()) {
		t.Fatalf("search phrase should not trigger cart: %q", msg)
	}
}
