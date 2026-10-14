package buyerflow

import (
	"strings"
	"testing"
)

func TestIsQuestionLikeEnglishWholeWordOnly(t *testing.T) {
	if IsQuestionLike("maggi tandoori 1 pcs") {
		t.Fatal("product name must not count as question because of substring do")
	}
	if !IsQuestionLike("how long is shipping") {
		t.Fatal("expected English shipping question")
	}
}

func TestEnglishShopIntentInScope(t *testing.T) {
	sim := newOmahSimulator()
	msg := "hi i want to shop"
	if !sim.InScope(msg) {
		t.Fatal("English shop intent should be in business scope")
	}
	if IsHumanEscalationRequest(msg) {
		t.Fatal("shop intent must not trigger human handoff")
	}
}

func TestEnglishShippingETAWithoutInventedDays(t *testing.T) {
	profile := omahProfile()
	reply, ok := TryShippingFAQReply("how many days until it arrives", profile, nil, false)
	if !ok {
		t.Fatal("expected shipping FAQ path")
	}
	if strings.Contains(reply, "2-3") || strings.Contains(reply, "2–3") {
		t.Fatalf("must not invent day counts without KB: %q", reply)
	}
}

func TestEnglishShippingETAFromKB(t *testing.T) {
	cat := "Pengiriman"
	kb := []KBEntry{{
		Question: "how long shipping",
		Answer:   "Usually 2-3 business days via courier.",
		Category: &cat,
		IsActive: true,
	}}
	reply, ok := TryShippingFAQReply("how many days until it arrives", nil, kb, false)
	if !ok || !strings.Contains(reply, "2-3") {
		t.Fatalf("KB ETA should pass through: %q ok=%v", reply, ok)
	}
}

func TestEnglishTalkToHumanKeepsCart(t *testing.T) {
	sim := newWB239488Sim()
	before := len(sim.Order.Items)
	out := sim.Turn("talk to a human")
	if out.Path != PathHumanHandoff {
		t.Fatalf("path=%s want %s", out.Path, PathHumanHandoff)
	}
	if sim.Order == nil || len(sim.Order.Items) != before {
		t.Fatalf("cart must stay, items=%d want %d", len(sim.Order.Items), before)
	}
	if !IsEnglishBuyerMessage("talk to a human") {
		t.Fatal("expected English detection")
	}
	if strings.Contains(strings.ToLower(out.Reply), "sambungkan") {
		t.Fatalf("expected English handoff copy: %q", out.Reply)
	}
}

func TestUnmatchedProductCatalogMissAiNoAnswer(t *testing.T) {
	msg := "harga flying saucer model xl berapa?"
	profile := omahProfile()
	reply, ok := replyFromBusinessCatalog(msg, profile, omahCatalog(), nil, nil)
	if !ok || !IsCatalogNotFoundReply(reply) {
		t.Fatalf("need catalog miss for probe, ok=%v reply=%q", ok, reply)
	}
	sim := newOmahSimulator()
	out := sim.Turn(msg)
	if out.Path != PathAiNoAnswer {
		t.Fatalf("path=%s want %s reply=%q", out.Path, PathAiNoAnswer, out.Reply)
	}
}

func TestAiNoAnswerHandoffDoesNotClearDraftOrder(t *testing.T) {
	sim := newWB239488Sim()
	before := len(sim.Order.Items)
	if sim.Order == nil {
		t.Fatal("fixture must have draft")
	}
	// Simulator keeps s.Order on ai_no_answer catalog handoff (same as autoreply: no clearOrderState).
	formal := strOrEmpty(sim.Profile.Tone) == "formal"
	_ = NoAnswerHandoffReply(formal, "do you sell rainbow unicorn cereal 900g?")
	if sim.Order == nil || len(sim.Order.Items) != before {
		t.Fatal("draft order must not be cleared by no-answer handoff path")
	}
}

func TestShouldNoAnswerHandoffHelper(t *testing.T) {
	if !ShouldNoAnswerHandoff("what is your return policy for opened snack packages?", true) {
		t.Fatal("expected no-answer handoff for unmatched in-scope question")
	}
	if ShouldNoAnswerHandoff("Loha", false) {
		t.Fatal("non-question chatter should not no-answer handoff")
	}
}

func TestEnglishGreetingReply(t *testing.T) {
	reply := GreetingReply("hello", "", "")
	if strings.Contains(reply, "Ada yang bisa") || strings.Contains(reply, "kak!") {
		t.Fatalf("English greeting should not use Indonesian tail: %q", reply)
	}
	if !strings.Contains(strings.ToLower(reply), "how can") {
		t.Fatalf("expected English help prompt: %q", reply)
	}
}

func TestIndonesianGreetingUnchanged(t *testing.T) {
	reply := GreetingReply("halo kak", "", "")
	if !strings.Contains(reply, "bantu") {
		t.Fatalf("Indonesian greeting regression: %q", reply)
	}
}

func TestEnglishRecipientPolicyReply(t *testing.T) {
	reply := buildRecipientPolicyReply("can I buy for someone else", false)
	if strings.Contains(reply, "Bisa kak") {
		t.Fatalf("expected English recipient policy: %q", reply)
	}
	if !strings.Contains(strings.ToLower(reply), "someone else") {
		t.Fatalf("expected English recipient policy: %q", reply)
	}
}

func TestEnglishOrderFlowAskProduct(t *testing.T) {
	sim := newOmahSimulator()
	out := sim.Turn("I want to order")
	if out.Path != PathOrderFlow {
		t.Fatalf("path=%s want order_flow", out.Path)
	}
	if strings.Contains(out.Reply, "Mau order produk") || strings.Contains(out.Reply, "Siap kak") {
		t.Fatalf("English buyer got Indonesian order prompt: %q", out.Reply)
	}
	if !strings.Contains(strings.ToLower(out.Reply), "product") {
		t.Fatalf("expected English ask-product: %q", out.Reply)
	}
}

func TestEnglishHumanHandoffReplyLanguage(t *testing.T) {
	sim := newOmahSimulator()
	out := sim.Turn("talk to customer service")
	if out.Path != PathHumanHandoff {
		t.Fatalf("path=%s", out.Path)
	}
	if strings.Contains(out.Reply, "sambungkan") || strings.Contains(out.Reply, "Siap kak") {
		t.Fatalf("English handoff should be English: %q", out.Reply)
	}
}

func TestRecipientPolicyNotAiOrBuyerHandoff(t *testing.T) {
	msgs := []string{
		"mau beli atas nama orang lain",
		"can I buy for someone else",
	}
	for _, msg := range msgs {
		if IsHumanEscalationRequest(msg) {
			t.Fatalf("recipient policy must not be CS handoff: %q", msg)
		}
		sim := newOmahSimulator()
		out := sim.Turn(msg)
		if out.Path == PathAiNoAnswer || out.Path == PathHumanHandoff {
			t.Fatalf("path=%s for recipient policy: %q", out.Path, msg)
		}
		if out.Path != PathRecipientPolicy {
			t.Fatalf("path=%s want %s for %q", out.Path, PathRecipientPolicy, msg)
		}
	}
}
