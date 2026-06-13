package order

import "testing"

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
