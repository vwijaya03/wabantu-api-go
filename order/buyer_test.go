package order

import (
	"testing"

	"encore.app/wabantu/shared/pii"
)

func TestBuyerLabel(t *testing.T) {
	o := Order{
		ContactDisplayName: "Local Test",
		ContactPhone:       "6281292066606",
	}
	if BuyerLabel(o) != "Local Test" {
		t.Fatalf("want contact name, got %q", BuyerLabel(o))
	}
	o2 := Order{
		ShippingAddress: &ShippingAddress{Name: "lulu lolita"},
	}
	if BuyerLabel(o2) != "lulu lolita" {
		t.Fatalf("fallback shipping name")
	}
	if BuyerLabel(Order{}) != "Tanpa contact" {
		t.Fatal("empty order label")
	}
}

func TestDecryptContactFieldSkipsPlaceholder(t *testing.T) {
	got := decryptContactField("", pii.Placeholder)
	if got != "" {
		t.Fatalf("placeholder name should be empty, got %q", got)
	}
	o := Order{ShippingAddress: &ShippingAddress{Name: "The Ngiek Ing"}}
	applyContactBuyer(&o, "", pii.Placeholder, "", "628999000111")
	if o.ContactDisplayName != "The Ngiek Ing" {
		t.Fatalf("want shipping fallback, got %q", o.ContactDisplayName)
	}
	if o.ContactPhone != "628999000111" {
		t.Fatalf("phone=%q", o.ContactPhone)
	}
}
