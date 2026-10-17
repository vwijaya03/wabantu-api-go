package buyerflow

import "testing"

func TestEnShipsTo(t *testing.T) {
	if got := enShipsTo("We ship", "across Indonesia"); got != "We ship across Indonesia" {
		t.Fatalf("got %q", got)
	}
	if got := enShipsTo("We deliver", "Surabaya"); got != "We deliver to Surabaya" {
		t.Fatalf("got %q", got)
	}
}
