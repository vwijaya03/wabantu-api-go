package buyerflow

import "testing"

func TestProductBaseNameStripsColorSuffix(t *testing.T) {
	hitam := ProductBaseName("Kaos Polos Premium - Hitam")
	putih := ProductBaseName("Kaos Polos Premium - Putih")
	if hitam != putih || hitam != "Kaos Polos Premium" {
		t.Fatalf("hitam=%q putih=%q", hitam, putih)
	}
	if ProductBaseName("Boxer Pria Mono - L") != "Boxer Pria Mono" {
		t.Fatal("size suffix must still strip")
	}
}
