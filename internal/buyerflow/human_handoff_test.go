package buyerflow

import (
	"strings"
	"testing"
)

func TestIsHumanEscalationRequest_positive(t *testing.T) {
	positive := []string{
		"minta cs",
		"hubungkan ke admin",
		"bicara dengan manusia",
		"tolong sambungkan ke CS",
		"mau chat sama CS",
		"hubungi admin",
		"sambungkan ke manusia",
		"customer service dong",
		"hubungkan ke customer service",
	}
	for _, msg := range positive {
		if !IsHumanEscalationRequest(msg) {
			t.Fatalf("IsHumanEscalationRequest(%q) should be true", msg)
		}
	}
}

func TestIsHumanEscalationRequest_negative(t *testing.T) {
	negative := []string{
		"sampai kapan promonya?",
		"ongkir ke jakarta berapa ya?",
		"Jl Sudirman No 10 Jakarta Pusat 10110",
		"Cadbury biscoff bar 130 gram 1 pcs",
		"estimasi sampai kapan ya?",
		"bisa order 1 pcs?",
		"mau beli atas nama orang lain bisa ya ?",
		"mau beli human hair",
		"customer service buka jam berapa",
	}
	for _, msg := range negative {
		if IsHumanEscalationRequest(msg) {
			t.Fatalf("IsHumanEscalationRequest(%q) should be false", msg)
		}
	}
}

func TestHumanHandoffActiveDraftPreservesCart(t *testing.T) {
	sim := newWB239488Sim()
	before := len(sim.Order.Items)
	out := sim.Turn("tolong sambungkan ke CS")
	if out.Path != PathHumanHandoff {
		t.Fatalf("path=%s want %s", out.Path, PathHumanHandoff)
	}
	if sim.Order == nil {
		t.Fatal("draft order must remain")
	}
	if len(sim.Order.Items) != before {
		t.Fatalf("cart items=%d want %d", len(sim.Order.Items), before)
	}
	reply := strings.ToLower(out.Reply)
	if strings.Contains(reply, "pesanan baru") || strings.Contains(reply, "order baru") {
		t.Fatalf("handoff reply must not start new order: %q", out.Reply)
	}
	if !strings.Contains(reply, "cs") {
		t.Fatalf("reply should mention CS: %q", out.Reply)
	}
}
