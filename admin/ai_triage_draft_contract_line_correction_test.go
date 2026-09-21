package admin

import (
	"strings"
	"testing"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/shared/triageincident"
)

func TestBuildDraftContract_dibatalkanLineMutateNotOrderCancel(t *testing.T) {
	userText := "cadburry nya tolong dibatalkan, lalu musang king durian nya mau nambah 1 ya"
	reply := "Untuk batalkan, pilih nomor pesanan yang benar ya kak:\n\n1. WB-239488D0 — Cadbury biscoff bar 130 gram"
	c := buildDraftContract("whatsapp", buyerflow.PathOrderCancel, "human_report", userText, reply)
	if c.Lane != triageincident.LaneBuyerflow {
		t.Fatalf("lane=%q want buyerflow", c.Lane)
	}
	if c.Assertions.WantPath != buyerflow.PathOrderFlow {
		t.Fatalf("wantPath=%q want order_flow", c.Assertions.WantPath)
	}
	if !ai.HasDeterministicInvariant(c) {
		t.Fatal("contract must be dispatchable to Composer")
	}
	if len(c.Assertions.CartExclude) == 0 || !strings.Contains(strings.Join(c.Assertions.CartExclude, " "), "cadbury") {
		t.Fatalf("cartExclude=%v want cadbury", c.Assertions.CartExclude)
	}
	if len(c.Assertions.CartInclude) == 0 || !strings.Contains(c.Assertions.CartInclude[0].NameContains, "durian") {
		t.Fatalf("cartInclude=%v want durian", c.Assertions.CartInclude)
	}
	joined := strings.Join(c.Assertions.ReplyExcludes, " ")
	if !strings.Contains(joined, "pilih nomor pesanan") {
		t.Fatalf("replyExcludes=%v", c.Assertions.ReplyExcludes)
	}
}
