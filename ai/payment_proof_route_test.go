package ai

import (
	"strings"
	"testing"
	"time"
)

func TestProofRoutingRejectedWithNewerOrderAsks(t *testing.T) {
	t0 := time.Date(2026, 10, 13, 10, 33, 0, 0, time.UTC)
	old := &persistedOrder{ID: "07b19997-3a34-4d48-a172-40757f5ab38a", PaymentStatus: "rejected", CreatedAt: t0}
	newer := &persistedOrder{ID: "5f6198fe-bb3f-4934-a610-a653f1334b05", PaymentStatus: "unpaid", CreatedAt: t0.Add(20 * time.Minute)}
	if !proofTargetSuperseded(old, newer, false) {
		t.Fatal("rejected target with a newer order must not be targeted")
	}
	refs := askOrderRefs(old, newer)
	if len(refs) != 2 || refs[0] != "WB-07B19997" || refs[1] != "WB-5F6198FE" {
		t.Fatalf("refs %v", refs)
	}
	reply := paymentProofAskOrderReply(refs)
	if !strings.Contains(reply, "pesanan yang mana") || !strings.Contains(reply, "WB-07B19997 / WB-5F6198FE") {
		t.Fatalf("reply %q", reply)
	}
}

func TestProofRoutingBurstNewOrderIntentAsks(t *testing.T) {
	img := time.Date(2026, 10, 13, 10, 53, 29, 980e6, time.UTC)
	old := &persistedOrder{ID: "07b19997-3a34-4d48-a172-40757f5ab38a", PaymentStatus: "rejected", CreatedAt: img.Add(-20 * time.Minute)}
	hist := []dbMessage{
		{Author: "ai", Type: "text", Body: "Ini katalog", CreatedAt: img.Add(-3 * time.Second)},
		{Author: "contact", Type: "text", Body: "Halo kak, saya mau pesan Kopi Arabika Gayo 250g 2 pcs", CreatedAt: img.Add(-439 * time.Millisecond)},
		{Author: "contact", Type: "image", CreatedAt: img},
	}
	burst := burstHasNewOrderIntent(hist, img)
	if !burst || !proofTargetSuperseded(old, old, burst) {
		t.Fatal("rejected target + new-order burst must ask (incident WB-07B19997)")
	}
}

// Normal paths unchanged: non-rejected targets, and a re-sent proof for the same rejected order.
func TestProofRoutingNormalPathUnchanged(t *testing.T) {
	img := time.Now()
	unpaid := &persistedOrder{ID: "a", PaymentStatus: "unpaid", CreatedAt: img.Add(-time.Hour)}
	newer := &persistedOrder{ID: "b", PaymentStatus: "unpaid", CreatedAt: img}
	if proofTargetSuperseded(unpaid, newer, true) {
		t.Fatal("non-rejected target must stay targeted")
	}
	rejected := &persistedOrder{ID: "a", PaymentStatus: "rejected", CreatedAt: img.Add(-time.Hour)}
	hist := []dbMessage{{Author: "contact", Type: "text", Body: "ini bukti transfer pesanan saya kak", CreatedAt: img.Add(-time.Second)}}
	if proofTargetSuperseded(rejected, rejected, burstHasNewOrderIntent(hist, img)) {
		t.Fatal("re-sent proof for the same rejected order (no newer order, no new-order intent) must stay targeted")
	}
	old := []dbMessage{{Author: "contact", Type: "text", Body: "mau pesan kopi", CreatedAt: img.Add(-10 * time.Minute)}}
	if burstHasNewOrderIntent(old, img) {
		t.Fatal("intent outside the burst window must not count")
	}
	if proofTargetSuperseded(nil, newer, true) {
		t.Fatal("nil target")
	}
}

func TestNewOrderIntentText(t *testing.T) {
	for s, want := range map[string]bool{
		"saya mau pesan Kopi 2 pcs":          true,
		"mau buat pesanan baru":              true,
		"pengen beli teh":                    true,
		"ini bukti transfer pesanan saya":    false,
		"sudah bayar ya kak, mau pesan lagi": false, // payment talk wins (conservative)
		"pesanan saya kapan dikirim?":        false,
		"":                                   false,
	} {
		if got := isNewOrderIntentText(s); got != want {
			t.Errorf("%q = %v want %v", s, got, want)
		}
	}
}
