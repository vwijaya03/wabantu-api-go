package adapters

import (
	"testing"

	"encore.app/wabantu/shared/interactionevidence"
	"encore.app/wabantu/shared/kbcontext"
)

func TestFromWhatsAppRejectsAbortedAndStaff(t *testing.T) {
	ok := FromWhatsApp(WhatsAppInput{
		ConversationID: "c1",
		InboundID:      "i1",
		OutboundID:     "o1",
		FinalText:      "halo",
		Path:           "consulting",
		Author:         "ai",
		Retrieval:      kbcontext.Trace{Hash: "abc"},
	})
	if !ok.Valid() {
		t.Fatal("final AI persist should be valid")
	}
	if ok.Channel != interactionevidence.ChannelWhatsApp {
		t.Fatalf("channel=%s", ok.Channel)
	}
	aborted := FromWhatsApp(WhatsAppInput{ConversationID: "c1", InboundID: "i1", Aborted: true})
	if aborted.Valid() {
		t.Fatal("aborted must not be stored as evidence")
	}
	staff := FromWhatsApp(WhatsAppInput{ConversationID: "c1", InboundID: "i1", Author: "staff"})
	if staff.Valid() {
		t.Fatal("staff must not be judged")
	}
}
