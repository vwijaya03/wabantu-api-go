package admin

import (
	"strings"
	"testing"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/shared/triageincident"
)

func TestBuildDraftContract_abonCatalogMissBecomesBuyerflow(t *testing.T) {
	c := buildDraftContract(
		"whatsapp",
		buyerflow.PathLLMGrounded,
		"wrong_answer",
		"hah ? nambah abon sapi 125 gram 1, abon sapi 500 gram 1, bisa nggak ini ?",
		"Saya belum menemukan data tersebut di katalog saat ini.",
	)
	if c.Lane != triageincident.LaneBuyerflow {
		t.Fatalf("lane=%q want buyerflow", c.Lane)
	}
	if c.Assertions.WantPath != buyerflow.PathOrderFlow {
		t.Fatalf("wantPath=%q want order_flow", c.Assertions.WantPath)
	}
	if !ai.HasDeterministicInvariant(c) {
		t.Fatal("contract must be dispatchable to Composer")
	}
	if len(c.Assertions.CartInclude) < 2 {
		t.Fatalf("cartInclude=%v want 2 lines", c.Assertions.CartInclude)
	}
	joined := c.Assertions.CartInclude[0].NameContains + " " + c.Assertions.CartInclude[1].NameContains
	if !strings.Contains(joined, "abon sapi 125") || !strings.Contains(joined, "abon sapi 500") {
		t.Fatalf("cart names = %v", c.Assertions.CartInclude)
	}
	if len(c.Assertions.ReplyExcludes) == 0 || !strings.Contains(strings.Join(c.Assertions.ReplyExcludes, ","), "belum menemukan") {
		t.Fatalf("replyExcludes=%v", c.Assertions.ReplyExcludes)
	}
}

func TestParseCartSegment_qtyAfterGram(t *testing.T) {
	name, qty, ok := parseCartSegment("nambah abon sapi 125 gram 1")
	if !ok {
		t.Fatal("expected parse")
	}
	if qty != 1 {
		t.Fatalf("qty=%d", qty)
	}
	if name != "abon sapi 125 gram" {
		t.Fatalf("name=%q", name)
	}
}
