package buyerflow

import "testing"

func TestIsChannelRepairMeta(t *testing.T) {
	if !IsChannelRepairMeta("maaf chatnya putus putus") {
		t.Fatal("Omah live meta must be channel repair")
	}
	if !IsChannelRepairMeta("ketik ulang ya kak") {
		t.Fatal("ketik ulang is channel repair")
	}
	if IsChannelRepairMeta("halo\nsaya mau beli\nmagi\nbisa ga ?") {
		t.Fatal("stitched purchase is not repair-only")
	}
	if IsChannelRepairMeta("saya mau beli maggi") {
		t.Fatal("purchase is not repair")
	}
}

func TestIsGreetingLike_stitchedOmahBurst(t *testing.T) {
	stitched := "halo\nsaya mau beli\nmagi\nbisa ga ?"
	if IsGreetingLike(stitched) {
		t.Fatal("stitched burst must not classify as greeting (would clearOrderState)")
	}
	if !IsGreetingLike("halo") {
		t.Fatal("halo alone is still greeting")
	}
}
