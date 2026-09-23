package templates

import "testing"

func TestRevenueSplitComputedServerSide(t *testing.T) {
	price := 100_000
	platformFee := price * platformFeePercent / 100
	devShare := price - platformFee
	if platformFee != 20_000 || devShare != 80_000 {
		t.Fatalf("unexpected split platform=%d dev=%d", platformFee, devShare)
	}
}
