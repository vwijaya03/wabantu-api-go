package adapters

import (
	"testing"

	"encore.app/wabantu/shared/interactionevidence"
	"encore.app/wabantu/shared/kbcontext"
)

func TestFromWebChatFinalTurn(t *testing.T) {
	ev := FromWebChat(WebChatInput{
		SessionID:       "sess-1",
		InboundID:       "in-1",
		OutboundID:      "out-1",
		ClientMessageID: "client-abc",
		UserText:        "harga durian?",
		FinalText:       "Durian biscuit Rp155.000",
		Path:            "catalog_db",
		Retrieval:       kbcontext.Trace{Hash: "trace1", CatalogIDs: []string{"cat-1"}},
		ProductCards:    []interactionevidence.ProductCard{{CatalogItemID: "cat-1", Name: "Durian", Price: 155000}},
	})
	if ev.Channel != interactionevidence.ChannelWebChat {
		t.Fatalf("channel=%s", ev.Channel)
	}
	if ev.ThreadRef != "sess-1" || ev.ClientMessageID != "client-abc" {
		t.Fatalf("refs thread=%q client=%q", ev.ThreadRef, ev.ClientMessageID)
	}
	if !ev.Valid() {
		t.Fatal("final grounded turn must be valid evidence")
	}
	if ev.EvidenceHash == "" || ev.InteractionRef == "" {
		t.Fatal("hash and interaction ref required")
	}
}

func TestFromWebChatRejectsAbortedAndStaff(t *testing.T) {
	aborted := FromWebChat(WebChatInput{SessionID: "s1", InboundID: "i1", Aborted: true})
	if aborted.Valid() {
		t.Fatal("aborted stream must not become evidence")
	}
	staff := FromWebChat(WebChatInput{SessionID: "s1", InboundID: "i1", Author: "staff"})
	if staff.Valid() {
		t.Fatal("staff handoff must not be judged as AI wrong_answer")
	}
}
